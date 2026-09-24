package nativewire

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

const maxObservedResponseBytes = 64 << 20

// DecodeResponseForObservation decodes a copy for billing and diagnostics.
// Callers must write the original wire bytes to the downstream client.
func DecodeResponseForObservation(wire []byte, headers http.Header) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding")))
	if encoding == "" || encoding == "identity" {
		return wire, nil
	}
	var reader io.Reader
	var closeReader func()
	switch encoding {
	case "gzip":
		gzipReader, err := gzip.NewReader(bytes.NewReader(wire))
		if err != nil {
			return nil, err
		}
		reader = gzipReader
		closeReader = func() { _ = gzipReader.Close() }
	case "br":
		reader = brotli.NewReader(bytes.NewReader(wire))
	case "deflate":
		deflateReader := flate.NewReader(bytes.NewReader(wire))
		reader = deflateReader
		closeReader = func() { _ = deflateReader.Close() }
	case "zstd":
		zstdReader, err := zstd.NewReader(bytes.NewReader(wire))
		if err != nil {
			return nil, err
		}
		reader = zstdReader
		closeReader = zstdReader.Close
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", encoding)
	}
	if closeReader != nil {
		defer closeReader()
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxObservedResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > maxObservedResponseBytes {
		return nil, fmt.Errorf("decoded response exceeds observation limit")
	}
	return decoded, nil
}
