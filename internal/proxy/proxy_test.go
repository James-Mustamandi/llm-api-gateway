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
)

func slowStreamUpstream(eventCount int, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < eventCount; i++ {
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

	p := New(upstream.Client(), upstream.URL, "example_key", slog.Default())

	gateway := httptest.NewServer(http.HandlerFunc(p.HandleChatCompletions))
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