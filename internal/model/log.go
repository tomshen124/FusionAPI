package model

import "time"

// RequestLog 请求日志
type RequestLog struct {
	ID         string    `json:"id"`
	RequestID  string    `json:"request_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	SourceID   string    `json:"source_id"`
	SourceName string    `json:"source_name"`
	Model      string    `json:"model"`

	// 请求信息
	HasTools    bool `json:"has_tools"`
	HasThinking bool `json:"has_thinking"`
	Stream      bool `json:"stream"`

	// 响应信息
	Success    bool  `json:"success"`
	StatusCode int   `json:"status_code"`
	LatencyMs  int64 `json:"latency_ms"`

	// Token 统计
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// 错误信息
	Error string `json:"error,omitempty"`

	// Failover 记录
	FailoverFrom string `json:"failover_from,omitempty"`

	// 客户端信息
	ClientIP   string `json:"client_ip,omitempty"`
	ClientTool string `json:"client_tool,omitempty"`
	APIKeyID   string `json:"api_key_id,omitempty"`

	// FC 兼容层
	FCCompatUsed bool `json:"fc_compat_used,omitempty"`
}

// UsageStats 用量统计
type UsageStats struct {
	Date     string `json:"date"` // 2026-02-06
	SourceID string `json:"source_id"`
	Model    string `json:"model"`

	RequestCount int `json:"request_count"`
	SuccessCount int `json:"success_count"`
	FailCount    int `json:"fail_count"`

	TotalTokens int64   `json:"total_tokens"`
	AvgLatency  float64 `json:"avg_latency_ms"`
}

// DailyStats 每日统计汇总
type DailyStats struct {
	Date          string  `json:"date"`
	TotalRequests int     `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`
	TotalTokens   int64   `json:"total_tokens"`
	AvgLatency    float64 `json:"avg_latency_ms"`
}

// SourceStats 源统计
type SourceStats struct {
	SourceID     string  `json:"source_id"`
	SourceName   string  `json:"source_name"`
	RequestCount int     `json:"request_count"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatency   float64 `json:"avg_latency_ms"`
	TotalTokens  int64   `json:"total_tokens"`
}

// LogQuery 日志查询参数
type LogQuery struct {
	SourceID   string    `form:"source_id"`
	RequestID  string    `form:"request_id"`
	Model      string    `form:"model"`
	Success    *bool     `form:"success"`
	StartTime  time.Time `form:"start_time"`
	EndTime    time.Time `form:"end_time"`
	Limit      int       `form:"limit"`
	Offset     int       `form:"offset"`
	ClientTool string    `form:"client_tool"`
	APIKeyID   string    `form:"api_key_id"`
	FCCompat   *bool     `form:"fc_compat"`
}
