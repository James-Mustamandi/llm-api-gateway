package main

import (
	"context"
	"errors"
	"github.com/James-Mustamandi/llm-api-gateway/internal/proxy"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {

	// Setup structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	slog.SetDefault(logger)

	upstreamKey := os.Getenv("OPENROUTER_API_KEY")
	if upstreamKey == "" {
		logger.Error("OPENROUTER_API_KEY not set")
		os.Exit(1)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	proxy := proxy.New(
		client,
		"https://openrouter.ai/api/v1/chat/completions",
		upstreamKey,
		logger,
	)

	mux := http.NewServeMux()

	// Handlers
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/v1/chat/completions", proxy.HandleChatCompletions)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
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
	shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("Shut down cleanly")
}
