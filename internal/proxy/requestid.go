package proxy

import (
	"crypto/rand"
	"encoding/hex"
)

func newRequestID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

