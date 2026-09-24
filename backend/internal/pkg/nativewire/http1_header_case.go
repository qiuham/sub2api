package nativewire

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
)

type http1HeaderCase uint8

const (
	http1CaseUnchanged http1HeaderCase = iota
	http1CaseCodexHTTP
	http1CaseCodexWS
)

// req owns serialization, connection reuse and all body/upgrade framing. Its
// GotConn trace starts one header block per HTTP request, including reuse and
// retries. This adapter only changes the case of three synthesized field names.
// It never infers request boundaries from body bytes or WebSocket frames.
type http1HeaderCaseConn struct {
	net.Conn
	mu     sync.Mutex
	mode   http1HeaderCase
	header []byte
	active bool
	err    error
}

func withHTTP1HeaderCaseDialer(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return nil, err
		}
		if conn == nil {
			return nil, errors.New("native HTTP/1 dialer returned a nil connection")
		}
		return &http1HeaderCaseConn{Conn: conn}, nil
	}
}

func (c *http1HeaderCaseConn) begin(mode http1HeaderCase) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.header) != 0 {
		_ = c.fail(errors.New("native HTTP/1 connection reused before headers completed"))
		return
	}
	c.mode = mode
	c.active = mode != http1CaseUnchanged
}

// Close remains the underlying Conn.Close: cancellation must be able to
// interrupt a blocked Write without waiting for this mutex.
func (c *http1HeaderCaseConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, c.err
	}
	if !c.active {
		return c.Conn.Write(p)
	}
	previous := len(c.header)
	// Bound only the HTTP header block, not a body included in the same Write.
	take := min(len(p), http.DefaultMaxHeaderBytes-previous)
	c.header = append(c.header, p[:take]...)
	end := bytes.Index(c.header, []byte("\r\n\r\n"))
	if end < 0 {
		if len(c.header) == http.DefaultMaxHeaderBytes {
			return take, c.fail(errors.New("native HTTP/1 headers exceed 1 MiB"))
		}
		return len(p), nil
	}
	if err := lowercaseSynthesizedHTTP1Headers(c.header[:end+4], c.mode); err != nil {
		return take, c.fail(err)
	}
	c.active = false
	total := len(c.header) + len(p[take:])
	buffers := net.Buffers{c.header}
	if take < len(p) {
		buffers = append(buffers, p[take:])
	}
	written, err := buffers.WriteTo(c.Conn)
	c.header = nil
	if err == nil && written != int64(total) {
		err = io.ErrShortWrite
	}
	// Casing is length-preserving. Bytes buffered by earlier calls were already
	// acknowledged; report only this call's bytes if the actual write fails.
	accepted := min(len(p), max(0, int(written)-previous))
	if err != nil {
		return accepted, c.fail(err)
	}
	return len(p), nil
}

func (c *http1HeaderCaseConn) fail(err error) error {
	c.err = err
	c.header = nil
	_ = c.Close()
	return err
}

func lowercaseSynthesizedHTTP1Headers(header []byte, mode http1HeaderCase) error {
	if mode == http1CaseUnchanged {
		return nil
	}
	position := bytes.Index(header, []byte("\r\n"))
	if position < 0 {
		return errors.New("native HTTP/1 request line missing")
	}
	position += 2
	for position < len(header)-2 {
		end := bytes.Index(header[position:], []byte("\r\n"))
		if end < 0 {
			return errors.New("native HTTP/1 header line incomplete")
		}
		line := header[position : position+end]
		colon := bytes.IndexByte(line, ':')
		if colon <= 0 {
			return errors.New("native HTTP/1 header name missing")
		}
		name := line[:colon]
		lower := bytes.EqualFold(name, []byte("User-Agent")) ||
			(mode == http1CaseCodexHTTP && (bytes.EqualFold(name, []byte("Host")) || bytes.EqualFold(name, []byte("Content-Length"))))
		if lower {
			for i, ch := range name {
				if ch >= 'A' && ch <= 'Z' {
					name[i] = ch + ('a' - 'A')
				}
			}
		}
		position += end + 2
	}
	return nil
}
