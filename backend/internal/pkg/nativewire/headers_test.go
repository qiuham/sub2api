package nativewire

import (
	"net/http"
	"testing"
)

func TestCopyRequestHeadersKeepsNativeFieldsButNotGatewaySecrets(t *testing.T) {
	src := http.Header{
		"X-Client-Request-Id": {"client-id"},
		"X-Future-Native":     {"value"},
		"Authorization":       {"Bearer client-key"},
		"X-Api-Key":           {"client-key"},
		"X-Forwarded-For":     {"client-ip"},
		"Connection":          {"keep-alive, X-Hop"},
		"X-Hop":               {"do-not-send"},
	}
	dst := http.Header{"Authorization": {"Bearer upstream-token"}}
	CopyRequestHeaders(dst, src)
	for key, want := range map[string]string{
		"Authorization":       "Bearer upstream-token",
		"X-Client-Request-Id": "client-id",
		"X-Future-Native":     "value",
	} {
		if got := dst.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"X-Api-Key", "X-Forwarded-For", "Connection", "X-Hop"} {
		if got := dst.Get(key); got != "" {
			t.Fatalf("%s leaked: %q", key, got)
		}
	}
}

func TestCopyResponseHeadersReplacesGatewaySignatureAndPreservesFutureFields(t *testing.T) {
	dst := http.Header{"X-Request-Id": {"gateway"}, "X-Frame-Options": {"DENY"}}
	src := http.Header{
		"Request-Id":            {"upstream"},
		"Anthropic-Ratelimit-X": {"42"},
		"Server-Timing":         {"edge;dur=1"},
		"Connection":            {"keep-alive, X-Hop"},
		"X-Hop":                 {"drop"},
		"Content-Length":        {"999"},
	}
	CopyResponseHeaders(dst, src)
	for key, want := range map[string]string{"Request-Id": "upstream", "Anthropic-Ratelimit-X": "42", "Server-Timing": "edge;dur=1"} {
		if got := dst.Get(key); got != want {
			t.Fatalf("%s=%q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"X-Request-Id", "X-Frame-Options", "Connection", "X-Hop", "Content-Length"} {
		if got := dst.Get(key); got != "" {
			t.Fatalf("unexpected %s=%q", key, got)
		}
	}
}
