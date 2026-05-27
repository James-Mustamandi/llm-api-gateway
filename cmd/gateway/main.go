package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/James-Mustamandi/llm-api-gateway/internal/keystore"
	"github.com/James-Mustamandi/llm-api-gateway/internal/provider"
	"github.com/James-Mustamandi/llm-api-gateway/internal/proxy"
	"github.com/James-Mustamandi/llm-api-gateway/internal/ratelimit"
	"github.com/James-Mustamandi/llm-api-gateway/internal/secrets"
	"github.com/James-Mustamandi/llm-api-gateway/internal/health"

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

	openrouter := provider.NewOpenAICompatible(
		"openrouter",
		"https://openrouter.ai/api/v1",
		upstreamKey,
		map[string]string {
			"HTTP-Referer": "https://github.com/James-Mustamandi/llm-api-gateway",
			"X-Title":		"llm-api-gateway",
		},
	)

	registry := provider.NewRegistry(openrouter)

	limiter := ratelimit.New(ratelimit.Config{
		Capacity: 1_000_000,
		RefillPerSecond: 1_000_000,
	})

	masterKey := os.Getenv("GATEWAY_MASTER_KEY")
	encryptor, err := secrets.NewEncryptor(masterKey)
	if err != nil {
		logger.Error("Invalid master key", "error", err)
		os.Exit(1)
	}

	store := keystore.NewMemoryStore(encryptor)
	if seed := os.Getenv("GATEWAY_SEED_KEYS"); seed != "" {
		var seeded map[string]map[string]string
		if err := json.Unmarshal([]byte(seed), &seeded); err != nil {
			logger.Error("invalid GATEWAY_SEED_KEYS json", "err", err)
			os.Exit(1)
		}

		for gatewayKey, providers := range seeded {
			for providerName, vendorKey := range providers {
				if err := store.Set(gatewayKey, providerName, vendorKey); err != nil {
					logger.Error("seeding key failed", "gateway_key", gatewayKey, "provider", providerName, "error", err)
					os.Exit(1)
				}

			}
		}
		logger.Info("seeded BYOK keys", "clients", len(seeded))
	}

	failureThresholdRetries := 5
	trackerTimeout := 5.0 * time.Second
	tracker := health.NewTracker(failureThresholdRetries, trackerTimeout)

	proxy := proxy.New(
		client,
		registry,
		limiter,
		logger,
		store,
		tracker,
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
