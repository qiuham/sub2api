package service

import (
	"encoding/json"
	"fmt"
)

// normalizeDeepSeekInlineToolDefinitions adapts Claude Code's inline tool blocks
// to the Anthropic shape accepted by DeepSeek. Claude Code sends a complete
// definition in a content block; DeepSeek expects that definition in top-level
// tools and a tool_reference block in the conversation.
func normalizeDeepSeekInlineToolDefinitions(body []byte) ([]byte, bool, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false, fmt.Errorf("parse messages body: %w", err)
	}
	definitions := make(map[string]map[string]any)
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case []any:
			out := make([]any, len(x))
			for i := range x {
				out[i] = walk(x[i])
			}
			return out
		case map[string]any:
			if x["type"] == "tool_definition" {
				d, ok := x["definition"].(map[string]any)
				if !ok {
					d = x
				}
				name, _ := d["name"].(string)
				if name != "" {
					cp := map[string]any{}
					for k, val := range d {
						if k != "type" {
							cp[k] = val
						}
					}
					definitions[name] = cp
					return map[string]any{"type": "tool_reference", "tool_name": name}
				}
			}
			for k, val := range x {
				x[k] = walk(val)
			}
			return x
		default:
			return v
		}
	}
	root = walk(root).(map[string]any)
	if len(definitions) == 0 {
		return body, false, nil
	}
	tools, _ := root["tools"].([]any)
	seen := map[string]bool{}
	for _, t := range tools {
		if m, ok := t.(map[string]any); ok {
			if n, _ := m["name"].(string); n != "" {
				seen[n] = true
			}
		}
	}
	for name, d := range definitions {
		if !seen[name] {
			tools = append(tools, d)
		}
	}
	root["tools"] = tools
	out, err := json.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
