package nativewire

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

type Header struct {
	Name  string
	Value string
}

type Request struct {
	Method     string
	RequestURI string
	Headers    []Header
	Body       []byte
}

func (r Request) Clone() Request {
	clone := r
	clone.Headers = append([]Header(nil), r.Headers...)
	clone.Body = append([]byte(nil), r.Body...)
	return clone
}

func (r Request) ReplaceHeader(name, value string) Request {
	clone := r.Clone()
	replaced := false
	out := clone.Headers[:0]
	for _, header := range clone.Headers {
		if !strings.EqualFold(header.Name, name) {
			out = append(out, header)
			continue
		}
		if !replaced {
			header.Value = value
			out = append(out, header)
			replaced = true
		}
	}
	if !replaced {
		out = append(out, Header{Name: name, Value: value})
	}
	clone.Headers = out
	return clone
}

func (r Request) BodyEqual(other Request) bool {
	return bytes.Equal(r.Body, other.Body)
}

// WriteHTTP1 writes exactly the ordered header names, values, and body carried
// by Request. The caller owns Host, Content-Length, and every other wire field;
// this package deliberately does not normalize or synthesize them.
func (r Request) WriteHTTP1(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s %s HTTP/1.1\r\n", r.Method, r.RequestURI); err != nil {
		return err
	}
	for _, header := range r.Headers {
		if _, err := fmt.Fprintf(w, "%s: %s\r\n", header.Name, header.Value); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\r\n"); err != nil {
		return err
	}
	_, err := w.Write(r.Body)
	return err
}
