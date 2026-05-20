package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type Proxy struct {
	client      *http.Client
	upstreamURL string
	upstreamKey string
	logger      *slog.Logger
}

func New(client *http.Client, upstreamURL, upstreamKey string, logger *slog.Logger) *Proxy {
	return &Proxy{
		client:      client,
		upstreamURL: upstreamURL,
		upstreamKey: upstreamKey,
		logger:      logger,
	}
}

func (p *Proxy) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)

	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
	}
	defer r.Body.Close()

	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, p.upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to build upstream request", http.StatusInternalServerError)
		return
	}

	copyHeaders(outReq.Header, r.Header)
	outReq.Header.Set("Authorization", "Bearer "+p.upstreamKey)
	if outReq.Header.Get("Content-Type") == "" {
		outReq.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := p.client.Do(outReq)

	if err != nil {
		p.logger.Error("upstream request failed", "err", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	written, _ := io.Copy(w, resp.Body)

	p.logger.Info("Proxied request",
		"upstream_status", resp.StatusCode,
		"resp_bytes", written,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}
