//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileSystemPromptStripRules(t *testing.T) {
	// 没有这一项：内置默认（一条）
	require.Len(t, compileSystemPromptStripRules("", false), 1)
	// 设成 [] 或空字符串：关闭
	require.Empty(t, compileSystemPromptStripRules("[]", true))
	require.Empty(t, compileSystemPromptStripRules("", true))
	// 不是 JSON 列表：关闭（并打日志）
	require.Empty(t, compileSystemPromptStripRules("Dual-use", true))
	// 坏规则跳过，好的留下
	rules := compileSystemPromptStripRules(`["(", "foo\\.", "bar"]`, true)
	require.Len(t, rules, 2)
	require.True(t, rules[0].MatchString("foo."))
}
