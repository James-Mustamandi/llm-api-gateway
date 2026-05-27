package provider

import (
	"net/http"
	"strings"
)

type OpenAICompatible struct {
	name string
	baseURL string
	apiKey string
	extraHeaders map[string]string
}


func NewOpenAICompatible(name, baseURL, apiKey string, extraHeaders map[string]string) *OpenAICompatible {
	return &OpenAICompatible{
		name: name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey: apiKey,
		extraHeaders: extraHeaders,
	}
}

func (openAICompatible *OpenAICompatible) Name() string {
	return openAICompatible.name
}

func (openAICompatible* OpenAICompatible) Endpoint(inboundPath string) string {
	path := strings.TrimPrefix(inboundPath, "/v1")
	return openAICompatible.baseURL + path
}

func (openAICompatible *OpenAICompatible) AuthorizeWith(req *http.Request, vendorKey string) {
	req.Header.Set("Authorization", "Bearer "+vendorKey)
	for key, value := range openAICompatible.extraHeaders {
		req.Header.Set(key, value)
	}
}

func (openAICompatible *OpenAICompatible) FallbackKey() string {
	return openAICompatible.apiKey
}