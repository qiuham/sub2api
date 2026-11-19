package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type unexplained429CooldownCache struct {
	mu       sync.Mutex
	loadedAt time.Time
	value    time.Duration
}

var antigravity429Cooldown unexplained429CooldownCache

// AntigravityUnexplained429Cooldown 没带额度说明的 Antigravity 429 要把模型锁多久。
//
// 设置 antigravity_unexplained_429_cooldown_seconds 是整数秒：
//   - 没有这一项、读库出错、写错或小于等于 0：不锁（返回 0）
//   - 大于 0：锁这么多秒
//
// 读出来缓存 5 秒，和系统提示清洗规则一样。
func (s *SettingService) AntigravityUnexplained429Cooldown(ctx context.Context) time.Duration {
	if s == nil || s.settingRepo == nil {
		return 0
	}
	c := &antigravity429Cooldown
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loadedAt.IsZero() && time.Since(c.loadedAt) < systemPromptStripCacheTTL {
		return c.value
	}
	c.loadedAt = time.Now()
	c.value = 0
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyAntigravityUnexplained429CooldownSeconds)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			logger.LegacyPrintf("service.setting", "antigravity_unexplained_429_cooldown_seconds read failed, not locking: %v", err)
		}
		return 0
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		logger.LegacyPrintf("service.setting", "antigravity_unexplained_429_cooldown_seconds invalid %q, not locking", raw)
		return 0
	}
	if n > 0 {
		c.value = time.Duration(n) * time.Second
	}
	return c.value
}
