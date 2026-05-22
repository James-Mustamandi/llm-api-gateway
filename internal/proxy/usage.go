package proxy

import (
	"bytes"
	"encoding/json"
)

type usageEnvelope struct {
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}


func parseUsageTokens(tail []byte) int {
	var usageEnv usageEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(tail), &usageEnv); err == nil {
		if usageEnv.Usage.TotalTokens > 0 {
			return usageEnv.Usage.TotalTokens
		}
	}

	lines := bytes.Split(tail, []byte("\n"))

	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		payload, ok := bytes.CutPrefix(line, []byte("data: "))
		if !ok {
			continue
		}

		if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
			continue
		}
		if !bytes.Contains(payload, []byte("usage")) {
			continue
		}

		if err := json.Unmarshal(payload, &usageEnv); err != nil {
			continue
		}

		if usageEnv.Usage.TotalTokens > 0 {
			return usageEnv.Usage.TotalTokens
		}
	}
	return 0
}