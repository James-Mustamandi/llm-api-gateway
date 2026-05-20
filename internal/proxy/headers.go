package proxy

import (
	"net/http"
	"strings"
)

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func copyHeaders(dest, src http.Header) {
	connectionHeaders := map[string]bool{}
	for _, value := range src.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				connectionHeaders[http.CanonicalHeaderKey(name)] = true
			}
		}
	}

	for name, values := range src {
		canonical := http.CanonicalHeaderKey(name)
		if hopByHopHeaders[canonical] || connectionHeaders[canonical] {
			continue
		}
		for _, v := range values {
			dest.Add(canonical, v)
		}
	}

}
