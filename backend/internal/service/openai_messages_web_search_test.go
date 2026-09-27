//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAsAnthropicWebSearchSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", accountType, stream), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-sol","max_tokens":128,"stream":%v,"messages":[{"role":"user","content":"search docs"}],"tool_choice":{"type":"auto","disable_parallel_tool_use":true},"tools":[{"type":"web_search_20250305","name":"web_search","allowed_domains":["example.com"]}]}`, stream))
				item := `{"type":"web_search_call","id":"ws_test","status":"completed","action":{"type":"search","query":"docs","sources":[{"type":"url","url":"https://example.com/docs"}]}}`
				events := []string{
					`{"type":"response.created","response":{"id":"resp_test","model":"gpt-6-sol","status":"in_progress","output":[]}}`,
					`{"type":"response.output_item.done","output_index":0,"item":` + item + `}`,
					`{"type":"response.completed","response":{"id":"resp_test","model":"gpt-6-sol","status":"completed","output":[` + item + `],"usage":{"input_tokens":10,"output_tokens":5}}}`,
				}
				var sse strings.Builder
				for _, e := range events {
					fmt.Fprintf(&sse, "data: %s\n\n", e)
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse.String()))}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				account := rawChatCompletionsTestAccount()
				if accountType == AccountTypeOAuth {
					account = openAISetupTokenCompatAccount(72)
					account.Type = AccountTypeOAuth
				}
				_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				require.NoError(t, err)
				require.Equal(t, 200, rec.Code)
				require.False(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Bool())
				require.Contains(t, gjson.GetBytes(upstream.lastBody, "include").String(), "web_search_call.action.sources")
				require.Equal(t, "example.com", gjson.GetBytes(upstream.lastBody, "tools.0.filters.allowed_domains.0").String())
				require.Contains(t, rec.Body.String(), `"type":"web_search_result"`)
				require.Contains(t, rec.Body.String(), "https://example.com/docs")
			})
		}
	}

}
