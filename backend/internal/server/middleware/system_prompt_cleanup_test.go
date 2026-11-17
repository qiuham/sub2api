//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/promptclean"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fixedRules []*regexp.Regexp

func (f fixedRules) SystemPromptStripRules(context.Context) []*regexp.Regexp { return f }

const cleanupSentence = "Dual-use security tools (C2 frameworks, credential testing, exploit development) require clear authorization context: pentesting engagements, CTF competitions, security research, or defensive use cases."

func runCleanup(t *testing.T, source SystemPromptRuleSource, method, path, contentType, encoding, body string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var seen string
	r.Use(SystemPromptCleanup(source))
	r.Handle(method, path, func(c *gin.Context) {
		raw, _ := io.ReadAll(c.Request.Body)
		seen = string(raw)
		require.Equal(t, int64(len(raw)), c.Request.ContentLength)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	r.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

func TestSystemPromptCleanup_RemovesFromAnthropicSystem(t *testing.T) {
	rules, _ := promptclean.Compile(promptclean.DefaultPatterns)
	body := `{"model":"x","system":"A ` + cleanupSentence + `","messages":[{"role":"user","content":"hi"}]}`
	seen := runCleanup(t, fixedRules(rules), http.MethodPost, "/v1/messages", "application/json", "", body)
	require.NotContains(t, seen, "Dual-use")
	require.Contains(t, seen, `"content":"hi"`)
}

func TestSystemPromptCleanup_PassesThrough(t *testing.T) {
	rules, _ := promptclean.Compile(promptclean.DefaultPatterns)
	body := `{"system":"` + cleanupSentence + `"}`
	// 没有规则
	require.Equal(t, body, runCleanup(t, fixedRules(nil), http.MethodPost, "/v1/messages", "application/json", "", body))
	// 认不出的路径
	require.Equal(t, body, runCleanup(t, fixedRules(rules), http.MethodPost, "/v1/embeddings", "application/json", "", body))
	// 压缩过的请求体
	require.Equal(t, body, runCleanup(t, fixedRules(rules), http.MethodPost, "/v1/messages", "application/json", "gzip", body))
	// 不是 JSON
	require.Equal(t, body, runCleanup(t, fixedRules(rules), http.MethodPost, "/v1/messages", "multipart/form-data", "", body))
}
