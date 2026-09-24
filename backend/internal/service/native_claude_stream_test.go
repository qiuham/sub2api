package service

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type countingStreamRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

type shortStreamReader struct {
	io.Reader
	max int
}

func (r shortStreamReader) Read(p []byte) (int, error) {
	if len(p) > r.max {
		p = p[:r.max]
	}
	return r.Reader.Read(p)
}

func (r *countingStreamRecorder) Flush() {
	r.flushes++
	r.ResponseRecorder.Flush()
}

func compressedSSEFixture(t *testing.T, events ...string) []byte {
	t.Helper()
	var wire bytes.Buffer
	zw := gzip.NewWriter(&wire)
	for _, event := range events {
		if _, err := zw.Write([]byte(event)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return wire.Bytes()
}

func TestNativeClaudeStreamPreservesBytesHeadersAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Header("X-Request-Id", "gateway")
	raw := "event: message_start\r\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\r\n\r\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	resp := &http.Response{Header: http.Header{"Request-Id": {"upstream"}, "Server-Timing": {"edge;dur=1"}}, Body: io.NopCloser(strings.NewReader(raw))}
	result, err := (&GatewayService{}).handleNativeClaudeStream(resp, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != raw {
		t.Fatalf("SSE bytes changed: got %q, want %q", got, raw)
	}
	if got := recorder.Header().Get("Request-Id"); got != "upstream" || recorder.Header().Get("X-Request-Id") != "" {
		t.Fatalf("request headers: %#v", recorder.Header())
	}
	if got := recorder.Header().Get("Server-Timing"); got != "edge;dur=1" {
		t.Fatalf("server-timing=%q", got)
	}
	if result == nil || result.usage.InputTokens != 7 || result.usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v", result)
	}
}

func TestNativeClaudeCompressedSSEPreservesWireAndObservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n"
	last := "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	wire := compressedSSEFixture(t, first, last)
	recorder := &countingStreamRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"gzip"}, "Content-Type": {"text/event-stream"}, "Request-Id": {"upstream"}}, Body: io.NopCloser(shortStreamReader{Reader: bytes.NewReader(wire), max: 16})}
	result, err := (&GatewayService{}).handleNativeClaudeStream(resp, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), wire) {
		t.Fatal("compressed SSE wire bytes changed")
	}
	if recorder.Header().Get("Content-Encoding") != "gzip" || recorder.Header().Get("Request-Id") != "upstream" {
		t.Fatalf("headers: %#v", recorder.Header())
	}
	if recorder.flushes < 2 {
		t.Fatalf("flushes=%d, want at least two", recorder.flushes)
	}
	if result.usage.InputTokens != 7 || result.usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v", result.usage)
	}
}
