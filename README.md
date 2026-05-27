# llm-api-gateway

An OpenAI-compatible HTTP reverse proxy for LLM API traffic, written in Go using the standard library. Acts as a single endpoint in front of multiple LLM providers (OpenAI, OpenRouter, and any other OpenAI-compatible upstream), with multi-provider failover, per-provider circuit breaking, token-aware rate limiting, encrypted per-client vendor key storage (BYOK), streaming proxy with context-aware disconnect handling, and Prometheus + JSON observability.

Clients point an unmodified OpenAI SDK at the gateway. The gateway terminates the client connection, decides which provider should serve the request, manages failover and health, meters tokens, and streams the response back — invisibly.

```python
from openai import OpenAI

client = OpenAI(
    api_key="gw_exampleuser",                         # gateway key — identifies the client
    base_url="http://localhost:8080/v1",        # the gateway, not OpenAI
)

resp = client.chat.completions.create(
    model="openai/gpt-4o-mini",
    messages=[{"role": "user", "content": "hello"}],
)
```

---

## Why this exists

Without a gateway, every service that talks to an LLM independently reimplements retries, rate limiting, failover, token tracking, and key management. The gateway centralizes those concerns so application code stays focused on prompts and product logic.

Concretely, the gateway provides:

- **Reliability.** When one provider returns 5xx or refuses connections, traffic transparently fails over to the next. Providers that fail repeatedly are taken out of rotation by a circuit breaker until they recover.
- **Fair budget enforcement.** Rate limiting is metered in *tokens*, not requests — because a 5-token request and a 50,000-token request differ in cost by 10,000×, and request-count limiting treats them identically.
- **Per-client key isolation.** Each client registers their own vendor API key (BYOK). Keys are stored encrypted at rest with AES-256-GCM; plaintext exists only as a transient local variable at the point of upstream call.
- **One observable surface.** Tokens, latencies, failures, and breaker states are exposed at `/stats` (JSON) and `/metrics` (Prometheus text format).
- **Zero client-side changes.** The gateway speaks the OpenAI wire format; any OpenAI-compatible SDK works against it by changing only `base_url`.

---

## Architecture

```
                    ┌──────────────────────────────────────────────────────┐
                    │                       Gateway                        │
Client ─── POST ──► │  ┌─ auth identity (Bearer)                            │
(OpenAI SDK)        │  ├─ token-bucket rate limiter (per gateway key)       │
                    │  ├─ provider selector (registry order, health-aware)  │
                    │  ├─ BYOK keystore (decrypt vendor key per request)    │
                    │  ├─ outbound HTTP client + circuit breaker per prov.  │
                    │  ├─ streaming proxy (Flush per chunk, ctx-cancellable)│
                    │  └─ metrics + structured logs (request-ID correlated) │
                    └──────────────────────────────────────────────────────┘
                                  │              │              │
                                  ▼              ▼              ▼
                              OpenAI       OpenRouter      Other OAI-compat
                                                           (vLLM, Groq, ...)
```

Each layer is a separate package under `internal/`:

| Package | Responsibility |
|---|---|
| `internal/proxy` | HTTP handler, failover loop, streaming with `http.Flusher`, context-aware disconnect, header forwarding (hop-by-hop stripped per RFC 7230) |
| `internal/provider` | `Provider` interface, `OpenAICompatible` implementation, registry |
| `internal/ratelimit` | Token-bucket limiter with lazy refill, per-key buckets, post-hoc reconciliation |
| `internal/secrets` | AES-256-GCM encryptor with nonce-prepend blob format |
| `internal/keystore` | Per-client vendor key storage; interface-backed for swappable persistence |
| `internal/health` | Three-state circuit breaker (closed/open/half-open) and per-provider tracker |
| `internal/metrics` | Counters + Prometheus and JSON exposition |
| `cmd/gateway` | Composition root: reads config, constructs dependencies, runs the server with graceful shutdown |

---

## Quickstart

### Requirements

- Go 1.21+ (for `log/slog`)
- An OpenRouter API key (free tier works) or any OpenAI-compatible provider's key

### Configuration

The gateway is configured by environment variables:

| Variable | Required | Purpose |
|---|---|---|
| `OPENROUTER_API_KEY` | yes (or other provider) | Fallback vendor key used when a client has no BYOK key registered |
| `GATEWAY_MASTER_KEY` | yes | Base64-encoded 32-byte master key for AES-256-GCM encryption of stored vendor keys |
| `GATEWAY_SEED_KEYS` | no | JSON map of seeded BYOK keys (see below) |

Generate a master key:

```sh
export GATEWAY_MASTER_KEY="$(openssl rand -base64 32)"
```

Optionally seed per-client BYOK keys at startup:

```sh
export GATEWAY_SEED_KEYS='{"gw_exampleuser":{"openrouter":"sk-or-v1-..."}}'
```

### Run

```sh
go run ./cmd/gateway
```

The server listens on `:8080`. Health check: `GET /healthz`.

### Make a request

```sh
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer gw_exampleuser" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o-mini",
    "messages": [{"role": "user", "content": "hello"}],
    "stream": false
  }'
```

For streaming, add `"stream": true` and use `curl -N` to disable client-side buffering so tokens appear as they arrive.

---

## Design decisions and tradeoffs

### OpenAI-compatible pass-through, not full normalization

The gateway forwards request and response bodies essentially untouched. It does not translate between provider-specific API shapes (e.g. OpenAI's `/chat/completions` to Anthropic's native `/v1/messages`). This is deliberate: it keeps the gateway a byte-forwarder rather than a format-translator, focusing the engineering on the proxy, concurrency, and reliability layers instead of brittle per-vendor JSON munging — particularly across the streaming event formats, which differ significantly between vendors.

Anthropic and Gemini remain reachable through their own OpenAI-compatible endpoints, or via OpenRouter. The result is broad provider reach without the translation tax. Normalizing to OpenAI format across vendors with native APIs is a documented extension point (see below).

### Failover is bounded by the first write to the client

Once the gateway has called `writer.WriteHeader(status)`, response bytes are on the wire and the gateway is committed to the chosen provider — the status cannot be retracted and partial body cannot be replayed. This shapes the failover loop: provider selection is retryable up to the moment of commit, and a mid-stream provider failure can only be logged and surfaced as a truncated response. This is the same constraint that makes streaming interesting at all — you commit to a status before knowing the outcome — viewed from a second angle.

### Rate limiting is token-weighted and reconciled post-hoc

Request-count limiting treats a 5-token request and a 50,000-token request as equal, which is incorrect for LLM workloads where token count is the unit of cost. The gateway meters in tokens via a token-bucket algorithm with lazy refill.

The honest difficulty: token cost is only known *after* the response completes (for streaming, in the final SSE chunk carrying `usage`). The gateway addresses this with a pre-flight gate (admit any key with a non-negative balance) and post-hoc reconciliation (charge the true token count after the response, allowing the balance to go negative). A key that overshoots once is permitted that request but throttled on subsequent ones until refill brings the balance back above zero. The negative-balance mechanism is the LLM-specific twist that distinguishes this from generic HTTP rate limiting.

For streaming, the final `usage` chunk is captured by a bounded rolling-tail buffer (8 KB) during forwarding — bounded memory regardless of response length.

### AES-256-GCM, not Fernet, not plain AES

Stored vendor keys are encrypted with AES-256-GCM. GCM is an AEAD (Authenticated Encryption with Associated Data) mode, which provides both confidentiality *and* integrity — tampered ciphertext is detected at decryption and produces an error rather than corrupted plaintext. Plain AES (e.g. CBC) provides only confidentiality and would allow undetected tampering of stored keys.

Each encryption uses a fresh 12-byte nonce from `crypto/rand`, prepended to the ciphertext (`nonce || ciphertext+tag`). Nonce reuse under a single key would break GCM's security entirely; a fresh nonce per call is non-negotiable and is enforced as the first operation in `Encrypt`. Decryption returns an error rather than partial or unauthenticated plaintext on any tampering, truncation, or wrong-key attempt.

The master key is held in `GATEWAY_MASTER_KEY` (separately from the encrypted store), so an attacker stealing only the store cannot decrypt. In production this would live in a secrets manager (KMS, Vault); the env-var pattern is the same shape.

### Circuit breaker prevents wasted work on known-bad providers

Reactive failover alone wastes one doomed attempt per request when a provider is down. The per-provider circuit breaker remembers recent failures and, after a threshold, transitions to *open* — skipping the provider entirely until a timeout elapses. After the timeout, exactly one trial request is allowed through (*half-open* state); if it succeeds the breaker closes, if it fails the breaker reopens and the timeout restarts.

The one-trial guarantee is enforced by the atomicity of check-and-transition under the breaker's mutex: the first goroutine to observe an elapsed timeout transitions open→half-open and becomes the trial; concurrent goroutines see half-open and are denied. This prevents the thundering herd of slamming a recovering provider with full load.

What counts as a health failure is deliberately scoped: transport errors and capacity-signaling statuses (429, 502, 503, 504) ding health; client-fault statuses (400, 401) do not, because the provider is healthy and rejected a bad request correctly. The same retryable/terminal split governs failover.

### Concurrency: stateless crypto, atomic counters, mutex-guarded state

The codebase makes deliberate choices between concurrency primitives based on what the state actually is:

- **`sync.Mutex`** for state with invariants between fields (rate limiter bucket: `credits` and `lastRefill` must move together under one lock; the entire check-and-deduct is atomic)
- **`sync.RWMutex`** for read-dominated state (keystore: many reads per write, so readers run concurrently)
- **`sync/atomic`** for independent monotonic counters (metrics: each counter is its own integer with no cross-counter invariant)
- **No synchronization** for stateless work (the AES-GCM cipher: many goroutines decrypt concurrently with no shared mutable state)

Every test runs under `-race`, and the package tests explicitly hammer concurrent paths to prove no data race exists.

---

## Endpoints

| Path | Purpose |
|---|---|
| `POST /v1/chat/completions` | Proxy a chat completion. Forwards to the registered provider chain. Supports `stream: true` and `stream: false`. |
| `GET /healthz` | Liveness check. Returns `ok`. |
| `GET /stats` | JSON snapshot of counters and per-provider circuit breaker states. Human-readable; intended for ad-hoc inspection and the future dashboard. |
| `GET /metrics` | Prometheus text exposition format. Intended for scraping. |

Response headers added by the gateway:

- `X-Gateway-Provider` — name of the provider that served this request.
- `X-Gateway-Request-Id` — short random ID correlating every server-side log line for this request.

---

## Testing

```sh
go test -race ./...
```

Notable tests:

- `internal/proxy.TestStreamingDisconnectStopsPromptly` — proves that when a streaming client disconnects mid-response, the upstream goroutine and connection are torn down promptly via context cancellation, rather than leaking until the upstream stream completes.
- `internal/proxy.TestFailoverToHealthyProvider` / `TestNoFailoverOnClientError` — prove the retry *policy*: transient/5xx errors fail over, 4xx errors are terminal and pass through.
- `internal/ratelimit.TestConcurrentConsumeNoRace` — 50 goroutines × 100 attempts hammer a capacity-1000 bucket; asserts *exactly* 1000 allowances (any more = check-and-deduct race over-granted; any fewer = an allowance was lost).
- `internal/secrets.TestTamperingIsDetected` — flips a byte in the ciphertext and asserts decryption fails, making the AEAD integrity guarantee visible rather than assumed.
- `internal/secrets.TestNonceIsUniquePerEncryption` — encrypts identical plaintext twice and asserts different outputs; catches accidental nonce reuse.
- `internal/keystore.TestStoredValueIsEncrypted` — white-box test reaching into the raw map to confirm the stored bytes don't contain plaintext.
- `internal/health.TestHalfOpenTrialAndRecovery` — proves that after the open timeout exactly one trial is allowed; a concurrent second caller is denied.

---

## Observability example

After a few requests, `/stats` returns:

```json
{
  "circuits": { "openrouter": "closed" },
  "counters": {
    "RequestsTotal": 5,
    "RateLimited": 0,
    "AllProvidersFailed": 0,
    "TokensTotal": 90,
    "ProviderSuccess": { "openrouter": 5 },
    "ProviderFailure": {}
  }
}
```

And `/metrics`:

```
# HELP gateway_requests_total Total requests received.
# TYPE gateway_requests_total counter
gateway_requests_total 5

# HELP gateway_tokens_total Total tokens proxied.
# TYPE gateway_tokens_total counter
gateway_tokens_total 90

# HELP gateway_provider_requests_total Successful requests per provider.
# TYPE gateway_provider_requests_total counter
gateway_provider_requests_total{provider="openrouter"} 5

# HELP gateway_circuit_state Circuit breaker state per provider.
# TYPE gateway_circuit_state gauge
gateway_circuit_state{provider="openrouter",state="closed"} 1
```

Server-side logs are structured (`log/slog`) and correlated by `request_id`:

```
level=INFO msg="proxied request" request_id=a1b2c3d4e5f60718 provider=openrouter
  stream=false upstream_status=200 resp_bytes=875 credits_available=999982.88
  attempts=1 latency_ms=2041
```

---

## Roadmap

Deliberately deferred extensions, each hanging off a seam already present in the codebase:

- **Vendor key registration endpoint.** A `POST /admin/keys` for clients to register/rotate BYOK keys at runtime. The `keystore.Store` interface already supports `Set`; only the admin auth layer and HTTP handler are missing.
- **Persistent keystore (SQLite/Postgres).** Same `keystore.Store` interface; swap the `MemoryStore` implementation in `cmd/gateway/main.go`.
- **Pluggable selection strategy.** The current "iterate registry order, skip open breakers" can be replaced with strategies that read `tracker.Statuses()`, `limiter.Available()`, and measured latencies to sort providers per request — by health, by remaining client budget, or by user-defined preference order. The hooks are present.
- **Dashboard UI.** A frontend reading `/stats` to render per-key token usage, per-provider health, and recent request logs. The data layer exists; the frontend doesn't.
- **Error envelope normalization.** Upstream errors are currently passed through verbatim, exposing some provider topology (e.g. `provider_name` from OpenRouter). Normalizing to a consistent gateway error shape is a small change in the failover loop's response-writing branch.
- **Sliding-window failure tracking.** The breaker currently trips on *consecutive* failures. A sliding-window count ("N failures in the last T seconds") is more sophisticated and is the natural V2 of the breaker.

---

## License

MIT.