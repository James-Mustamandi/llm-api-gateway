package proxy

import (
	"io"
	"os"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/James-Mustamandi/llm-api-gateway/internal/keystore"
	"github.com/James-Mustamandi/llm-api-gateway/internal/provider"
	"github.com/James-Mustamandi/llm-api-gateway/internal/ratelimit"
	"github.com/James-Mustamandi/llm-api-gateway/internal/secrets"

)

func TestFailoverToHealthyProvider(t *testing.T) {
	brokenCalled := false
	broken := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		brokenCalled = true
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer broken.Close()

	workingCalled := false
	working := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request){
		workingCalled = true
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		io.WriteString(writer, `{"id":"ok","choices":[{"message}": {"content":"hi"}]}`)
	}))
	defer working.Close()

	registry := provider.NewRegistry(
		provider.NewOpenAICompatible("broken", broken.URL, "k", nil),
		provider.NewOpenAICompatible("working", working.URL, "k", nil),
	)

	limiter := ratelimit.New(ratelimit.Config{Capacity: 1_000_000, RefillPerSecond: 1_000_000})
	masterKey := os.Getenv("GATEWAY_MASTER_KEY")
	encryptor, err := secrets.NewEncryptor(masterKey)
	if err != nil {
		t.Fatalf("error with master key")
	}

	store := keystore.NewMemoryStore(encryptor)

	proxy := New(&http.Client{Timeout: 5 * time.Second}, registry, limiter, slog.Default(), store)
	gateway := httptest.NewServer(http.HandlerFunc(proxy.HandleChatCompletions))
	defer gateway.Close()

	response, err := http.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"stream":false}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("Got status %d, want 200 - failover did not reach a working provider", response.StatusCode)
	}
	if !brokenCalled {
		t.Error("Broken provider was never called, it should have been tried first")
	}
	if !workingCalled {
		t.Error("Working provider was not never called, failover did not occur")
	}
	if !strings.Contains(string(responseBody), `"content":"hi"`) {
		t.Errorf("Unexpected body: %s", responseBody)
	}
	if got := response.Header.Get("X-Gateway-Provider"); got != "working" {
		t.Errorf("X-Gateway-Provider = %q, want %q", got, "working")
	}
}


func TestNoFailoverOnClientError(t *testing.T) {
	secondCalled := false
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		io.WriteString(writer, `{"error":"bad request"}`)
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		secondCalled = true
		writer.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	registry := provider.NewRegistry(
		provider.NewOpenAICompatible("first", first.URL, "k", nil),
		provider.NewOpenAICompatible("second", second.URL, "k", nil),
	)

	limiter := ratelimit.New(ratelimit.Config{Capacity: 1_000_000, RefillPerSecond: 1_000_000})
	masterKey := os.Getenv("GATEWAY_MASTER_KEY")
	encryptor, _ := secrets.NewEncryptor(masterKey)
	store := keystore.NewMemoryStore(encryptor)
	proxy := New(&http.Client{Timeout: 5 * time.Second}, registry, limiter, slog.Default(), store)
	gateway := httptest.NewServer(http.HandlerFunc(proxy.HandleChatCompletions))
	defer gateway.Close()

	response, err := http.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"stream":false}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400 - 400 (note: should pass), not faill over", response.StatusCode)
	}
	if secondCalled {
		t.Error("second provider was called - 400 is terminal and must not trigger failover")
	}


}