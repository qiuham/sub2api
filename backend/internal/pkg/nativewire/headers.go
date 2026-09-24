package nativewire

import (
	"net/http"
	"strings"
)

// CopyRequestHeaders keeps native client end-to-end headers while withholding
// gateway credentials, hop-by-hop fields, and ingress proxy metadata.
func CopyRequestHeaders(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	blocked := map[string]bool{}
	for _, value := range src.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			blocked[strings.ToLower(strings.TrimSpace(token))] = true
		}
	}
	for key, values := range src {
		lower := strings.ToLower(strings.TrimSpace(key))
		if blocked[lower] || shouldDropNativeRequestHeader(lower) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func shouldDropNativeRequestHeader(key string) bool {
	switch key {
	case "authorization", "proxy-authorization", "proxy-authenticate", "x-api-key", "x-goog-api-key",
		"connection", "proxy-connection", "keep-alive", "transfer-encoding", "te", "trailer", "upgrade",
		"host", "content-length", "cookie", "forwarded", "via", "x-real-ip":
		return true
	}
	return strings.HasPrefix(key, "x-forwarded-") || strings.HasPrefix(key, "x-sub2api-") || strings.HasPrefix(key, "sec-websocket-")
}

// CopyResponseHeaders replaces gateway-generated headers with upstream
// end-to-end headers. Framing is left to net/http, since the body can be
// consumed for accounting before it is written downstream.
func CopyResponseHeaders(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for key := range dst {
		delete(dst, key)
	}
	blocked := map[string]bool{}
	for _, value := range src.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			blocked[strings.ToLower(strings.TrimSpace(token))] = true
		}
	}
	for key, values := range src {
		lower := strings.ToLower(key)
		if blocked[lower] || lower == "connection" || lower == "content-length" || lower == "transfer-encoding" || lower == "keep-alive" || lower == "upgrade" || lower == "proxy-authenticate" || lower == "proxy-authorization" || lower == "trailer" {
			continue
		}
		dst[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
}
