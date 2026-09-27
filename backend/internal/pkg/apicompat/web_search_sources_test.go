package apicompat

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const searchOutput = `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"documentation","sources":[{"type":"url","url":"https://example.com/docs","title":"Documentation"},{"type":"url","url":"https://example.com/docs","title":"Documentation"},{"type":"url","url":"https://example.org/reference"}]}}`

func TestWebSearchSourcesRequest(t *testing.T) {
	var req AnthropicRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6-sol","messages":[{"role":"user","content":"search docs"}],"tools":[{"type":"web_search_20250305","name":"web_search","allowed_domains":["example.com"],"blocked_domains":["blocked.example"],"user_location":{"type":"approximate","city":"London","country":"GB","timezone":"Europe/London"}}]}`), &req))
	out, err := AnthropicToResponses(&req)
	require.NoError(t, err)
	require.Contains(t, out.Include, "web_search_call.action.sources")
	data, err := json.Marshal(out.Tools[0])
	require.NoError(t, err)
	require.Contains(t, string(data), `"allowed_domains":["example.com"]`)
	require.Contains(t, string(data), `"blocked_domains":["blocked.example"]`)
	require.Contains(t, string(data), `"city":"London"`)
}

func assertSearchSources(t *testing.T, blocks []AnthropicContentBlock) {
	t.Helper()
	for _, b := range blocks {
		if b.Type != "web_search_tool_result" {
			continue
		}
		var results []map[string]any
		require.NoError(t, json.Unmarshal(b.Content, &results))
		require.Len(t, results, 2, "search sources must not be replaced with an empty result list")
		require.Equal(t, "web_search_result", results[0]["type"])
		require.Equal(t, "https://example.com/docs", results[0]["url"])
		require.Equal(t, "Documentation", results[0]["title"])
		require.Equal(t, "https://example.org/reference", results[1]["url"])
		return
	}
	t.Fatal("missing web_search_tool_result")
}

func TestWebSearchSourcesNonStreaming(t *testing.T) {
	var item ResponsesOutput
	require.NoError(t, json.Unmarshal([]byte(searchOutput), &item))
	out := ResponsesToAnthropic(&ResponsesResponse{ID: "resp_1", Status: "completed", Output: []ResponsesOutput{item}}, "gpt-6-sol")
	assertSearchSources(t, out.Content)
}
func TestWebSearchSourcesStreaming(t *testing.T) {
	var evt ResponsesStreamEvent
	require.NoError(t, json.Unmarshal([]byte(`{"type":"response.output_item.done","output_index":0,"item":`+searchOutput+`}`), &evt))
	events := ResponsesEventToAnthropicEvents(&evt, NewResponsesEventToAnthropicState())
	var blocks []AnthropicContentBlock
	for _, e := range events {
		if e.ContentBlock != nil {
			blocks = append(blocks, *e.ContentBlock)
		}
	}
	assertSearchSources(t, blocks)
}

func TestMessagesToolControls(t *testing.T) {
	for _, tc := range []string{`{"type":"auto","disable_parallel_tool_use":true}`, `{"type":"tool","name":"web_search","disable_parallel_tool_use":true}`} {
		var req AnthropicRequest
		require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6-sol","tools":[{"type":"web_search_20250305","name":"web_search"}],"tool_choice":`+tc+`}`), &req))
		out, err := AnthropicToResponses(&req)
		require.NoError(t, err)
		require.NotNil(t, out.ParallelToolCalls)
		require.False(t, *out.ParallelToolCalls)
		if strings.Contains(tc, `"type":"tool"`) {
			require.JSONEq(t, `{"type":"web_search"}`, string(out.ToolChoice))
		}
	}
}

func TestMessagesToolErrorAndURLImage(t *testing.T) {
	var req AnthropicRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6-sol","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","is_error":true,"content":[{"type":"text","text":"permission denied"},{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]}]}`), &req))
	out, err := AnthropicToResponses(&req)
	require.NoError(t, err)
	var input []ResponsesInputItem
	require.NoError(t, json.Unmarshal(out.Input, &input))
	require.Contains(t, input[0].Output, "is_error")
	require.Contains(t, input[0].Output, "permission denied")
	require.Len(t, input, 2)
	require.Contains(t, string(input[1].Content), "https://example.com/image.png")
}

func TestMessagesClientToolsRoundTrip(t *testing.T) {
	for _, name := range []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebFetch", "WebSearch", "Agent", "Task", "TaskOutput", "TodoWrite", "mcp__example__lookup"} {
		t.Run(name, func(t *testing.T) {
			args := json.RawMessage(`{"query":"hello","nested":{"items":[1,true,"值"]}}`)
			in := &ResponsesResponse{Status: "completed", Output: []ResponsesOutput{{Type: "function_call", CallID: "call_client", Name: name, Arguments: string(args)}}}
			msg := ResponsesToAnthropic(in, "gpt-6-sol")
			require.Len(t, msg.Content, 1)
			require.Equal(t, name, msg.Content[0].Name)
			require.Equal(t, "call_client", msg.Content[0].ID)
			require.JSONEq(t, string(args), string(msg.Content[0].Input))
			body, err := json.Marshal(msg.Content)
			require.NoError(t, err)
			out, err := AnthropicToResponses(&AnthropicRequest{Model: "gpt-6-sol", Messages: []AnthropicMessage{
				{Role: "assistant", Content: body},
				{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call_client","content":"done"}]`)},
			}})
			require.NoError(t, err)
			var items []ResponsesInputItem
			require.NoError(t, json.Unmarshal(out.Input, &items))
			require.Len(t, items, 2)
			require.Equal(t, name, items[0].Name)
			require.Equal(t, items[0].CallID, items[1].CallID)
			require.Equal(t, "done", items[1].Output)
		})
	}
}

func TestWebSearchSourcesEmptyAndInvalid(t *testing.T) {
	for _, action := range []*WebSearchAction{nil, {}, {Sources: []WebSearchSource{{URL: "javascript:alert(1)"}, {URL: ""}, {URL: "file:///etc/hosts"}, {URL: "oai-weather"}}}} {
		require.JSONEq(t, "[]", string(webSearchResults(action)))
	}
	req, err := AnthropicToResponses(&AnthropicRequest{Tools: []AnthropicTool{{Name: "WebSearch", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
	require.NoError(t, err)
	require.Equal(t, "function", req.Tools[0].Type, "client WebSearch is not a hosted search tool")
	require.NotContains(t, req.Include, "web_search_call.action.sources")
}
