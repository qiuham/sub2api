package nativewire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type caseTestConn struct {
	net.Conn
	output    bytes.Buffer
	closed    bool
	failAfter int
	writeErr  error
}

func (c *caseTestConn) Write(p []byte) (int, error) {
	if c.failAfter >= 0 && c.output.Len()+len(p) > c.failAfter {
		n := max(0, c.failAfter-c.output.Len())
		_, _ = c.output.Write(p[:n])
		return n, c.writeErr
	}
	return c.output.Write(p)
}

func (c *caseTestConn) Close() error { c.closed = true; return nil }

func TestHTTP1HeaderCaseEveryWriteBoundary(t *testing.T) {
	raw := "POST /Host: HTTP/1.1\r\nHost: example.test\r\nUser-Agent: Fixture Host\r\nContent-Length: 33\r\nX-Note: User-Agent: Keep\r\nHostility: untouched\r\n\r\nUser-Agent: BODY\r\nHost: BODY\r\n\r\n"
	want := strings.NewReplacer("\r\nHost: example", "\r\nhost: example", "\r\nUser-Agent: Fixture", "\r\nuser-agent: Fixture", "\r\nContent-Length: 33", "\r\ncontent-length: 33").Replace(raw)
	for split := 0; split <= len(raw); split++ {
		underlying := &caseTestConn{failAfter: -1}
		conn := &http1HeaderCaseConn{Conn: underlying}
		conn.begin(http1CaseCodexHTTP)
		input := []byte(raw)
		n, err := conn.Write(input[:split])
		require.NoError(t, err)
		require.Equal(t, split, n)
		n, err = conn.Write(input[split:])
		require.NoError(t, err)
		require.Equal(t, len(raw)-split, n)
		require.Equal(t, want, underlying.output.String(), "split=%d", split)
		require.Equal(t, raw, string(input), "caller's bytes must not be mutated")
	}
	underlying := &caseTestConn{failAfter: -1}
	conn := &http1HeaderCaseConn{Conn: underlying}
	conn.begin(http1CaseCodexHTTP)
	for i := range raw {
		n, err := conn.Write([]byte(raw[i : i+1]))
		require.NoError(t, err)
		require.Equal(t, 1, n)
	}
	require.Equal(t, want, underlying.output.String())
}

func TestHTTP1HeaderCaseReuseAndWSFramesStayOpaque(t *testing.T) {
	underlying := &caseTestConn{failAfter: -1}
	conn := &http1HeaderCaseConn{Conn: underlying}
	header := "GET / HTTP/1.1\r\nHost: fixture\r\nUser-Agent: Fixture\r\n\r\n"
	_, err := conn.Write([]byte(header)) // no GotConn: no transformation
	require.NoError(t, err)
	conn.begin(http1CaseCodexHTTP)
	_, err = conn.Write([]byte(header))
	require.NoError(t, err)
	conn.begin(http1CaseUnchanged) // account/mode changes must not inherit state
	_, err = conn.Write([]byte(header))
	require.NoError(t, err)
	conn.begin(http1CaseCodexWS)
	_, err = conn.Write([]byte(header))
	require.NoError(t, err)
	frame := append([]byte{0x82, 0x80, 0xff, 0x01, 0x00, 0x0d}, []byte(header)...)
	_, err = conn.Write(frame)
	require.NoError(t, err)
	want := header + strings.ReplaceAll(strings.ReplaceAll(header, "Host:", "host:"), "User-Agent:", "user-agent:") + header + strings.ReplaceAll(header, "User-Agent:", "user-agent:")
	require.Equal(t, append([]byte(want), frame...), underlying.output.Bytes())
}

func TestHTTP1HeaderCaseBoundedHeaderNotBody(t *testing.T) {
	underlying := &caseTestConn{failAfter: -1}
	conn := &http1HeaderCaseConn{Conn: underlying}
	conn.begin(http1CaseCodexHTTP)
	header := "POST / HTTP/1.1\r\nHost: fixture\r\n\r\n"
	body := bytes.Repeat([]byte("Host: BODY\r\n\r\n"), 200000)
	input := append([]byte(header), body...)
	n, err := conn.Write(input)
	require.NoError(t, err)
	require.Equal(t, len(input), n)
	require.Equal(t, append([]byte(strings.ReplaceAll(header, "Host:", "host:")), body...), underlying.output.Bytes())
	tooLarge := &caseTestConn{failAfter: -1}
	bounded := &http1HeaderCaseConn{Conn: tooLarge}
	bounded.begin(http1CaseCodexHTTP)
	_, err = bounded.Write([]byte("GET / HTTP/1.1\r\nX-Large: " + strings.Repeat("x", http.DefaultMaxHeaderBytes)))
	require.ErrorContains(t, err, "exceed 1 MiB")
	require.True(t, tooLarge.closed)
	require.Zero(t, tooLarge.output.Len())
}

func TestHTTP1HeaderCasePartialWriteFailureAccounting(t *testing.T) {
	raw := []byte("POST / HTTP/1.1\r\nHost: fixture\r\nUser-Agent: Fixture\r\n\r\nbody")
	for _, limit := range []int{8, 32} {
		underlying := &caseTestConn{failAfter: limit, writeErr: io.ErrUnexpectedEOF}
		conn := &http1HeaderCaseConn{Conn: underlying}
		conn.begin(http1CaseCodexHTTP)
		n, err := conn.Write(raw[:12])
		require.NoError(t, err)
		require.Equal(t, 12, n)
		n, err = conn.Write(raw[12:])
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Equal(t, max(0, limit-12), n)
		require.True(t, underlying.closed)
		n, err = conn.Write(raw)
		require.Zero(t, n)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
	short := &caseTestConn{failAfter: 8}
	conn := &http1HeaderCaseConn{Conn: short}
	conn.begin(http1CaseCodexHTTP)
	_, err := conn.Write(raw)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.True(t, short.closed)
}

func TestHTTP1HeaderCaseRejectsIncompleteReuse(t *testing.T) {
	underlying := &caseTestConn{failAfter: -1}
	conn := &http1HeaderCaseConn{Conn: underlying}
	conn.begin(http1CaseCodexHTTP)
	_, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost:"))
	require.NoError(t, err)
	conn.begin(http1CaseCodexHTTP)
	_, err = conn.Write([]byte("fixture\r\n\r\n"))
	require.ErrorContains(t, err, "before headers completed")
	require.True(t, underlying.closed)
}

func TestHTTP1HeaderCaseTraceRearmsPooledConnection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()
	records := make(chan string, 3)
	serverErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(serverConn)
		for i := 0; i < 3; i++ {
			var header strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					serverErr <- err
					return
				}
				_, _ = header.WriteString(line)
				if line == "\r\n" {
					break
				}
			}
			records <- header.String()
			if _, err := io.WriteString(serverConn, "HTTP/1.1 204 No Content\r\n\r\n"); err != nil {
				serverErr <- err
				return
			}
		}
	}()
	dials, callbacks := 0, 0
	transport := NewHTTP1Transport(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		if dials > 1 {
			return nil, errors.New("unexpected second dial")
		}
		return clientConn, nil
	}})
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for i := 0; i < 3; i++ {
		ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { callbacks++ }})
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://fixture.test/", nil)
		require.NoError(t, err)
		r.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
		resp, err := client.Do(r)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		select {
		case raw := <-records:
			require.Contains(t, raw, "\r\nuser-agent: codex_cli_rs/")
			require.Contains(t, raw, "\r\nhost: fixture.test\r\n")
		case err := <-serverErr:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			t.Fatal("raw request capture timed out")
		}
	}
	require.Equal(t, 1, dials)
	require.Equal(t, 3, callbacks, "existing request timing trace must still fire")
}

func TestHTTP1HeaderCaseWrapsAfterTLSHandshake(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "tls-fixture")
	}))
	defer server.Close()
	baseTransport, ok := server.Client().Transport.(*http.Transport)
	require.True(t, ok)
	trusted := baseTransport.TLSClientConfig.Clone()
	dialer := &tls.Dialer{Config: trusted}
	transport := NewHTTP1Transport(&http.Transport{DialTLSContext: dialer.DialContext})
	defer transport.CloseIdleConnections()
	r, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	r.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Do(r)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "tls-fixture", string(body))
}

type caseTrackedBody struct {
	io.ReadCloser
	closed bool
}

func (b *caseTrackedBody) Close() error { b.closed = true; return b.ReadCloser.Close() }

func TestHTTP1HeaderCaseRejectsUnconfiguredTLS(t *testing.T) {
	dialed := false
	transport := NewHTTP1Transport(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected network request")
	}})
	defer transport.CloseIdleConnections()
	body := &caseTrackedBody{ReadCloser: io.NopCloser(strings.NewReader("fixture"))}
	r, err := http.NewRequest(http.MethodPost, "https://fixture.test/", body)
	require.NoError(t, err)
	r.Header.Set("User-Agent", "codex_cli_rs/0.156.1 (Linux; x86_64)")
	response, err := transport.RoundTrip(r)
	require.Nil(t, response)
	require.ErrorContains(t, err, "requires its custom TLS dialer")
	require.True(t, body.closed)
	require.False(t, dialed)
}

func TestHTTP1HeaderCaseCloseInterruptsBlockedWrite(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()
	conn := &http1HeaderCaseConn{Conn: client}
	conn.begin(http1CaseCodexHTTP)
	done := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: fixture\r\n\r\n"))
		done <- err
	}()
	require.NoError(t, server.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, err := io.ReadFull(server, make([]byte, 1)) // writer has entered the underlying Write
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited for the header writer's mutex")
	}
}
