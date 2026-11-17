package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/promptclean"
)

// systemPromptStripCacheTTL 规则读出来缓存多久：改了数据库里的设置，最多这么久生效，也不用每个请求都查一次库。
const systemPromptStripCacheTTL = 5 * time.Second

type systemPromptStripCache struct {
	mu       sync.Mutex
	loadedAt time.Time
	raw      string
	found    bool
	rules    []*regexp.Regexp
}

var promptStripCache systemPromptStripCache

// SystemPromptStripRules 转发前要从系统提示里删掉的规则（见 promptclean）。
//
// 设置 system_prompt_strip_patterns 是正则表达式的 JSON 列表：
//   - 没有这一项（或读库出错）：用内置默认 promptclean.DefaultPatterns
//   - "[]" 或空字符串：关闭清洗
//   - 写错的正则跳过并打日志，不影响其他规则和转发
func (s *SettingService) SystemPromptStripRules(ctx context.Context) []*regexp.Regexp {
	if s == nil || s.settingRepo == nil {
		rules, _ := promptclean.Compile(promptclean.DefaultPatterns)
		return rules
	}
	c := &promptStripCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loadedAt.IsZero() && time.Since(c.loadedAt) < systemPromptStripCacheTTL {
		return c.rules
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeySystemPromptStripPatterns)
	found := err == nil
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		logger.LegacyPrintf("service.setting", "system_prompt_strip_patterns read failed, using defaults: %v", err)
	}
	c.loadedAt = time.Now()
	if !c.rulesUnchanged(raw, found) {
		c.raw, c.found = raw, found
		c.rules = compileSystemPromptStripRules(raw, found)
	}
	return c.rules
}

func (c *systemPromptStripCache) rulesUnchanged(raw string, found bool) bool {
	return c.rules != nil && c.raw == raw && c.found == found
}

func compileSystemPromptStripRules(raw string, found bool) []*regexp.Regexp {
	patterns := promptclean.DefaultPatterns
	if found {
		patterns = nil
		if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &patterns); err != nil {
				logger.LegacyPrintf("service.setting", "system_prompt_strip_patterns is not a JSON string list, cleaning disabled: %v", err)
				return []*regexp.Regexp{}
			}
		}
	}
	rules, bad := promptclean.Compile(patterns)
	for _, pattern := range bad {
		logger.LegacyPrintf("service.setting", "system_prompt_strip_patterns: skipped invalid pattern %q", pattern)
	}
	if rules == nil {
		rules = []*regexp.Regexp{}
	}
	return rules
}
