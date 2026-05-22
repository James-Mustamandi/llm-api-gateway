package proxy

import (
	"net"
	"net/http"
	"strings"
)

func clientKey(request *http.Request) string {
	auth := request.Header.Get("Authorization")

	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		token := strings.TrimSpace(after)
		if token != "" {
			return "key:" + token
		}
	}

	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	return "ip:" + host
}