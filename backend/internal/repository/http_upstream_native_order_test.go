package repository

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/nativewire"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Exercise the actual account client pool, not a Request.Write approximation.
// The loopback server is plaintext so this test isolates HTTP serialization;
// ClientHello fidelity is covered separately in pkg/tlsfingerprint.
func TestNativeClientPoolUsesOrderedHTTP1Wire(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "stock"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer func() { _ = listener.Close() }()
			type capture struct {
				names []string
				body  []byte
				err   error
			}
			captured := make(chan capture, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					captured <- capture{err: err}
					return
				}
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(conn)
				var header strings.Builder
				names := []string{}
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						captured <- capture{err: err}
						return
					}
					_, _ = header.WriteString(line)
					if line == "\r\n" {
						break
					}
					if name, _, ok := strings.Cut(line, ":"); ok {
						names = append(names, name)
					}
				}
				request, err := http.ReadRequest(bufio.NewReader(io.MultiReader(strings.NewReader(header.String()), reader)))
				if err != nil {
					captured <- capture{err: err}
					return
				}
				body, err := io.ReadAll(request.Body)
				_ = request.Body.Close()
				captured <- capture{names: names, body: body, err: err}
				_, _ = io.WriteString(conn, "HTTP/1.1 204 No Content\r\n\r\n")
			}()
			upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
			require.True(t, ok)
			entry, err := upstream.getClientEntryWithTLS("", 7, 1, tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1"), service.HTTPUpstreamProfileOpenAI, false, false, native)
			require.NoError(t, err)
			defer entry.client.CloseIdleConnections()
			body := []byte(`{"input":"unchanged"}`)
			request, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/fixture", bytes.NewReader(body))
			require.NoError(t, err)
			request.Header = http.Header{
				"Session-Id": {"fixture-session"}, "Content-Type": {"application/json"},
				"Authorization": {"Bearer fixture"}, "Originator": {"codex_cli_rs"},
				"User-Agent": {"codex_cli_rs/0.156.1 (Linux; x86_64)"},
			}
			entry.client.Timeout = 3 * time.Second
			response, err := entry.client.Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			got := <-captured
			require.NoError(t, got.err)
			require.Equal(t, body, got.body)
			want := []string{"Host", "User-Agent", "Content-Length", "Authorization", "Content-Type", "Originator", "Session-Id", "Accept-Encoding"}
			if native {
				want = []string{"session-id", "content-type", "authorization", "originator", "user-agent", "host", "content-length"}
			}
			require.Equal(t, want, got.names)
			t.Logf("MODE=%s ORDER_MATCH=true BODY_EQUAL=true NAMES=%v", name, got.names)
		})
	}
}

func TestNativeOrderedHTTP1ConcurrentEarlyClose(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: begin\n\n")
		_ = http.NewResponseController(w).Flush()
		if requests.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, "data: complete\n\n")
	}))
	defer server.Close()
	defer server.CloseClientConnections()
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	entry, err := upstream.getClientEntryWithTLS("", 7, 1, tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1"), service.HTTPUpstreamProfileOpenAI, false, false, true)
	require.NoError(t, err)
	defer entry.client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(nativewire.MarkRequest(t.Context()), 8*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	r.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	response, err := doUpstreamRequest(entry.client, r)
	require.NoError(t, err)
	_, err = io.ReadFull(response.Body, make([]byte, len("data: begin\n\n")))
	require.NoError(t, err)
	started, readDone := make(chan struct{}), make(chan struct{})
	reader := &notifyReadCloser{ReadCloser: response.Body, started: started}
	go func() { _, _ = io.Copy(io.Discard, reader); close(readDone) }()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- response.Body.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Native response close blocked behind Read")
	}
	select {
	case <-readDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Native response reader leaked")
	}
	response, err = doUpstreamRequest(entry.client, r)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "data: begin\n\ndata: complete\n\n", string(body))
	require.NoError(t, r.Context().Err())
	t.Log("EARLY_CLOSE=true READER_RELEASED=true NEXT_RESPONSE_COMPLETE=true CALLER_CONTEXT_ALIVE=true")
}
