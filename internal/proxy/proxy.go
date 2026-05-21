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


func (proxy *Proxy) HandleChatCompletions(writer http.ResponseWriter, response *http.Request) {
	body, err := io.ReadAll(response.Body)

	if err != nil {
		http.Error(writer, "failed to read request body", http.StatusBadRequest)
	}
	defer response.Body.Close()

	var streamReq streamRequest
	_ = json.Unmarshal(body, &streamReq)

	outReq, err := http.NewRequestWithContext(response.Context(), http.MethodPost, proxy.upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(writer, "Failed to build upstream request", http.StatusInternalServerError)
		return
	}

	copyHeaders(outReq.Header, response.Header)
	outReq.Header.Set("Authorization", "Bearer "+proxy.upstreamKey)
	if outReq.Header.Get("Content-Type") == "" {
		outReq.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := proxy.client.Do(outReq)

	if err != nil {

		if errors.Is(err, context.Canceled) {
			proxy.logger.Info("Client disconnected before upstream responded")
		}

		proxy.logger.Error("upstream request failed", "err", err)
		http.Error(writer, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(writer.Header(), resp.Header)
	writer.WriteHeader(resp.StatusCode)

	var written int64
	if streamReq.Stream {
		written, err = proxy.streamCopy(response.Context(), writer, resp.Body)
	} else {
		written, err = io.Copy(writer, resp.Body)
	}

	logLevel := slog.LevelInfo
	if err != nil {
		logLevel = slog.LevelWarn
	}

	proxy.logger.Log(response.Context(), logLevel, "proxied request",
		"stream", streamReq.Stream,
		"upstream_status", resp.StatusCode,
		"resp_bytes", written,
		"latency_ms", time.Since(start).Milliseconds(),
		"err", err,
	)
}

func (proxy *Proxy) streamCopy(ctx context.Context, writer http.ResponseWriter, body io.Reader) (int64, error) {
	flusher, canFlush := writer.(http.Flusher)
	if !canFlush {
		return io.Copy(writer, body)
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
			written, writeErr := writer.Write(buffer[:n])
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