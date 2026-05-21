package proxy

import (
	"bytes"
	"encoding/json"
	"context"
	"errors"
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


type streamRequest struct {
	Stream bool `json:"stream"`
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

	var sr streamRequest
	_ = json.Unmarshal(body, &sr)

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

		if errors.Is(err, context.Canceled) {
			p.logger.Info("Client disconnected before upstream responded")
		}

		p.logger.Error("upstream request failed", "err", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	var written int64
	if sr.Stream {
		written, err = p.streamCopy(r.Context(), w, resp.Body)
	} else {
		written, err = io.Copy(w, resp.Body)
	}

	logLevel := slog.LevelInfo
	if err != nil {
		logLevel = slog.LevelWarn
	}

	p.logger.Log(r.Context(), logLevel, "proxied request",
		"stream", sr.Stream,
		"upstream_status", resp.StatusCode,
		"resp_bytes", written,
		"latency_ms", time.Since(start).Milliseconds(),
		"err", err,
	)
}

func (p *Proxy) streamCopy(ctx context.Context, w http.ResponseWriter, body io.Reader) (int64, error) {
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		return io.Copy(w, body)
	}

	bytesInKB := 1024
	totalBufferKB := 4
	buffer := make([]byte, bytesInKB * totalBufferKB)
	var total int64
	for {

		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		n, readErr := body.Read(buffer)
		if n > 0 {
			written, writeErr := w.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			flusher.Flush()
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}