package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
)


type HealthReporter interface {
	Statuses() map[string]string
}

type Handler struct {
	counters *Counters
	health HealthReporter
}

func NewHandler(counters *Counters, health HealthReporter) *Handler {
	return &Handler{counters: counters, health: health}
}

// Using Prometheus text exposition format...
func (handler *Handler) Prometheus(writer http.ResponseWriter, reader *http.Request) {
	snapshot := handler.counters.Snapshot()
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")

	fmt.Fprintf(writer, "# HELP gateway_requests_total Total requests received.\n")
	fmt.Fprintf(writer, "# TYPE gateway_requests_total counter\n")
	fmt.Fprintf(writer, "gateway_requests_total %d\n", snapshot.RequestsTotal)

	fmt.Fprintf(writer, "# HELP gateway_rate_limited_total Requests rejected by ratelimiting.\n")
	fmt.Fprintf(writer, "# TYPE gateway_rate_limited_total counter\n")
	fmt.Fprintf(writer, "gateway_rate_limited_total %d\n", snapshot.RateLimited)

	fmt.Fprintf(writer, "# HELP gateway_tokens_total Total tokens proxied.\n")
	fmt.Fprintf(writer, "# TYPE gateway_tokens_total counter\n")
	fmt.Fprintf(writer, "gateway_tokens_total %d\n", snapshot.TokensTotal)

	fmt.Fprintf(writer, "# HELP gateway_provider_requests_total Successful requests per provider. \n")
	fmt.Fprintf(writer, "# TYPE gateway_provider_requests_total counter\n")

	for name, count := range snapshot.ProviderSuccess {
		fmt.Fprintf(writer, "gateway_provider_requests_total{provider=%q} %d\n", name, count)
	}

	for name, count := range snapshot.ProviderFailure {
		fmt.Fprintf(writer, "gateawy_provider_failures_total{provider=%q} %d\n", name, count)
	}

	fmt.Fprintf(writer, "# HELP gateway_circuit_state Circuit breaker state per provider.\n")
	fmt.Fprintf(writer, "# TYPE gateway_circuit_state gauge\n")
	for name, state := range handler.health.Statuses() {
		fmt.Fprintf(writer, "gateway_circuit_state{provider=%q, state=%q} 1\n", name, state)
	}
}

func (handler *Handler) JSON(writer http.ResponseWriter, request *http.Request) {
	out := map[string]any{
		"counters": handler.counters.Snapshot(),
		"circuits": handler.health.Statuses(),
	}
	writer.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(out)
}
