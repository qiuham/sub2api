package nativewire

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestDecodeResponseForObservationKeepsWireSeparate(t *testing.T) {
	plain := []byte(`{"usage":{"input_tokens":2}}`)
	compressors := map[string]func(*bytes.Buffer) (io.WriteCloser, error){
		"gzip":    func(dst *bytes.Buffer) (io.WriteCloser, error) { return gzip.NewWriter(dst), nil },
		"deflate": func(dst *bytes.Buffer) (io.WriteCloser, error) { return flate.NewWriter(dst, flate.DefaultCompression) },
		"br":      func(dst *bytes.Buffer) (io.WriteCloser, error) { return brotli.NewWriter(dst), nil },
		"zstd":    func(dst *bytes.Buffer) (io.WriteCloser, error) { return zstd.NewWriter(dst) },
	}
	for encoding, makeWriter := range compressors {
		t.Run(encoding, func(t *testing.T) {
			var wire bytes.Buffer
			writer, err := makeWriter(&wire)
			require.NoError(t, err)
			_, err = writer.Write(plain)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			original := append([]byte(nil), wire.Bytes()...)
			observed, err := DecodeResponseForObservation(wire.Bytes(), http.Header{"Content-Encoding": {encoding}})
			require.NoError(t, err)
			require.Equal(t, plain, observed)
			require.Equal(t, original, wire.Bytes())
		})
	}
	observed, err := DecodeResponseForObservation(plain, http.Header{})
	require.NoError(t, err)
	require.Equal(t, plain, observed)
	_, err = DecodeResponseForObservation(plain, http.Header{"Content-Encoding": {"unknown"}})
	require.Error(t, err)
}
