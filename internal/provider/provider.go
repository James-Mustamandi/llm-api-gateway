package provider

import (
	"net/http"
)

type Provider interface {
	// Stable indentifer for logging + health metrics
	Name() string

	// Returns the full upstream url
	// e.g /v1/chat/completions -> https://openarouter.ai/api/v1/chat/completions
	Endpoint(inboundPath string) string

	// Attach provider's credentials to an outbound request
	Authorize(req *http.Request)
}