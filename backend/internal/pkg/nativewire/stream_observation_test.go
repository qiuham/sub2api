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
)

func TestStreamObservationKeepsEncodedBytesSeparate(t *testing.T) {
	plain := []byte("event: response.completed\ndata: {\"usage\":{\"input_tokens\":2}}\n\n")
	for _, tc := range []struct {
		name      string
		newWriter func(io.Writer) (io.WriteCloser, error)
	}{
		{"gzip", func(w io.Writer) (io.WriteCloser, error) { return gzip.NewWriter(w), nil }},
		{"deflate", func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.DefaultCompression) }},
		{"br", func(w io.Writer) (io.WriteCloser, error) { return brotli.NewWriter(w), nil }},
		{"zstd", func(w io.Writer) (io.WriteCloser, error) { return zstd.NewWriter(w) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var encoded, forwarded bytes.Buffer
			writer, err := tc.newWriter(&encoded)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = writer.Write(plain); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			reader, closeReader, err := NewStreamObservationReader(bytes.NewReader(encoded.Bytes()), http.Header{"Content-Encoding": {tc.name}}, &forwarded)
			if err != nil {
				t.Fatal(err)
			}
			defer closeReader()
			observed, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(observed, plain) {
				t.Fatalf("observed=%q", observed)
			}
			if !bytes.Equal(forwarded.Bytes(), encoded.Bytes()) {
				t.Fatal("encoded bytes changed")
			}
		})
	}
}
