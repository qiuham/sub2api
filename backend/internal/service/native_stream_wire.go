package service

import (
	"io"
	"net/http"
)

// nativeStreamWireWriter flushes each upstream read without changing the bytes.
// A client disconnect does not stop observation of the upstream stream.
type nativeStreamWireWriter struct {
	dst          io.Writer
	flusher      http.Flusher
	disconnected bool
}

func (w *nativeStreamWireWriter) Write(p []byte) (int, error) {
	if !w.disconnected {
		if _, err := w.dst.Write(p); err != nil {
			w.disconnected = true
		} else {
			w.flusher.Flush()
		}
	}
	return len(p), nil
}
