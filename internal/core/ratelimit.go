package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/xiaopang/fusionapi/internal/model"
)

const (
	AutoBanThreshold = 50               // 连续错误数阈值
	AutoBanDuration  = 30 * time.Minute // 自动封禁持续时间
)

// RateLimiter 频率限制器
type RateLimiter struct {
	mu         sync.Mutex
	windows    map[string][]time.Time // keyID -> request timestamps for RPM sliding window
	dailyCount map[string]int         // keyID+date -> count
	concurrent map[string]int         // keyID -> current concurrent count
	errorCount map[string]int         // keyID -> consecutive error count
	autoBanned map[string]time.Time   // keyID -> ban time
}

// NewRateLimiter 创建频率限制器
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		windows:    make(map[string][]time.Time),
		dailyCount: make(map[string]int),
		concurrent: make(map[string]int),
		errorCount: make(map[string]int),
		autoBanned: make(map[string]time.Time),
	}
	// Start cleanup goroutine
	go rl.cleanup()
	return rl
}

// Enter performs an atomic "check + accounting + (optional) concurrent token acquisition".
//
// It returns (allowed, reason, release).
// The release function is always non-nil and is safe to call multiple times.
func (r *RateLimiter) Enter(keyID string, limits model.KeyLimits, tool string) (bool, string, func()) {
	// Always return a non-nil release to simplify callers.
	var once sync.Once
	release := func() {
		once.Do(func() {
			if limits.Concurrent <= 0 {
				return
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.concurrent[keyID] > 0 {
				r.concurrent[keyID]--
			}
		})
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Check RPM
	if limits.RPM > 0 {
		windowStart := now.Add(-time.Minute)
		timestamps := r.windows[keyID]
		// Clean old entries
		valid := timestamps[:0]
		for _, t := range timestamps {
			if t.After(windowStart) {
				valid = append(valid, t)
			}
		}
		r.windows[keyID] = valid
		if len(valid) >= limits.RPM {
			return false, fmt.Sprintf("RPM limit exceeded (%d/%d)", len(valid), limits.RPM), release
		}
	}

	// Check daily quota
	if limits.DailyQuota > 0 {
		dateKey := keyID + ":" + now.Format("2006-01-02")
		if r.dailyCount[dateKey] >= limits.DailyQuota {
			return false, fmt.Sprintf("Daily quota exceeded (%d/%d)", r.dailyCount[dateKey], limits.DailyQuota), release
		}
	}

	// Check tool quota first (avoid charging RPM/daily quota if tool quota is exceeded)
	var toolDateKey string
	if tool != "" && tool != "unknown" && len(limits.ToolQuotas) > 0 {
		if quota, ok := limits.ToolQuotas[tool]; ok && quota > 0 {
			toolDateKey = keyID + ":" + tool + ":" + now.Format("2006-01-02")
			current := r.dailyCount[toolDateKey]
			if current >= quota {
				return false, fmt.Sprintf("Tool quota exceeded for %s (%d/%d)", tool, current, quota), release
			}
		}
	}

	// Check concurrent
	if limits.Concurrent > 0 {
		if r.concurrent[keyID] >= limits.Concurrent {
			return false, fmt.Sprintf("Concurrent limit exceeded (%d/%d)", r.concurrent[keyID], limits.Concurrent), release
		}
	}

	// Record accounting (only after all checks pass)
	if limits.RPM > 0 {
		r.windows[keyID] = append(r.windows[keyID], now)
	}
	if limits.DailyQuota > 0 {
		dateKey := keyID + ":" + now.Format("2006-01-02")
		r.dailyCount[dateKey]++
	}
	if toolDateKey != "" {
		r.dailyCount[toolDateKey]++
	}
	if limits.Concurrent > 0 {
		r.concurrent[keyID]++
	}

	return true, "", release
}

// Allow 检查是否允许请求
// Returns (allowed bool, reason string)
func (r *RateLimiter) Allow(keyID string, limits model.KeyLimits) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Check RPM
	if limits.RPM > 0 {
		windowStart := now.Add(-time.Minute)
		timestamps := r.windows[keyID]
		// Clean old entries
		valid := timestamps[:0]
		for _, t := range timestamps {
			if t.After(windowStart) {
				valid = append(valid, t)
			}
		}
		r.windows[keyID] = valid
		if len(valid) >= limits.RPM {
			return false, fmt.Sprintf("RPM limit exceeded (%d/%d)", len(valid), limits.RPM)
		}
	}

	// Check daily quota
	if limits.DailyQuota > 0 {
		dateKey := keyID + ":" + now.Format("2006-01-02")
		if r.dailyCount[dateKey] >= limits.DailyQuota {
			return false, fmt.Sprintf("Daily quota exceeded (%d/%d)", r.dailyCount[dateKey], limits.DailyQuota)
		}
	}

	// Record the request
	if limits.RPM > 0 {
		r.windows[keyID] = append(r.windows[keyID], now)
	}
	if limits.DailyQuota > 0 {
		dateKey := keyID + ":" + now.Format("2006-01-02")
		r.dailyCount[dateKey]++
	}

	return true, ""
}

// AllowWithTool 检查是否允许请求（含工具配额）
func (r *RateLimiter) AllowWithTool(keyID string, limits model.KeyLimits, tool string) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Check RPM
	if limits.RPM > 0 {
		windowStart := now.Add(-time.Minute)
		timestamps := r.windows[keyID]
		valid := timestamps[:0]
		for _, t := range timestamps {
			if t.After(windowStart) {
				valid = append(valid, t)
			}
		}
		r.windows[keyID] = valid
		if len(valid) >= limits.RPM {
			return false, fmt.Sprintf("RPM limit exceeded (%d/%d)", len(valid), limits.RPM)
		}
	}

	// Check daily quota
	if limits.DailyQuota > 0 {
		dateKey := keyID + ":" + now.Format("2006-01-02")
		if r.dailyCount[dateKey] >= limits.DailyQuota {
			return false, fmt.Sprintf("Daily quota exceeded (%d/%d)", r.dailyCount[dateKey], limits.DailyQuota)
		}
	}

	// Check tool quota before charging other quotas
	var toolDateKey string
	if tool != "" && tool != "unknown" && len(limits.ToolQuotas) > 0 {
		if quota, ok := limits.ToolQuotas[tool]; ok && quota > 0 {
			toolDateKey = keyID + ":" + tool + ":" + now.Format("2006-01-02")
			current := r.dailyCount[toolDateKey]
			if current >= quota {
				return false, fmt.Sprintf("Tool quota exceeded for %s (%d/%d)", tool, current, quota)
			}
		}
	}

	// Record accounting
	if limits.RPM > 0 {
		r.windows[keyID] = append(r.windows[keyID], now)
	}
	if limits.DailyQuota > 0 {
		dateKey := keyID + ":" + now.Format("2006-01-02")
		r.dailyCount[dateKey]++
	}
	if toolDateKey != "" {
		r.dailyCount[toolDateKey]++
	}

	return true, ""
}

// AcquireConcurrent 获取并发令牌
func (r *RateLimiter) AcquireConcurrent(keyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.concurrent[keyID]++
}

// ReleaseConcurrent 释放并发令牌
func (r *RateLimiter) ReleaseConcurrent(keyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.concurrent[keyID] > 0 {
		r.concurrent[keyID]--
	}
}

// RecordError 记录请求错误
func (r *RateLimiter) RecordError(keyID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorCount[keyID]++
	if r.errorCount[keyID] >= AutoBanThreshold {
		r.autoBanned[keyID] = time.Now()
		return true // 触发自动封禁
	}
	return false
}

// RecordSuccess 记录请求成功（重置错误计数）
func (r *RateLimiter) RecordSuccess(keyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorCount[keyID] = 0
}

// IsAutoBanned 检查是否被自动封禁
func (r *RateLimiter) IsAutoBanned(keyID string) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	banTime, exists := r.autoBanned[keyID]
	if !exists {
		return false, 0
	}
	elapsed := time.Since(banTime)
	if elapsed >= AutoBanDuration {
		delete(r.autoBanned, keyID)
		delete(r.errorCount, keyID)
		return false, 0
	}
	return true, AutoBanDuration - elapsed
}

// cleanup periodically cleans old data
func (r *RateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		windowStart := now.Add(-time.Minute)
		// Clean RPM windows
		for k, timestamps := range r.windows {
			valid := timestamps[:0]
			for _, t := range timestamps {
				if t.After(windowStart) {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(r.windows, k)
			} else {
				r.windows[k] = valid
			}
		}
		// Clean old daily counts (keep only today)
		today := now.Format("2006-01-02")
		for k := range r.dailyCount {
			if len(k) >= 10 && k[len(k)-10:] != today {
				delete(r.dailyCount, k)
			}
		}
		// Clean expired auto-bans
		for k, banTime := range r.autoBanned {
			if now.Sub(banTime) >= AutoBanDuration {
				delete(r.autoBanned, k)
				delete(r.errorCount, k)
			}
		}
		r.mu.Unlock()
	}
}
