package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNativeCodexStreamPreservesBytesHeadersAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Header("X-Request-Id", "gateway")
	raw := "event: response.created\r\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\r\n\r\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"
	resp := &http.Response{Header: http.Header{"Request-Id": {"upstream"}, "Server-Timing": {"edge;dur=1"}}, Body: io.NopCloser(strings.NewReader(raw))}
	result, err := (&OpenAIGatewayService{}).handleNativeCodexStream(resp, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != raw {
		t.Fatalf("SSE bytes changed: got %q, want %q", got, raw)
	}
	if got := recorder.Header().Get("Request-Id"); got != "upstream" || recorder.Header().Get("X-Request-Id") != "" {
		t.Fatalf("request headers: %#v", recorder.Header())
	}
	if result == nil || result.usage.InputTokens != 5 || result.usage.OutputTokens != 2 || result.responseID != "resp_1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestNativeCodexCompressedSSEPreservesWireAndObservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n"
	last := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"
	wire := compressedSSEFixture(t, first, last)
	recorder := &countingStreamRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"gzip"}, "Content-Type": {"text/event-stream"}, "Request-Id": {"upstream"}}, Body: io.NopCloser(shortStreamReader{Reader: bytes.NewReader(wire), max: 16})}
	result, err := (&OpenAIGatewayService{}).handleNativeCodexStream(resp, c, time.Now())
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
	if result.usage.InputTokens != 5 || result.usage.OutputTokens != 2 || result.responseID != "resp_1" {
		t.Fatalf("result=%+v", result)
	}
}
