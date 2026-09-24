package nativewire

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// NewStreamObservationReader decodes SSE only for observation. The tee sends
// upstream's encoded bytes to the downstream writer as they are read.
func NewStreamObservationReader(raw io.Reader, headers http.Header, downstream io.Writer) (io.Reader, func(), error) {
	wire := io.TeeReader(raw, downstream)
	switch strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding"))) {
	case "", "identity":
		return wire, func() {}, nil
	case "gzip":
		r, err := gzip.NewReader(wire)
		if err != nil {
			return nil, nil, err
		}
		return r, func() { _ = r.Close() }, nil
	case "br":
		return brotli.NewReader(wire), func() {}, nil
	case "deflate":
		r := flate.NewReader(wire)
		return r, func() { _ = r.Close() }, nil
	case "zstd":
		r, err := zstd.NewReader(wire)
		if err != nil {
			return nil, nil, err
		}
		return r, r.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported content encoding %q", headers.Get("Content-Encoding"))
	}
}
