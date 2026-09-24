package nativewire

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type headerCaptureListener struct {
	net.Listener
	mu          sync.Mutex
	first       string
	connections int
}

type headerCaptureConn struct {
	net.Conn
	parent *headerCaptureListener
	data   []byte
	done   bool
}

func (l *headerCaptureListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.connections++
	l.mu.Unlock()
	return &headerCaptureConn{Conn: c, parent: l}, nil
}

func (c *headerCaptureConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if !c.done {
		c.data = append(c.data, p[:n]...)
		if end := bytes.Index(c.data, []byte("\r\n\r\n")); end >= 0 {
			c.parent.mu.Lock()
			if c.parent.first == "" {
				c.parent.first = string(c.data[:end])
			}
			c.parent.mu.Unlock()
			c.done = true
			c.data = nil
		}
	}
	return n, err
}

func TestNativeHTTP1TransportWireOrderBodyAndPoolReuse(t *testing.T) {
	for _, test := range []struct {
		name, ua string
		headers  http.Header
		want     []string
	}{
		{"claude", "claude-cli/2.1.281 (external, cli)", http.Header{
			"Accept": {"application/json"}, "Content-Type": {"application/json"},
			"X-Stainless-Os": {"Linux"}, "Anthropic-Beta": {"fixture"},
			"authorization": {"Bearer fixture"}, "X-App": {"cli"},
		}, []string{"Accept", "Content-Type", "User-Agent", "X-Stainless-OS", "anthropic-beta", "authorization", "x-app", "Host", "Content-Length", "X-Custom"}},
		{"codex", "codex_cli_rs/0.156.1 (Linux; x86_64)", http.Header{
			"Session-Id": {"fixture-session"}, "Content-Type": {"application/json"},
			"Authorization": {"Bearer fixture"}, "Originator": {"codex_cli_rs"},
		}, []string{"session-id", "content-type", "authorization", "originator", "user-agent", "host", "content-length", "X-Custom"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var compressed bytes.Buffer
			zip := gzip.NewWriter(&compressed)
			_, err := zip.Write([]byte("fixture response"))
			require.NoError(t, err)
			require.NoError(t, zip.Close())
			bodies := make(chan []byte, 2)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				bodies <- body
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(compressed.Bytes())
			}))
			capture := &headerCaptureListener{Listener: server.Listener}
			server.Listener = capture
			server.Start()
			defer server.Close()
			transport := NewHTTP1Transport(&http.Transport{MaxIdleConnsPerHost: 2})
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			body := []byte("{ \"input\": \"unchanged\" }\n")
			for i := 0; i < 2; i++ {
				r, err := http.NewRequest(http.MethodPost, server.URL+"/fixture", bytes.NewReader(body))
				require.NoError(t, err)
				r.Header = test.headers.Clone()
				r.Header.Set("User-Agent", test.ua)
				r.Header["X-Custom"] = []string{"one", "two"}
				r.Header[req.HeaderOderKey] = []string{"malicious-order"}
				r.Header[req.PseudoHeaderOderKey] = []string{"malicious-pseudo"}
				original := r.Header.Clone()
				resp, err := client.Do(r)
				require.NoError(t, err)
				got, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, body, <-bodies)
				require.Equal(t, compressed.Bytes(), got, "Native must retain encoded response bytes")
				require.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
				require.False(t, resp.Uncompressed)
				require.Equal(t, original, r.Header, "serializer must not mutate caller headers")
			}
			capture.mu.Lock()
			defer capture.mu.Unlock()
			names := []string{}
			for _, line := range strings.Split(capture.first, "\r\n")[1:] {
				name, _, _ := strings.Cut(line, ":")
				if len(names) == 0 || names[len(names)-1] != name {
					names = append(names, name)
				}
			}
			require.Equal(t, test.want, names)
			require.Contains(t, capture.first, "X-Custom: one\r\nX-Custom: two")
			require.NotContains(t, capture.first, "__header_order__")
			require.NotContains(t, capture.first, "__pseudo_header_order__")
			require.NotContains(t, capture.first, "Accept-Encoding:")
			require.Equal(t, 1, capture.connections, "fully consumed bodies must preserve keep-alive")
			t.Logf("WIRE_NAMES=%v BODY_EQUAL=true COMPRESSED_RESPONSE_EQUAL=true CONNECTIONS=%d", names, capture.connections)
		})
	}
}

func TestNativeHTTP1TransportInheritsConfiguration(t *testing.T) {
	base := &http.Transport{MaxIdleConns: 17, MaxIdleConnsPerHost: 3, MaxConnsPerHost: 4,
		IdleConnTimeout: 5 * time.Second, ResponseHeaderTimeout: 6 * time.Second,
		TLSHandshakeTimeout: 7 * time.Second, ExpectContinueTimeout: 8 * time.Second,
		ReadBufferSize: 9000, WriteBufferSize: 10000, MaxResponseHeaderBytes: 11000}
	transport := NewHTTP1Transport(base)
	defer transport.CloseIdleConnections()
	require.Nil(t, transport.Proxy, "Native direct routing must ignore environment proxy defaults")
	require.Equal(t, base.MaxIdleConns, transport.MaxIdleConns)
	require.Equal(t, base.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	require.Equal(t, base.MaxConnsPerHost, transport.MaxConnsPerHost)
	require.Equal(t, base.IdleConnTimeout, transport.IdleConnTimeout)
	require.Equal(t, base.ResponseHeaderTimeout, transport.ResponseHeaderTimeout)
	require.Equal(t, base.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	require.Equal(t, base.ExpectContinueTimeout, transport.ExpectContinueTimeout)
	require.Equal(t, base.ReadBufferSize, transport.ReadBufferSize)
	require.Equal(t, base.WriteBufferSize, transport.WriteBufferSize)
	require.Equal(t, base.MaxResponseHeaderBytes, transport.MaxResponseHeaderBytes)
	require.True(t, transport.DisableCompression)
	require.False(t, transport.AutoDecompression)
}

func TestNativeHTTP1TransportCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	transport := NewHTTP1Transport(&http.Transport{})
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	r.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	done := make(chan error, 1)
	go func() {
		response, err := transport.RoundTrip(r)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach local upstream")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("request cancellation stalled")
	}
}

func TestNativeHTTP1TransportKeepsCustomTLSDialer(t *testing.T) {
	wantErr := errors.New("fixture dialer reached")
	called := make(chan string, 1)
	base := &http.Transport{DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		called <- network + " " + address
		return nil, wantErr
	}}
	transport := NewHTTP1Transport(base)
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://native.fixture.test/", nil)
	require.NoError(t, err)
	r.Header.Set("User-Agent", "claude-cli/2.1.281 (external, cli)")
	response, err := transport.RoundTrip(r)
	require.Nil(t, response)
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, "tcp native.fixture.test:443", <-called)
}
