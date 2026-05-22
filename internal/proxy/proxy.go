package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/James-Mustamandi/llm-api-gateway/internal/provider"
	"github.com/James-Mustamandi/llm-api-gateway/internal/ratelimit"
)

type Proxy struct {
	client      *http.Client
	registry 	*provider.Registry
	limiter		*ratelimit.Limiter
	logger 		*slog.Logger
}


type streamRequest struct {
	Stream bool `json:"stream"`
}

const usageTailBytes = 8192

func New(client *http.Client, registry *provider.Registry, limiter *ratelimit.Limiter, logger *slog.Logger) *Proxy {
	return &Proxy{
		client:		client,
		registry:	registry,
		limiter:	limiter,	
		logger:		logger,
	}
}


func (proxy *Proxy) HandleChatCompletions(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)

	if err != nil {
		http.Error(writer, "failed to read request body", http.StatusBadRequest)
	}
	defer request.Body.Close()

	key := clientKey(request)
	if !proxy.limiter.Allow(key, 0) {
		proxy.logger.Warn("Rate limited", "key", key, "balance", proxy.limiter.CreditsAvailable(key))
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"error":{"message":rate limit exceeded", "type":"rate_limit_error"}}`)
		return
	}


	var streamReq streamRequest
	_ = json.Unmarshal(body, &streamReq)

	providers := proxy.registry.Providers()
	if len(providers) == 0 {
		http.Error(writer, "No providers configured", http.StatusInternalServerError)
		return
	}
	
	start := time.Now()
	var lastError error

	for i, provider := range providers {
		outReq, err := http.NewRequestWithContext(request.Context(), http.MethodPost, provider.Endpoint(request.URL.Path), bytes.NewReader(body))
		if err != nil {
			lastError = fmt.Errorf("Building request for %s: %w", provider.Name(), err)
			continue
		}

		copyHeaders(outReq.Header, request.Header)
		provider.Authorize(outReq)
		if outReq.Header.Get("Content-Type") == "" {
			outReq.Header.Set("Content-Type", "application/json")
		}

		upstreamResponse, err := proxy.client.Do(outReq)
		if err != nil {

			if errors.Is(err, context.Canceled) {
				proxy.logger.Info("Client disconnected before upstream responded", "provider", provider.Name())
			}
			lastError = fmt.Errorf("%s request failed: %w", provider.Name(), err)
			proxy.logger.Warn("provider failed, trying next", "provider", provider.Name(), "attempt", i + 1, "err", err)
			continue
		}

		if shouldFailover(upstreamResponse.StatusCode) && i < len(providers) - 1 {
			upstreamResponse.Body.Close()
			lastError = fmt.Errorf("%s returned status %d", provider.Name(), upstreamResponse.StatusCode)
			proxy.logger.Warn("provider returned retryable status, trying next", "provider", provider.Name(), "status", upstreamResponse.StatusCode, "attempt", i + 1)
			continue
		}

		defer upstreamResponse.Body.Close()

		copyHeaders(writer.Header(), upstreamResponse.Header)
		writer.Header().Set("X-Gateway-Provider", provider.Name())
		writer.WriteHeader(upstreamResponse.StatusCode)

		var written int64
		if streamReq.Stream {
			written, err = proxy.streamCopy(request.Context(), writer, upstreamResponse.Body, key)
		} else {
			respBody, readErr := io.ReadAll(upstreamResponse.Body)
			if readErr != nil {
				err = readErr
			} else {
				written, err = io.Copy(writer, bytes.NewReader(respBody))
				tokens := parseUsageTokens(respBody)
				if tokens > 0 {
					proxy.limiter.Charge(key, float64(tokens))
				}
			}
		}

		logLevel := slog.LevelInfo
		if err != nil {
			logLevel = slog.LevelWarn
		}

		proxy.logger.Log(request.Context(), logLevel, "proxied request",
			"provider", provider.Name(),
			"stream", streamReq.Stream,
			"upstream_status", upstreamResponse.StatusCode,
			"resp_bytes", written,
			"attempts", i + 1,
			"latency_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return
	}
	proxy.logger.Error("All providers failed", "err", lastError, "Number of providers", len(providers))
	http.Error(writer, "All upstream providers failed", http.StatusBadGateway)
}

func (proxy *Proxy) streamCopy(ctx context.Context, writer http.ResponseWriter, body io.Reader, key string) (int64, error) {
	flusher, canFlush := writer.(http.Flusher)
	if !canFlush {
		return io.Copy(writer, body)
	}


	tail := make([]byte, 0, usageTailBytes * 2)
	bytesInKB := 1024
	totalBufferKB := 4
	buffer := make([]byte, bytesInKB * totalBufferKB)
	var total int64
	var copyErr error

	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		if copyErr != nil {
			break
		}


		n, readErr := body.Read(buffer)
		if n > 0 {
			written, writeErr := writer.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			flusher.Flush()

			tail = append(tail, buffer[:n]...)
			if len(tail) > 2 * usageTailBytes {
				tail = tail[len(tail)-usageTailBytes:]
			}

		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}
	if tokens := parseUsageTokens(tail); tokens > 0 {
		proxy.limiter.Charge(key, float64(tokens))
	}
	return total, copyErr
}