package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)


func main() {

	// Setup structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions {
		Level: slog.LevelInfo,
	}))

	slog.SetDefault(logger)

	client := &http.Client {
		Timeout: 60 * time.Second,
	}


	mux := http.NewServeMux()

	// Handlers
	mux.HandleFunc("/healthz", func (w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/v1/chat/completions", chatHandler(client))


	server := &http.Server {
		Addr: ":8080",
		Handler: mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout: 120 * time.Second,
	}


	serverError := make(chan error, 1)

	go func() {
		slog.Info("Listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverError <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
		case err := <-serverError:
			slog.Error("server failed", "err", err)
			os.Exit(1)
		case sig := <-stop:
			slog.Info("Shutting down", "Signal", sig.String())

	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("Shut down cleanly")
}


func chatHandler(client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
		}
		defer r.Body.Close()

		// Outbound request to provider
		upstreamURL := "https://openrouter.ai/api/v1/chat/completions"
		outReq, err := http.NewRequestWithContext(
			r.Context(),
			http.MethodPost,
			upstreamURL,
			bytes.NewReader(body),
		)

		if err != nil {
			http.Error(w, "Failed to build upstream request", http.StatusInternalServerError)
			return
		}

		// Headers for provider
		outReq.Header.Set("Content-Type", "application/json")
		outReq.Header.Set("Authorization", "Bearer "+os.Getenv("OPENROUTER_API_KEY"))
		
		// Send the request
		start := time.Now()
		resp, err := client.Do(outReq)
		if err != nil {
			slog.Error("Upstream request failed", "err", err)
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy upstream status and body back to client
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		written, _ := io.Copy(w, resp.Body)

		slog.Info("proxied request", 
			"upstream_status", resp.StatusCode,
			"resp_bytes", written,
			"latency_ms", time.Since(start).Milliseconds(),
		)
	}
}

