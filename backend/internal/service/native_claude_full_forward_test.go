package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/nativewire"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeClaudeFullForwardPreservesRequestAndResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("claude", "2.1.281")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	body := nativeClaudeTelemetryTestBody(false)
	sse := "event: message_start\r\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}\r\n\r\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	upstream := &nativeCodexForwardCapture{httpUpstreamRecorder: httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "Request-Id": {"upstream-rid"}},
		Body:       io.NopCloser(bytes.NewBufferString(sse)),
	}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.281 (external, cli)")
	c.Request.Header.Set("anthropic-beta", "foo-2025-01-01")
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	account := &Account{
		ID: 7, Name: "native-claude", Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Concurrency: 1, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "fixture-token"},
		Extra: map[string]any{
			"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7,
		},
	}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, body, upstream.lastBody)
	require.True(t, tlsfingerprint.MatchesVerifiedNativeProfile(upstream.profile, profile))
	require.Equal(t, "Bearer fixture-token", getHeaderRaw(upstream.lastReq.Header, "authorization"))
	require.Equal(t, "foo-2025-01-01", getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"))
	require.True(t, nativewire.IsRequest(upstream.lastReq.Context()))
	require.Equal(t, sse, rec.Body.String())
	require.Equal(t, "upstream-rid", rec.Header().Get("Request-Id"))

	upstreamError := `{"type":"error","error":{"type":"invalid_request_error","message":"fixture error"}}`
	upstream.resp = &http.Response{StatusCode: http.StatusBadRequest,
		Header: http.Header{"Content-Type": {"application/json"}, "Request-Id": {"upstream-error-id"}},
		Body:   io.NopCloser(bytes.NewBufferString(upstreamError))}
	errorRec := httptest.NewRecorder()
	errorContext, _ := gin.CreateTestContext(errorRec)
	errorContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	errorContext.Request.Header.Set("Content-Type", "application/json")
	errorContext.Request.Header.Set("User-Agent", "claude-cli/2.1.281 (external, cli)")
	errorContext.Request.Header.Set("anthropic-beta", "foo-2025-01-01")
	parsedError, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	_, err = svc.Forward(context.Background(), errorContext, account, parsedError)
	require.Error(t, err)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, body, upstream.lastBody)
	require.Equal(t, http.StatusBadRequest, errorRec.Code)
	require.Equal(t, upstreamError, errorRec.Body.String())
	require.Equal(t, "upstream-error-id", errorRec.Header().Get("Request-Id"))
}

func TestNativeClaudeErrorPreservesUpstreamBodyAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Header("X-Request-Id", "gateway-id")
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"fixture error"}}`)
	resp := &http.Response{StatusCode: http.StatusBadRequest,
		Header: http.Header{"Content-Type": {"application/json"}, "Request-Id": {"upstream-id"}, "Server-Timing": {"edge;dur=1"}},
		Body:   io.NopCloser(bytes.NewReader(body))}
	account := &Account{ID: 7, Name: "native-claude", Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{"native_wire_mode": "native"}}
	svc := &GatewayService{cfg: &config.Config{}}
	_, _ = svc.handleErrorResponse(context.Background(), resp, c, account)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, body, rec.Body.Bytes())
	require.Equal(t, "upstream-id", rec.Header().Get("Request-Id"))
	require.Equal(t, "", rec.Header().Get("X-Request-Id"))
	require.Equal(t, "edge;dur=1", rec.Header().Get("Server-Timing"))
}

func TestNativeClaudeNonStreamingResponsePreservesBytesAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := []byte("{\n  \"type\": \"message\", \"usage\": {\"input_tokens\": 2, \"output_tokens\": 1, \"cache_creation\": {\"ephemeral_5m_input_tokens\": 3, \"ephemeral_1h_input_tokens\": 0}}\n}\n")
	resp := &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json; charset=fixture"}, "Request-Id": {"upstream-id"}},
		Body:   io.NopCloser(bytes.NewReader(body))}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{"native_wire_mode": "native", "cache_ttl_override_enabled": true, "cache_ttl_override_target": "1h"}}
	svc := &GatewayService{cfg: &config.Config{}}
	svc.cfg.Security.ResponseHeaders.Enabled = true
	usage, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "claude-sonnet-4-5", "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, 2, usage.InputTokens)
	require.Equal(t, 3, usage.CacheCreation5mTokens)
	require.Equal(t, body, rec.Body.Bytes())
	require.Equal(t, "application/json; charset=fixture", rec.Header().Get("Content-Type"))
	require.Equal(t, "upstream-id", rec.Header().Get("Request-Id"))
}

func TestNativeClaudeCountTokensPreservesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("claude", "2.1.281")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"ping"}]}`)
	account := &Account{ID: 7, Name: "native-claude", Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Concurrency: 1, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "fixture-token"},
		Extra:       map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7}}
	responseBody := []byte("{\n  \"input_tokens\": 5\n}\n")
	upstream := &nativeCodexForwardCapture{httpUpstreamRecorder: httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json; charset=fixture"}, "Request-Id": {"count-id"}},
		Body: io.NopCloser(bytes.NewReader(responseBody)),
	}}}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.281 (external, cli)")
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	require.NoError(t, svc.ForwardCountTokens(context.Background(), c, account, parsed))
	require.Equal(t, body, upstream.lastBody)
	require.True(t, nativewire.IsRequest(upstream.lastReq.Context()))
	require.Equal(t, responseBody, rec.Body.Bytes())
	require.Equal(t, "count-id", rec.Header().Get("Request-Id"))
	require.Equal(t, "application/json; charset=fixture", rec.Header().Get("Content-Type"))

	errorBody := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"count fixture"}}`)
	upstream.resp = &http.Response{StatusCode: http.StatusBadRequest,
		Header: http.Header{"Content-Type": {"application/json"}, "Request-Id": {"count-error-id"}},
		Body:   io.NopCloser(bytes.NewReader(errorBody))}
	errorRec := httptest.NewRecorder()
	errorContext, _ := gin.CreateTestContext(errorRec)
	errorContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	errorContext.Request.Header.Set("User-Agent", "claude-cli/2.1.281 (external, cli)")
	parsedError, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	require.Error(t, svc.ForwardCountTokens(context.Background(), errorContext, account, parsedError))
	require.Equal(t, http.StatusBadRequest, errorRec.Code)
	require.Equal(t, errorBody, errorRec.Body.Bytes())
	require.Equal(t, "count-error-id", errorRec.Header().Get("Request-Id"))
}

func TestNativeClaudeCompressedNonStreamingResponseKeepsWireBodyAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var wire bytes.Buffer
	zw := gzip.NewWriter(&wire)
	_, err := zw.Write([]byte(`{"type":"message","usage":{"input_tokens":2,"output_tokens":1}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"}},
		Body:   io.NopCloser(bytes.NewReader(wire.Bytes()))}
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{"native_wire_mode": "native"}}
	svc := &GatewayService{cfg: &config.Config{}}
	usage, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "claude-sonnet-4-5", "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, 2, usage.InputTokens)
	require.Equal(t, wire.Bytes(), rec.Body.Bytes())
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
}
