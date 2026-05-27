package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"os"

	"github.com/James-Mustamandi/llm-api-gateway/internal/provider"
	"github.com/James-Mustamandi/llm-api-gateway/internal/ratelimit"
	"github.com/James-Mustamandi/llm-api-gateway/internal/keystore"
	"github.com/James-Mustamandi/llm-api-gateway/internal/secrets"
	"github.com/James-Mustamandi/llm-api-gateway/internal/health"
	"github.com/James-Mustamandi/llm-api-gateway/internal/metrics"

)

func slowStreamUpstream(eventCount int, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for range eventCount {
			select {
			case <- r.Context().Done():
				return
			case <-time.After(delay):
				_, _ = io.WriteString(w, "data: token\n\n")
				flusher.Flush()
			}
		}
	}
}

func TestStreamingDisconnect(t *testing.T) {
	upstream := httptest.NewServer(slowStreamUpstream(100, 50 * time.Millisecond))
	defer upstream.Close()	

	registry := provider.NewRegistry(
		provider.NewOpenAICompatible("fake", upstream.URL, "fake-key", nil),
	)

	limiter := ratelimit.New(ratelimit.Config{Capacity: 100, RefillPerSecond: 100})

	masterKey := os.Getenv("GATEWAY_MASTER_KEY")
	encryptor, err := secrets.NewEncryptor(masterKey)
	if err != nil {
		t.Fatalf("invalid master key")
	}

	store := keystore.NewMemoryStore(encryptor)

	failureThresholdRetries := 5
	trackerTimeout := 5.0 * time.Second
	tracker := health.NewTracker(failureThresholdRetries, trackerTimeout)
	counters := metrics.New()

	proxy := New(upstream.Client(), registry, limiter, slog.Default(), store, tracker, counters)

	gateway := httptest.NewServer(http.HandlerFunc(proxy.HandleChatCompletions))
	defer gateway.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL, strings.NewReader(`{"stream":true}`))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer resp.Body.Close()

	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, _ = io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)

	if elapsed > 2 * time.Second {
		t.Fatalf("Stream took %v to stop after disconnect. Expected prompt termination -"+"the upstream read is not being cancelled on client disconnect", elapsed)

	}
	t.Logf("stream stopped %v after start (cancel invoked at 200ms) upstream cancellation works", elapsed)
}