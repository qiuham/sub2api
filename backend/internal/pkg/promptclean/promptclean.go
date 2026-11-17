// Package promptclean 在请求转发前，按规则删掉系统提示里的指定句子。
//
// 只动系统提示（各协议里“系统/开发者”那一段），不碰用户和助手的对话内容。
// 协议按请求路径判断：
//   - Anthropic Messages（/messages、/messages/count_tokens）：system（字符串或 text 块数组）
//   - OpenAI Chat Completions（/chat/completions）：role 为 system/developer 的消息
//   - OpenAI Responses（/responses）：instructions，以及 input 里 role 为 system/developer 的消息
//   - Gemini 原生（:generateContent、:streamGenerateContent、:countTokens）：systemInstruction / system_instruction
//
// 用途：有些上游会对系统提示里的特定句子做出异常反应（例如 Google Antigravity 对 Claude Code
// 系统提示里“两用安全工具要授权”那句回假 429），在网关这一层统一删掉，所有客户端都管得到。
package promptclean

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Protocol 请求体的格式。
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolAnthropic
	ProtocolChatCompletions
	ProtocolResponses
	ProtocolGemini
)

// ProtocolForPath 按请求路径判断请求体格式；认不出返回 ProtocolUnknown（不清洗）。
func ProtocolForPath(path string) Protocol {
	switch {
	case strings.Contains(path, "/messages"):
		return ProtocolAnthropic
	case strings.Contains(path, "/chat/completions"):
		return ProtocolChatCompletions
	case strings.Contains(path, "/responses"):
		return ProtocolResponses
	case strings.Contains(path, ":generateContent"), strings.Contains(path, ":streamGenerateContent"),
		strings.Contains(path, ":countTokens"):
		return ProtocolGemini
	}
	return ProtocolUnknown
}

// Clean 删掉 body 里系统提示中所有匹配 rules 的文字，返回新的 body 和删了几处。
// 没有匹配时原样返回 body（同一个切片），不做任何拷贝。body 不是 JSON 时也原样返回。
func Clean(body []byte, protocol Protocol, rules []*regexp.Regexp) ([]byte, int) {
	if len(rules) == 0 || protocol == ProtocolUnknown || !gjson.ValidBytes(body) {
		return body, 0
	}
	var paths []string
	switch protocol {
	case ProtocolAnthropic:
		paths = textPaths(body, "system", "text")
	case ProtocolChatCompletions:
		paths = messagePaths(body, "messages", "text")
	case ProtocolResponses:
		paths = append(textPaths(body, "instructions", "text"), messagePaths(body, "input", "text")...)
	case ProtocolGemini:
		for _, root := range []string{"systemInstruction", "system_instruction"} {
			if gjson.GetBytes(body, root).Exists() {
				paths = append(paths, textPaths(body, root+".parts", "text")...)
			}
		}
	}
	total := 0
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Type != gjson.String {
			continue
		}
		cleaned, hits := apply(value.String(), rules)
		if hits == 0 {
			continue
		}
		updated, err := sjson.SetBytes(body, path, cleaned)
		if err != nil {
			continue
		}
		body = updated
		total += hits
	}
	return body, total
}

// textPaths 某个位置是字符串就返回它本身；是数组就返回每个元素的 textField（或元素本身是字符串）。
func textPaths(body []byte, root, textField string) []string {
	node := gjson.GetBytes(body, root)
	switch {
	case node.Type == gjson.String:
		return []string{root}
	case node.IsArray():
		var out []string
		for i, item := range node.Array() {
			prefix := root + "." + strconv.Itoa(i)
			if item.Type == gjson.String {
				out = append(out, prefix)
			} else if item.Get(textField).Type == gjson.String {
				out = append(out, prefix+"."+textField)
			}
		}
		return out
	}
	return nil
}

// messagePaths 消息数组里 role 为 system/developer 的那些消息的文字位置（content 为字符串或内容块数组）。
func messagePaths(body []byte, root, textField string) []string {
	node := gjson.GetBytes(body, root)
	if !node.IsArray() {
		return nil
	}
	var out []string
	for i, msg := range node.Array() {
		role := msg.Get("role").String()
		if role != "system" && role != "developer" {
			continue
		}
		out = append(out, textPaths(body, root+"."+strconv.Itoa(i)+".content", textField)...)
	}
	return out
}

func apply(text string, rules []*regexp.Regexp) (string, int) {
	hits := 0
	for _, rule := range rules {
		matches := rule.FindAllStringIndex(text, -1)
		if len(matches) == 0 {
			continue
		}
		hits += len(matches)
		text = rule.ReplaceAllString(text, "")
	}
	return text, hits
}

// DefaultPatterns 没配置时用的规则：Claude Code 系统提示里“两用安全工具要授权”那句。
// Google Antigravity 对带这句的请求回 "Resource has been exhausted" 的假 429。
var DefaultPatterns = []string{
	`Dual-use security tools \(C2 frameworks[^.]*\.`,
}

// Compile 把规则文本编译成正则；写错的跳过，并在 bad 里返回出错的那几条。
func Compile(patterns []string) (rules []*regexp.Regexp, bad []string) {
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			bad = append(bad, pattern)
			continue
		}
		rules = append(rules, re)
	}
	return rules, bad
}
