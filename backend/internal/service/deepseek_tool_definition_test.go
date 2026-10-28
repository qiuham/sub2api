package service

import (
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestNormalizeDeepSeekInlineToolDefinitions(t *testing.T) {
	in := []byte(`{"model":"deepseek-chat","tools":[{"name":"old","description":"x","input_schema":{"type":"object"}}],"messages":[{"role":"assistant","content":[{"type":"tool_definition","definition":{"name":"lookup","description":"find","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}},{"type":"text","text":"ok"}]}]}`)
	out, changed, err := normalizeDeepSeekInlineToolDefinitions(in)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "tool_reference", gjson.GetBytes(out, "messages.0.content.0.type").String())
	require.Equal(t, "lookup", gjson.GetBytes(out, "messages.0.content.0.tool_name").String())
	require.Equal(t, "lookup", gjson.GetBytes(out, "tools.1.name").String())
}
func TestNormalizeDeepSeekInlineToolDefinitionsNoop(t *testing.T) {
	in := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out, changed, err := normalizeDeepSeekInlineToolDefinitions(in)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, string(in), string(out))
}
