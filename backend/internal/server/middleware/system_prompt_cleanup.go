package middleware

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/promptclean"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"

	"github.com/gin-gonic/gin"
)

// SystemPromptRuleSource 提供当前的清洗规则（service.SettingService 实现）。
type SystemPromptRuleSource interface {
	SystemPromptStripRules(ctx context.Context) []*regexp.Regexp
}

// SystemPromptCleanup 转发前按规则删掉系统提示里的指定句子，所有平台、所有入口通用。
//
// 挂载位置：每条网关链上紧跟 GroupModelAllowlist 之后，在调度和各平台转发之前。
// 只处理带 JSON 请求体的 POST，按路径认协议（见 promptclean.ProtocolForPath），只改系统提示，
// 不碰用户和助手的对话内容。没有规则、认不出协议、没匹配上时请求体原样放行。
func SystemPromptCleanup(source SystemPromptRuleSource) gin.HandlerFunc {
	return func(c *gin.Context) {
		if source == nil || c.Request == nil || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		protocol := promptclean.ProtocolForPath(c.Request.URL.Path)
		if protocol == promptclean.ProtocolUnknown || !isJSONContentType(c.GetHeader("Content-Type")) ||
			!isIdentityEncoding(c.GetHeader("Content-Encoding")) {
			c.Next()
			return
		}
		rules := source.SystemPromptStripRules(c.Request.Context())
		if len(rules) == 0 {
			c.Next()
			return
		}
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			if RejectRequestBodyReadError(c, "middleware.system_prompt_cleanup", err) {
				return
			}
			AbortWithRequestBodyReadFailure(c, "middleware.system_prompt_cleanup", err)
			return
		}
		cleaned, hits := promptclean.Clean(body, protocol, rules)
		if hits > 0 {
			logger.LegacyPrintf("middleware.system_prompt_cleanup", "removed %d system prompt match(es) path=%s", hits, c.Request.URL.Path)
		}
		requestmodel.ResetRequestBody(c.Request, cleaned)
		c.Next()
	}
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return contentType == "" || strings.Contains(contentType, "json")
}

// isIdentityEncoding 请求体没压缩：压缩过的读不出 JSON，原样放行。
func isIdentityEncoding(encoding string) bool {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	return encoding == "" || encoding == "identity"
}
