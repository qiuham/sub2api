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

type nativeCodexForwardCapture struct {
	httpUpstreamRecorder
	profile *tlsfingerprint.Profile
}

func (u *nativeCodexForwardCapture) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.profile = profile
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestNativeCodexFullForwardPreservesRequestAndResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	body := []byte(`{"model":"gpt-6-sol","input":[{"role":"user","content":"ping"}],"stream":true,"store":false}`)
	sse := "event: response.created\r\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\r\n\r\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"
	upstream := &nativeCodexForwardCapture{httpUpstreamRecorder: httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "Request-Id": {"upstream-rid"}},
		Body:       io.NopCloser(bytes.NewBufferString(sse)),
	}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	c.Request.Header.Set("version", "0.156.1")
	account := &Account{
		ID: 7, Name: "native-codex", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account"},
		Extra:       map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, body, upstream.lastBody)
	require.True(t, nativewire.IsRequest(upstream.lastReq.Context()))
	require.True(t, tlsfingerprint.MatchesVerifiedNativeProfile(upstream.profile, profile))
	require.Equal(t, "Bearer fixture-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, sse, rec.Body.String())
	require.Equal(t, "upstream-rid", rec.Header().Get("Request-Id"))
}

func TestNativeCodexForwardDoesNotNormalizeFastTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	body := []byte(`{"model":"gpt-6-sol","input":"ping","service_tier":"fast","stream":false}`)
	upstream := &nativeCodexForwardCapture{httpUpstreamRecorder: httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(`{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1}}`)),
	}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	c.Request.Header.Set("version", "0.156.1")
	account := &Account{ID: 7, Name: "native-codex", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account"},
		Extra:       map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, body, upstream.lastBody)
}

func TestNativeCodexForwardDoesNotRetryRejectedField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	body := []byte(`{"model":"gpt-6-sol","input":"ping","truncation":"auto","stream":false}`)
	upstreamError := `{"error":{"code":"unknown_parameter","message":"Unknown parameter: truncation","param":"truncation"}}`
	upstream := &nativeCodexForwardCapture{httpUpstreamRecorder: httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}, "Request-Id": {"upstream-error-id"}},
		Body: io.NopCloser(bytes.NewBufferString(upstreamError)),
	}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	c.Request.Header.Set("version", "0.156.1")
	account := &Account{ID: 7, Name: "native-codex", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-account"},
		Extra:       map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	_, _ = svc.Forward(context.Background(), c, account, body)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, body, upstream.lastBody)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, upstreamError, rec.Body.String())
	require.Equal(t, "upstream-error-id", rec.Header().Get("Request-Id"))
}

func TestNativeCodexNonStreamingResponseDoesNotConvertSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte("event: response.completed\r\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}\r\n\r\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "Request-Id": {"upstream-id"}}, Body: io.NopCloser(bytes.NewReader(body))}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"native_wire_mode": "native"}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	_, err := svc.handleNonStreamingResponsePassthrough(context.Background(), resp, c, account, "gpt-6-sol", "")
	require.NoError(t, err)
	require.Equal(t, body, rec.Body.Bytes())
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, "upstream-id", rec.Header().Get("Request-Id"))
}

func TestNativeCodexCompactPreservesClientAccept(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	c.Request.Header.Set("Accept", "text/event-stream")
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "fixture-account"},
		Extra:       map[string]any{"native_wire_mode": "native"}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, []byte(`{"model":"gpt-6-sol","input":[]}`), "fixture-token")
	require.NoError(t, err)
	require.Equal(t, "text/event-stream", req.Header.Get("Accept"))
	require.True(t, nativewire.IsRequest(req.Context()))
}

func TestNativeCodexCompressedNonStreamingResponseKeepsWireBodyAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var wire bytes.Buffer
	zw := gzip.NewWriter(&wire)
	_, err := zw.Write([]byte(`{"id":"resp_1","usage":{"input_tokens":2,"output_tokens":1}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"}},
		Body:   io.NopCloser(bytes.NewReader(wire.Bytes()))}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Extra: map[string]any{"native_wire_mode": "native"}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	result, err := svc.handleNonStreamingResponsePassthrough(context.Background(), resp, c, account, "gpt-6-sol", "")
	require.NoError(t, err)
	require.Equal(t, 2, result.usage.InputTokens)
	require.Equal(t, wire.Bytes(), rec.Body.Bytes())
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
}
