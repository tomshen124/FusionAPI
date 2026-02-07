package model

import "time"

// APIKey API 密钥
type APIKey struct {
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	Enabled      bool      `json:"enabled"`
	Limits       KeyLimits `json:"limits"`
	AllowedTools []string  `json:"allowed_tools"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
}

// KeyLimits 密钥限制
type KeyLimits struct {
	RPM        int            `json:"rpm"`                    // 每分钟请求数，0=无限
	DailyQuota int            `json:"daily_quota"`            // 每日配额，0=无限
	Concurrent int            `json:"concurrent"`             // 并发数，0=无限
	ToolQuotas map[string]int `json:"tool_quotas,omitempty"`  // 工具名 -> 每日配额
}

// ClientInfo 客户端信息（存入 gin.Context）
type ClientInfo struct {
	KeyID string // 关联的 API Key ID
	Tool  string // 识别出的工具名
	IP    string // 客户端 IP
}

// ToolStats 工具使用统计
type ToolStats struct {
	Tool         string `json:"tool"`
	RequestCount int    `json:"request_count"`
	LastUsedAt   string `json:"last_used_at"`
}

// KeyDailyUsage Key 每日使用量
type KeyDailyUsage struct {
	Date         string  `json:"date"`
	RequestCount int     `json:"request_count"`
	SuccessCount int     `json:"success_count"`
	FailCount    int     `json:"fail_count"`
	TotalTokens  int64   `json:"total_tokens"`
	AvgLatency   float64 `json:"avg_latency_ms"`
}
