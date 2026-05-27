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

	// Attaches vendor key to request using provider's auth scheme, proxy decides whcih key (per-client BYOK)
	// provider decides how to attach it (bearer header, x-api-key, etc)
	AuthorizeWith(req *http.Request, vendorKey string)

	// default provider key
	FallbackKey() string

}