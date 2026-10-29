//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPatchReleaseDeepSeekNative(t *testing.T) {
	body := []byte(`{"model":"deepseek-flash","max_tokens":32,"stream":false,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"search_1","content":[{"type":"tool_definition","definition":{"name":"lookup","description":"find","input_schema":{"type":"object"}}}]}]}]}`)
	a := nativeAnthropicTestAccount()
	a.Platform = PlatformDeepseek
	u := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
	s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u}
	_, err := s.ForwardAsAnthropic(context.Background(), adaptiveProtocolTestContext("/v1/messages", body), a, body, "", "")
	require.NoError(t, err)
	require.NotContains(t, string(u.lastBody), `"tool_definition"`)
	require.Contains(t, string(u.lastBody), `"tool_reference"`)
	require.Contains(t, string(u.lastBody), `"tool_name":"lookup"`)
}

func TestPatchReleaseOpenAIEncryptedRetry(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","max_tokens":32,"stream":false,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"thinking","thinking":"old","signature":"CAQS_foreign_signature"},{"type":"text","text":"old answer"}]},{"role":"user","content":"reply OK"}]}`)
	a := rawChatCompletionsTestAccount()
	a.Credentials["api_protocol"] = APIProtocolResponses
	success := `data: {"type":"response.completed","response":{"id":"resp_test","model":"gpt-6-astra","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":10,"output_tokens":1}}}` + "\n\n"
	u := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_encrypted_content","message":"The encrypted content CAQS could not be verified"}}`))},
		{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(success))},
	}}
	s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u}
	_, err := s.ForwardAsAnthropic(context.Background(), adaptiveProtocolTestContext("/v1/messages", body), a, body, "test-session", "")
	require.NoError(t, err)
	require.Len(t, u.bodies, 2)
	require.Contains(t, string(u.bodies[0]), "CAQS_foreign_signature")
	require.NotContains(t, string(u.bodies[1]), "CAQS_foreign_signature")
	require.Contains(t, string(u.bodies[1]), "old answer")
	require.Equal(t, u.requests[0].Header, u.requests[1].Header, "retry must retain identity headers")
}

func TestPatchReleaseEncryptedErrorClassifier(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{400, `{"error":{"message":"The encrypted content CAQS could not be verified"}}`, true},
		{400, `{"error":{"message":"Encrypted content could not be decrypted or parsed"}}`, true},
		{422, `{"error":{"code":"invalid_encrypted_content"}}`, true},
		{400, `{"error":{"message":"Invalid schema for tool"}}`, false},
		{429, `{"error":{"code":"invalid_encrypted_content"}}`, false},
	} {
		require.Equal(t, tc.want, isOpenAICompatInvalidEncryptedContentResponse(tc.status, []byte(tc.body)))
	}
}
