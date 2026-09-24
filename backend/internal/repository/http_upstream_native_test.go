package repository

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/nativewire"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestTLSFingerprintClientPoolRebuildsWhenProfileChanges(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	first := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	second := *first
	second.CipherSuites = append([]uint16(nil), first.CipherSuites...)
	second.CipherSuites[0], second.CipherSuites[1] = second.CipherSuites[1], second.CipherSuites[0]
	entry1, err := upstream.getClientEntryWithTLS("", 7, 1, first, service.HTTPUpstreamProfileOpenAI, false, false, true)
	require.NoError(t, err)
	entry1Again, err := upstream.getClientEntryWithTLS("", 7, 1, first, service.HTTPUpstreamProfileOpenAI, false, false, true)
	require.NoError(t, err)
	require.Same(t, entry1, entry1Again)
	entry2, err := upstream.getClientEntryWithTLS("", 7, 1, &second, service.HTTPUpstreamProfileOpenAI, false, false, true)
	require.NoError(t, err)
	require.NotSame(t, entry1, entry2, "a changed profile must not reuse a TLS transport with the old ClientHello")
}

func TestVerifiedNativeHTTPTransportDoesNotNegotiateHTTP2(t *testing.T) {
	for _, tc := range []struct{ family, version string }{
		{"claude", "2.1.281"}, {"codex", "0.156.1"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			profile := tlsfingerprint.VerifiedNativeProfile(tc.family, tc.version)
			require.NotNil(t, profile)
			require.Empty(t, profile.ALPNProtocols, "measured native ClientHello has no ALPN extension")
			transport, err := buildUpstreamTransportWithTLSFingerprint(poolSettings{}, nil, profile)
			require.NoError(t, err)
			defer transport.CloseIdleConnections()
			require.False(t, transport.ForceAttemptHTTP2)
		})
	}
}

func TestNativeTLSRequestRejectsUnfingerprintedFallback(t *testing.T) {
	upstream := NewHTTPUpstream(nil)
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	for _, test := range []struct{ target, proxy string }{
		{"http://example.test/v1/responses", ""},
		{"https://example.test/v1/responses", "https://proxy.example.test:8443"},
		{"https://example.test/v1/responses", "ftp://proxy.example.test:21"},
	} {
		req, err := http.NewRequestWithContext(nativewire.MarkRequest(context.Background()), http.MethodPost, test.target, nil)
		require.NoError(t, err)
		resp, err := upstream.DoWithTLS(req, test.proxy, 7, 1, profile)
		require.Error(t, err)
		require.Nil(t, resp)
	}
}

func TestNativeTLSClientPoolDisablesAutomaticCompression(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	nativeEntry, err := upstream.getClientEntryWithTLS("", 7, 1, profile, service.HTTPUpstreamProfileOpenAI, false, false, true)
	require.NoError(t, err)
	stockEntry, err := upstream.getClientEntryWithTLS("", 7, 1, profile, service.HTTPUpstreamProfileOpenAI, false, false, false)
	require.NoError(t, err)
	require.NotSame(t, nativeEntry, stockEntry)
	nativeTransport, ok := nativeEntry.client.Transport.(*req.Transport)
	require.True(t, ok)
	stockTransport, ok := stockEntry.client.Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, nativeTransport.DisableCompression)
	require.False(t, stockTransport.DisableCompression)
}

func TestNativeUpstreamKeepsEncodedResponseBytes(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write([]byte(`{"native":true}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	want := compressed.Bytes()
	var accepts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepts = append(accepts, r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(want)
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(nativewire.MarkRequest(context.Background()), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := doUpstreamRequest(client, req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
	withoutEncoding, err := http.NewRequestWithContext(nativewire.MarkRequest(context.Background()), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	second, err := doUpstreamRequest(client, withoutEncoding)
	require.NoError(t, err)
	defer func() { _ = second.Body.Close() }()
	secondBody, err := io.ReadAll(second.Body)
	require.NoError(t, err)
	require.Equal(t, want, secondBody)
	require.Equal(t, []string{"gzip", ""}, accepts, "Native transport must not invent Accept-Encoding")
}

func TestNativeUpstreamDoesNotFollowRedirects(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	client := &http.Client{}
	req, err := http.NewRequestWithContext(nativewire.MarkRequest(context.Background()), http.MethodGet, "https://example.test/", nil)
	require.NoError(t, err)
	derived := upstream.httpClientForUpstreamRequest(client, req)
	require.NotNil(t, derived.CheckRedirect)
	require.ErrorIs(t, derived.CheckRedirect(req, nil), http.ErrUseLastResponse)
}
