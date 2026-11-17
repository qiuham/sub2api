//go:build unit

package promptclean

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const sentence = "Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases."

func rules(t *testing.T) []*regexp.Regexp {
	t.Helper()
	compiled, bad := Compile(DefaultPatterns)
	if len(bad) > 0 || len(compiled) != 1 {
		t.Fatalf("default patterns: %v %v", compiled, bad)
	}
	return compiled
}

func TestProtocolForPath(t *testing.T) {
	cases := map[string]Protocol{
		"/v1/messages":                                  ProtocolAnthropic,
		"/antigravity/v1/messages/count_tokens":         ProtocolAnthropic,
		"/v1/chat/completions":                          ProtocolChatCompletions,
		"/v1/responses":                                 ProtocolResponses,
		"/backend-api/codex/responses":                  ProtocolResponses,
		"/v1beta/models/gemini-3-flash:generateContent": ProtocolGemini,
		"/v1beta/models/x:streamGenerateContent":        ProtocolGemini,
		"/v1/models":                                    ProtocolUnknown,
	}
	for path, want := range cases {
		if got := ProtocolForPath(path); got != want {
			t.Errorf("%s: got %v want %v", path, got, want)
		}
	}
}

func TestClean_AnthropicStringAndBlocks(t *testing.T) {
	r := rules(t)
	body := []byte(`{"system":"A. ` + sentence + ` B.","messages":[{"role":"user","content":"` + sentence + `"}]}`)
	out, n := Clean(body, ProtocolAnthropic, r)
	if n != 1 || strings.Contains(gjson.GetBytes(out, "system").String(), "Dual-use") {
		t.Fatalf("string system not cleaned: n=%d %s", n, out)
	}
	if gjson.GetBytes(out, "messages.0.content").String() != sentence {
		t.Fatalf("user message must not change: %s", out)
	}

	blocks := []byte(`{"system":[{"type":"text","text":"x"},{"type":"text","text":"IMPORTANT: ` + sentence + `","cache_control":{"type":"ephemeral"}}]}`)
	out, n = Clean(blocks, ProtocolAnthropic, r)
	if n != 1 || gjson.GetBytes(out, "system.1.text").String() != "IMPORTANT: " {
		t.Fatalf("block system not cleaned: n=%d %s", n, out)
	}
	if gjson.GetBytes(out, "system.1.cache_control.type").String() != "ephemeral" {
		t.Fatalf("other fields must stay: %s", out)
	}
}

func TestClean_ChatCompletions(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"` + sentence + `"},{"role":"developer","content":[{"type":"text","text":"y ` + sentence + `"}]},{"role":"user","content":"` + sentence + `"}]}`)
	out, n := Clean(body, ProtocolChatCompletions, rules(t))
	if n != 2 {
		t.Fatalf("want 2 hits, got %d: %s", n, out)
	}
	if gjson.GetBytes(out, "messages.2.content").String() != sentence {
		t.Fatalf("user message must not change: %s", out)
	}
}

func TestClean_Responses(t *testing.T) {
	body := []byte(`{"instructions":"` + sentence + `","input":[{"role":"developer","content":[{"type":"input_text","text":"` + sentence + `"}]},{"role":"user","content":"` + sentence + `"}]}`)
	out, n := Clean(body, ProtocolResponses, rules(t))
	if n != 2 || gjson.GetBytes(out, "instructions").String() != "" {
		t.Fatalf("want 2 hits, got %d: %s", n, out)
	}
	if gjson.GetBytes(out, "input.1.content").String() != sentence {
		t.Fatalf("user message must not change: %s", out)
	}
}

func TestClean_Gemini(t *testing.T) {
	body := []byte(`{"systemInstruction":{"parts":[{"text":"` + sentence + `"}]},"contents":[{"role":"user","parts":[{"text":"` + sentence + `"}]}]}`)
	out, n := Clean(body, ProtocolGemini, rules(t))
	if n != 1 || gjson.GetBytes(out, "contents.0.parts.0.text").String() != sentence {
		t.Fatalf("got %d: %s", n, out)
	}
}

func TestClean_NoMatchReturnsSameSlice(t *testing.T) {
	body := []byte(`{"system":"nothing here"}`)
	out, n := Clean(body, ProtocolAnthropic, rules(t))
	if n != 0 || &out[0] != &body[0] {
		t.Fatalf("no-match must return the same slice")
	}
	if out, n := Clean([]byte(`not json`), ProtocolAnthropic, rules(t)); n != 0 || string(out) != "not json" {
		t.Fatalf("invalid json must pass through")
	}
}

func TestCompile_SkipsBadPatterns(t *testing.T) {
	compiled, bad := Compile([]string{"ok", "(", " "})
	if len(compiled) != 1 || len(bad) != 1 || bad[0] != "(" {
		t.Fatalf("compiled=%v bad=%v", compiled, bad)
	}
}
