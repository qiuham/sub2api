package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeTLSProfileVersionAndBindingGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, family, version, ua, declared string
		platform                            string
	}{
		{"claude", "claude", "2.1.280", "claude-cli/2.1.280 (external, cli)", "", PlatformAnthropic},
		{"claude-current", "claude", "2.1.281", "claude-cli/2.1.281 (external, cli)", "", PlatformAnthropic},
		{"codex", "codex", "0.155.1", "codex_cli_rs/0.155.1 (Linux; x86_64)", "0.155.1", PlatformOpenAI},
		{"codex-current", "codex", "0.156.1", "codex_cli_rs/0.156.1 (Linux; x86_64)", "0.156.1", PlatformOpenAI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verified := tlsfingerprint.VerifiedNativeProfile(tc.family, tc.version)
			require.NotNil(t, verified)
			profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
				7: {ID: 7, Name: "verified", CipherSuites: verified.CipherSuites, Curves: verified.Curves,
					PointFormats: verified.PointFormats, SignatureAlgorithms: verified.SignatureAlgorithms,
					ALPNProtocols: verified.ALPNProtocols, SupportedVersions: verified.SupportedVersions,
					KeyShareGroups: verified.KeyShareGroups, PSKModes: verified.PSKModes, Extensions: verified.Extensions},
			}}
			account := &Account{Platform: tc.platform, Type: AccountTypeOAuth, Extra: map[string]any{
				"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7,
			}}
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			request.Header.Set("User-Agent", tc.ua)
			if tc.declared != "" {
				request.Header.Set("version", tc.declared)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = request
			matched, err := nativeTLSProfile(c, account, profiles)
			require.NoError(t, err)
			require.True(t, tlsfingerprint.MatchesVerifiedNativeProfile(matched, verified))

			account.Extra["tls_fingerprint_profile_id"] = 99
			_, err = nativeTLSProfile(c, account, profiles)
			require.Error(t, err, "missing bound template must reject")
			account.Extra["tls_fingerprint_profile_id"] = 7
			account.Extra["enable_tls_fingerprint"] = false
			_, err = nativeTLSProfile(c, account, profiles)
			require.Error(t, err, "disabled TLS must reject")
			account.Extra["enable_tls_fingerprint"] = true
			request.Header.Set("User-Agent", tc.ua+"-unknown")
			// A suffix cannot change the parsed semantic version; use an unknown version.
			if tc.family == "claude" {
				request.Header.Set("User-Agent", "claude-cli/9.9.9")
			} else {
				request.Header.Set("User-Agent", "codex_cli_rs/9.9.9")
				request.Header.Set("version", "9.9.9")
			}
			_, err = nativeTLSProfile(c, account, profiles)
			require.Error(t, err, "unknown version must reject")
		})
	}
}

func TestNativeClaudeRequestPreservesBodyAndBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	input := []byte(`{"model":"claude-sonnet-4-5","system":[{"type":"text","text":"hello"}],"messages":[],"stream":false}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.280 (external, cli)")
	c.Request.Header.Set("anthropic-beta", "foo-2025-01-01")
	c.Request.Header.Set("x-client-request-id", "original-id")
	svc := &GatewayService{cfg: &config.Config{}}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"native_wire_mode": "native"}}
	req, wireBody, err := svc.buildUpstreamRequest(context.Background(), c, account, input, "oauth-token", "oauth", "claude-sonnet-4-5", false, false)
	require.NoError(t, err)
	require.Equal(t, input, wireBody)
	actualBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, input, actualBody)
	require.Equal(t, "foo-2025-01-01", getHeaderRaw(req.Header, "anthropic-beta"))
	require.Equal(t, "original-id", getHeaderRaw(req.Header, "x-client-request-id"))
}

func TestNativeClaudeCountTokensPreservesBodyAndBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	input := []byte(`{"model":"claude-sonnet-4-5","messages":[],"temperature":0.5}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.280")
	c.Request.Header.Set("anthropic-beta", "foo-2025-01-01")
	svc := &GatewayService{cfg: &config.Config{}}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"native_wire_mode": "native"}}
	req, wireBody, err := svc.buildCountTokensRequest(context.Background(), c, account, input, "oauth-token", "oauth", "claude-sonnet-4-5", false)
	require.NoError(t, err)
	require.Equal(t, input, wireBody)
	actualBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, input, actualBody)
	require.Equal(t, "foo-2025-01-01", getHeaderRaw(req.Header, "anthropic-beta"))
}

type nativeUpstreamRecorder struct {
	plain int
	tls   int
	seen  *tlsfingerprint.Profile
}

func (r *nativeUpstreamRecorder) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	r.plain++
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func (r *nativeUpstreamRecorder) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, p *tlsfingerprint.Profile) (*http.Response, error) {
	r.tls++
	r.seen = p
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func TestNativeCodexNormalUpstreamUsesBoundTLS(t *testing.T) {
	verified := tlsfingerprint.VerifiedNativeProfile("codex", "0.155.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: "codex", CipherSuites: verified.CipherSuites, Curves: verified.Curves,
			PointFormats: verified.PointFormats, SignatureAlgorithms: verified.SignatureAlgorithms,
			SupportedVersions: verified.SupportedVersions, KeyShareGroups: verified.KeyShareGroups,
			PSKModes: verified.PSKModes, Extensions: verified.Extensions},
	}}
	upstream := &nativeUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream, tlsFPProfileService: profiles}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7,
	}}
	req := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	req.Header.Set("User-Agent", "codex_cli_rs/0.155.1 (Linux; x86_64)")
	_, err := svc.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.tls)
	require.Zero(t, upstream.plain)
	require.True(t, tlsfingerprint.MatchesVerifiedNativeProfile(upstream.seen, verified))

	account.Extra["native_wire_mode"] = "sub2api"
	_, err = svc.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.plain)
}
