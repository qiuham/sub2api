package nativewire

import (
	"fmt"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/imroc/req/v3"
)

// These layouts describe measured CLI requests, not arbitrary SDK clients.
// Unknown headers retain their values and are appended deterministically; their
// original position is unavailable after net/http has parsed ingress headers.
var claudeHTTP1Order = []string{
	"Accept", "Content-Type", "User-Agent", "X-Claude-Code-Session-Id",
	"X-Stainless-Arch", "X-Stainless-Lang", "X-Stainless-OS",
	"X-Stainless-Package-Version", "X-Stainless-Retry-Count", "X-Stainless-Runtime",
	"X-Stainless-Runtime-Version", "X-Stainless-Timeout", "anthropic-beta",
	"anthropic-dangerous-direct-browser-access", "anthropic-version", "authorization",
	"x-api-key", "x-app", "Connection", "Host", "Accept-Encoding", "Content-Length",
}

var codexHTTP1Order = []string{
	"x-codex-beta-features", "x-codex-window-id", "x-codex-turn-metadata",
	"x-openai-internal-codex-responses-lite", "x-client-request-id", "session-id",
	"thread-id", "accept", "content-type", "authorization", "originator",
	"user-agent", "host", "content-length",
}

var codexWSHTTP1Order = []string{
	"Host", "Connection", "Upgrade", "Sec-WebSocket-Version", "Sec-WebSocket-Key",
	"authorization", "user-agent", "originator", "openai-beta", "x-codex-turn-metadata",
	"x-codex-beta-features", "x-client-request-id", "session-id", "thread-id",
	"x-codex-window-id", "sec-websocket-extensions",
}

// NewHTTP1Transport reuses req's maintained HTTP serializer and the existing
// transport's dialers/pool settings. It is only installed for Native accounts.
// A bounded header-only adapter fixes req's synthesized header casing after
// TLS. Bodies, SSE events and WebSocket frames remain byte-for-byte untouched.
// This does not imply equal TCP/TLS framing, timing or unmeasured OAuth layouts.
func NewHTTP1Transport(base *http.Transport) *req.Transport {
	hasCustomTLS := base.DialTLSContext != nil
	t := req.NewTransport().EnableForceHTTP1().DisableAutoDecode()
	t.Proxy = base.Proxy // nil must remain nil, rather than ProxyFromEnvironment.
	t.DialContext = withHTTP1HeaderCaseDialer(base.DialContext)
	if hasCustomTLS {
		// Wrap the returned TLS connection, never the encrypted socket beneath it.
		t.DialTLSContext = withHTTP1HeaderCaseDialer(base.DialTLSContext)
	}
	if base.TLSClientConfig != nil {
		t.TLSClientConfig = base.TLSClientConfig.Clone()
	}
	t.TLSHandshakeTimeout = base.TLSHandshakeTimeout
	t.DisableKeepAlives = base.DisableKeepAlives
	t.DisableCompression = true
	t.AutoDecompression = false
	t.MaxIdleConns = base.MaxIdleConns
	t.MaxIdleConnsPerHost = base.MaxIdleConnsPerHost
	t.MaxConnsPerHost = base.MaxConnsPerHost
	t.IdleConnTimeout = base.IdleConnTimeout
	t.ResponseHeaderTimeout = base.ResponseHeaderTimeout
	t.ExpectContinueTimeout = base.ExpectContinueTimeout
	t.MaxResponseHeaderBytes = base.MaxResponseHeaderBytes
	t.ReadBufferSize = base.ReadBufferSize
	t.WriteBufferSize = base.WriteBufferSize
	t.ProxyConnectHeader = base.ProxyConnectHeader.Clone()
	t.GetProxyConnectHeader = base.GetProxyConnectHeader
	t.OnProxyConnectResponse = base.OnProxyConnectResponse
	t.WrapRoundTripFunc(func(next http.RoundTripper) req.HttpRoundTripFunc {
		return func(original *http.Request) (*http.Response, error) {
			out, err := orderedHTTP1Request(original)
			if err == nil && original.URL != nil && original.URL.Scheme == "https" && !hasCustomTLS {
				err = fmt.Errorf("native HTTPS transport requires its custom TLS dialer")
			}
			if err != nil {
				if original.Body != nil {
					_ = original.Body.Close()
				}
				return nil, err
			}
			mode := http1CaseUnchanged
			if openai.IsCodexOfficialClientRequestStrict(out.UserAgent()) {
				mode = http1CaseCodexHTTP
				if strings.EqualFold(out.Header.Get("Upgrade"), "websocket") {
					mode = http1CaseCodexWS
				}
			}
			// GotConn fires for every checkout, not only newly dialed connections.
			// WithClientTrace composes with existing timing/accounting hooks.
			trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
				if conn, ok := info.Conn.(*http1HeaderCaseConn); ok {
					conn.begin(mode)
				}
			}}
			out = out.WithContext(httptrace.WithClientTrace(out.Context(), trace))
			response, err := next.RoundTrip(out)
			if response != nil {
				response.Request = original
			}
			return response, err
		}
	})
	return t
}

func orderedHTTP1Request(original *http.Request) (*http.Request, error) {
	ua := original.UserAgent()
	var layout []string
	switch {
	case strings.HasPrefix(strings.ToLower(ua), "claude-cli/"):
		layout = claudeHTTP1Order
	case openai.IsCodexOfficialClientRequestStrict(ua):
		layout = codexHTTP1Order
		if strings.EqualFold(original.Header.Get("Upgrade"), "websocket") {
			layout = codexWSHTTP1Order
		}
	default:
		return nil, fmt.Errorf("native HTTP/1 header layout requires a supported CLI User-Agent")
	}
	out := original.Clone(original.Context())
	out.Header = make(http.Header, len(original.Header)+1)
	names := make(map[string]string, len(layout))
	order := append([]string(nil), layout...)
	for _, name := range layout {
		names[strings.ToLower(name)] = name
	}
	// The library handles these fields outside Header and looks them up in
	// canonical form. Renaming them would emit duplicates or a default UA.
	for _, name := range []string{"Host", "User-Agent", "Content-Length", "Transfer-Encoding", "Trailer"} {
		names[strings.ToLower(name)] = name
	}
	keys := make([]string, 0, len(original.Header))
	for key := range original.Header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lower := strings.ToLower(key)
		if lower == req.HeaderOderKey || lower == req.PseudoHeaderOderKey {
			continue // Never accept transport configuration from HTTP input.
		}
		name, known := names[lower]
		if !known {
			name = key
			names[lower] = name
			order = append(order, name)
		}
		out.Header[name] = append(out.Header[name], original.Header[key]...)
	}
	// req's sorter needs an index for generated framing headers as well.
	order = append(order, "Transfer-Encoding", "Trailer")
	if _, exists := names["connection"]; !exists {
		order = append(order, "Connection")
	}
	out.Header[req.HeaderOderKey] = order
	return out, nil
}
