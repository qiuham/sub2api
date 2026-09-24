package nativewire

import (
	"bytes"
	"reflect"
	"testing"
)

func TestReplaceHeaderPreservesBodyAndHeaderOrder(t *testing.T) {
	original := Request{
		Method:     "POST",
		RequestURI: "/v1/messages?beta=true",
		Headers: []Header{
			{Name: "accept", Value: "application/json"},
			{Name: "authorization", Value: "Bearer client"},
			{Name: "content-type", Value: "application/json"},
			{Name: "x-client-request-id", Value: "request-1"},
		},
		Body: []byte("{\n  \"model\": \"claude-opus-5\", \"stream\": true\n}"),
	}

	modified := original.ReplaceHeader("Authorization", "Bearer upstream")
	if !original.BodyEqual(modified) {
		t.Fatal("body bytes changed while replacing an authentication header")
	}
	wantHeaders := []Header{
		{Name: "accept", Value: "application/json"},
		{Name: "authorization", Value: "Bearer upstream"},
		{Name: "content-type", Value: "application/json"},
		{Name: "x-client-request-id", Value: "request-1"},
	}
	if !reflect.DeepEqual(modified.Headers, wantHeaders) {
		t.Fatalf("headers = %#v, want %#v", modified.Headers, wantHeaders)
	}
	if string(original.Body) != "{\n  \"model\": \"claude-opus-5\", \"stream\": true\n}" {
		t.Fatal("original body was mutated")
	}
}

func TestReplaceHeaderCollapsesDuplicateAuthenticationOnly(t *testing.T) {
	original := Request{
		Headers: []Header{
			{Name: "Accept", Value: "text/event-stream"},
			{Name: "Authorization", Value: "Bearer first"},
			{Name: "authorization", Value: "Bearer second"},
			{Name: "Thread-Id", Value: "thread-1"},
		},
		Body: []byte(`{"input":[{"role":"user","content":"hi"}]}`),
	}

	modified := original.ReplaceHeader("authorization", "Bearer upstream")
	wantHeaders := []Header{
		{Name: "Accept", Value: "text/event-stream"},
		{Name: "Authorization", Value: "Bearer upstream"},
		{Name: "Thread-Id", Value: "thread-1"},
	}
	if !reflect.DeepEqual(modified.Headers, wantHeaders) {
		t.Fatalf("headers = %#v, want %#v", modified.Headers, wantHeaders)
	}
	if !original.BodyEqual(modified) {
		t.Fatal("body bytes changed")
	}
}

func TestWriteHTTP1PreservesExactWireBytes(t *testing.T) {
	request := Request{
		Method:     "POST",
		RequestURI: "/v1/messages?beta=true",
		Headers: []Header{
			{Name: "host", Value: "api.anthropic.com"},
			{Name: "Authorization", Value: "Bearer upstream"},
			{Name: "Content-Type", Value: "application/json"},
			{Name: "content-length", Value: "17"},
		},
		Body: []byte("{\n  \"stream\":true}"),
	}

	var wire bytes.Buffer
	if err := request.WriteHTTP1(&wire); err != nil {
		t.Fatal(err)
	}
	want := "POST /v1/messages?beta=true HTTP/1.1\r\n" +
		"host: api.anthropic.com\r\n" +
		"Authorization: Bearer upstream\r\n" +
		"Content-Type: application/json\r\n" +
		"content-length: 17\r\n" +
		"\r\n" +
		"{\n  \"stream\":true}"
	if wire.String() != want {
		t.Fatalf("wire bytes differ\ngot:  %q\nwant: %q", wire.String(), want)
	}
}
