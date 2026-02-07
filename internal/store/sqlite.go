package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xiaopang/fusionapi/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

// Store 数据存储
type Store struct {
	db *sql.DB
}

// New 创建存储实例
func New(dbPath string) (*Store, error) {
	// 确保目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// migrate 数据库迁移
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key TEXT NOT NULL DEFAULT '',
		priority INTEGER DEFAULT 1,
		weight INTEGER DEFAULT 100,
		enabled INTEGER DEFAULT 1,
		capabilities TEXT,
		cpa_config TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS request_logs (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		source_id TEXT,
		source_name TEXT,
		model TEXT,
		has_tools INTEGER,
		has_thinking INTEGER,
		stream INTEGER,
		success INTEGER,
		status_code INTEGER,
		latency_ms INTEGER,
		prompt_tokens INTEGER,
		completion_tokens INTEGER,
		total_tokens INTEGER,
		error TEXT,
		failover_from TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON request_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_logs_source ON request_logs(source_id);
	CREATE INDEX IF NOT EXISTS idx_logs_model ON request_logs(model);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// 增量迁移：为旧数据库添加 cpa_config 列
	s.db.Exec("ALTER TABLE sources ADD COLUMN cpa_config TEXT")

	// API Keys table
	s.db.Exec(`CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		key TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		enabled INTEGER DEFAULT 1,
		limits TEXT,
		allowed_tools TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME
	)`)

	// request_logs 增量迁移
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN client_ip TEXT DEFAULT ''")
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN client_tool TEXT DEFAULT ''")
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN api_key_id TEXT DEFAULT ''")
	s.db.Exec("ALTER TABLE request_logs ADD COLUMN fc_compat_used INTEGER DEFAULT 0")

	return nil
}

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// === Source CRUD ===

// SaveSource 保存源
func (s *Store) SaveSource(src *model.Source) error {
	caps, _ := json.Marshal(src.Capabilities)
	cpaJSON := ""
	if src.CPA != nil {
		b, _ := json.Marshal(src.CPA)
		cpaJSON = string(b)
	}
	_, err := s.db.Exec(`
		INSERT INTO sources (id, name, type, base_url, api_key, priority, weight, enabled, capabilities, cpa_config, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			priority = excluded.priority,
			weight = excluded.weight,
			enabled = excluded.enabled,
			capabilities = excluded.capabilities,
			cpa_config = excluded.cpa_config,
			updated_at = CURRENT_TIMESTAMP
	`, src.ID, src.Name, src.Type, src.BaseURL, src.APIKey, src.Priority, src.Weight, src.Enabled, string(caps), cpaJSON)
	return err
}

// GetSource 获取源
func (s *Store) GetSource(id string) (*model.Source, error) {
	row := s.db.QueryRow(`
		SELECT id, name, type, base_url, api_key, priority, weight, enabled, capabilities, COALESCE(cpa_config, '')
		FROM sources WHERE id = ?
	`, id)

	var src model.Source
	var capsJSON, cpaJSON string
	err := row.Scan(&src.ID, &src.Name, &src.Type, &src.BaseURL, &src.APIKey,
		&src.Priority, &src.Weight, &src.Enabled, &capsJSON, &cpaJSON)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(capsJSON), &src.Capabilities)
	if cpaJSON != "" {
		src.CPA = &model.CPAConfig{}
		json.Unmarshal([]byte(cpaJSON), src.CPA)
	}
	return &src, nil
}

// ListSources 列出所有源
func (s *Store) ListSources() ([]*model.Source, error) {
	rows, err := s.db.Query(`
		SELECT id, name, type, base_url, api_key, priority, weight, enabled, capabilities, COALESCE(cpa_config, '')
		FROM sources ORDER BY priority, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []*model.Source
	for rows.Next() {
		var src model.Source
		var capsJSON, cpaJSON string
		if err := rows.Scan(&src.ID, &src.Name, &src.Type, &src.BaseURL, &src.APIKey,
			&src.Priority, &src.Weight, &src.Enabled, &capsJSON, &cpaJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(capsJSON), &src.Capabilities)
		if cpaJSON != "" {
			src.CPA = &model.CPAConfig{}
			json.Unmarshal([]byte(cpaJSON), src.CPA)
		}
		sources = append(sources, &src)
	}
	return sources, nil
}

// DeleteSource 删除源
func (s *Store) DeleteSource(id string) error {
	_, err := s.db.Exec("DELETE FROM sources WHERE id = ?", id)
	return err
}

// === Request Logs ===

// SaveLog 保存请求日志
func (s *Store) SaveLog(log *model.RequestLog) error {
	_, err := s.db.Exec(`
		INSERT INTO request_logs (id, timestamp, source_id, source_name, model,
			has_tools, has_thinking, stream, success, status_code, latency_ms,
			prompt_tokens, completion_tokens, total_tokens, error, failover_from,
			client_ip, client_tool, api_key_id, fc_compat_used)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.Timestamp, log.SourceID, log.SourceName, log.Model,
		log.HasTools, log.HasThinking, log.Stream, log.Success, log.StatusCode, log.LatencyMs,
		log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.Error, log.FailoverFrom,
		log.ClientIP, log.ClientTool, log.APIKeyID, log.FCCompatUsed)
	return err
}

// QueryLogs 查询日志
func (s *Store) QueryLogs(query *model.LogQuery) ([]*model.RequestLog, error) {
	sql := "SELECT id, timestamp, source_id, source_name, model, has_tools, has_thinking, stream, success, status_code, latency_ms, prompt_tokens, completion_tokens, total_tokens, error, failover_from, COALESCE(client_ip, ''), COALESCE(client_tool, ''), COALESCE(api_key_id, ''), COALESCE(fc_compat_used, 0) FROM request_logs WHERE 1=1"
	args := []any{}

	if query.SourceID != "" {
		sql += " AND source_id = ?"
		args = append(args, query.SourceID)
	}
	if query.Model != "" {
		sql += " AND model = ?"
		args = append(args, query.Model)
	}
	if query.Success != nil {
		sql += " AND success = ?"
		args = append(args, *query.Success)
	}
	if !query.StartTime.IsZero() {
		sql += " AND timestamp >= ?"
		args = append(args, query.StartTime)
	}
	if !query.EndTime.IsZero() {
		sql += " AND timestamp <= ?"
		args = append(args, query.EndTime)
	}
	if query.ClientTool != "" {
		sql += " AND client_tool = ?"
		args = append(args, query.ClientTool)
	}
	if query.APIKeyID != "" {
		sql += " AND api_key_id = ?"
		args = append(args, query.APIKeyID)
	}

	sql += " ORDER BY timestamp DESC"

	if query.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", query.Limit)
	} else {
		sql += " LIMIT 100"
	}
	if query.Offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", query.Offset)
	}

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*model.RequestLog
	for rows.Next() {
		var log model.RequestLog
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.SourceID, &log.SourceName, &log.Model,
			&log.HasTools, &log.HasThinking, &log.Stream, &log.Success, &log.StatusCode, &log.LatencyMs,
			&log.PromptTokens, &log.CompletionTokens, &log.TotalTokens, &log.Error, &log.FailoverFrom,
			&log.ClientIP, &log.ClientTool, &log.APIKeyID, &log.FCCompatUsed); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}
	return logs, nil
}

// GetDailyStats 获取每日统计
func (s *Store) GetDailyStats(days int) ([]*model.DailyStats, error) {
	rows, err := s.db.Query(`
		SELECT
			date(timestamp) as date,
			COUNT(*) as total_requests,
			ROUND(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) * 100.0 / COUNT(*), 2) as success_rate,
			SUM(total_tokens) as total_tokens,
			ROUND(AVG(latency_ms), 2) as avg_latency
		FROM request_logs
		WHERE timestamp >= date('now', ?)
		GROUP BY date(timestamp)
		ORDER BY date DESC
	`, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*model.DailyStats
	for rows.Next() {
		var s model.DailyStats
		if err := rows.Scan(&s.Date, &s.TotalRequests, &s.SuccessRate, &s.TotalTokens, &s.AvgLatency); err != nil {
			return nil, err
		}
		stats = append(stats, &s)
	}
	return stats, nil
}

// GetSourceStats 获取源统计
func (s *Store) GetSourceStats(days int) ([]*model.SourceStats, error) {
	rows, err := s.db.Query(`
		SELECT
			source_id,
			source_name,
			COUNT(*) as request_count,
			ROUND(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) * 100.0 / COUNT(*), 2) as success_rate,
			ROUND(AVG(latency_ms), 2) as avg_latency,
			SUM(total_tokens) as total_tokens
		FROM request_logs
		WHERE timestamp >= date('now', ?)
		GROUP BY source_id
		ORDER BY request_count DESC
	`, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*model.SourceStats
	for rows.Next() {
		var s model.SourceStats
		if err := rows.Scan(&s.SourceID, &s.SourceName, &s.RequestCount, &s.SuccessRate, &s.AvgLatency, &s.TotalTokens); err != nil {
			return nil, err
		}
		stats = append(stats, &s)
	}
	return stats, nil
}

// CleanOldLogs 清理过期日志
func (s *Store) CleanOldLogs(retentionDays int) (int64, error) {
	result, err := s.db.Exec(`
		DELETE FROM request_logs
		WHERE timestamp < date('now', ?)
	`, fmt.Sprintf("-%d days", retentionDays))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// === API Keys CRUD ===

// SaveAPIKey saves or updates an API key
func (s *Store) SaveAPIKey(key *model.APIKey) error {
	limitsJSON, _ := json.Marshal(key.Limits)
	toolsJSON, _ := json.Marshal(key.AllowedTools)
	_, err := s.db.Exec(`
		INSERT INTO api_keys (id, key, name, enabled, limits, allowed_tools, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			enabled = excluded.enabled,
			limits = excluded.limits,
			allowed_tools = excluded.allowed_tools,
			last_used_at = excluded.last_used_at
	`, key.ID, key.Key, key.Name, key.Enabled, string(limitsJSON), string(toolsJSON), key.CreatedAt, key.LastUsedAt)
	return err
}

// GetAPIKey 获取 API Key
func (s *Store) GetAPIKey(id string) (*model.APIKey, error) {
	row := s.db.QueryRow(`
		SELECT id, key, name, enabled, COALESCE(limits, '{}'), COALESCE(allowed_tools, '[]'), created_at, COALESCE(last_used_at, created_at)
		FROM api_keys WHERE id = ?
	`, id)
	return s.scanAPIKey(row)
}

// GetAPIKeyByKey 根据 key 查询
func (s *Store) GetAPIKeyByKey(key string) (*model.APIKey, error) {
	row := s.db.QueryRow(`
		SELECT id, key, name, enabled, COALESCE(limits, '{}'), COALESCE(allowed_tools, '[]'), created_at, COALESCE(last_used_at, created_at)
		FROM api_keys WHERE key = ?
	`, key)
	return s.scanAPIKey(row)
}

// scanAPIKey 扫描单行 API Key
func (s *Store) scanAPIKey(row *sql.Row) (*model.APIKey, error) {
	var ak model.APIKey
	var limitsJSON, toolsJSON string
	err := row.Scan(&ak.ID, &ak.Key, &ak.Name, &ak.Enabled, &limitsJSON, &toolsJSON, &ak.CreatedAt, &ak.LastUsedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(limitsJSON), &ak.Limits)
	json.Unmarshal([]byte(toolsJSON), &ak.AllowedTools)
	return &ak, nil
}

// ListAPIKeys 列出所有 API Key
func (s *Store) ListAPIKeys() ([]*model.APIKey, error) {
	rows, err := s.db.Query(`
		SELECT id, key, name, enabled, COALESCE(limits, '{}'), COALESCE(allowed_tools, '[]'), created_at, COALESCE(last_used_at, created_at)
		FROM api_keys ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*model.APIKey
	for rows.Next() {
		var ak model.APIKey
		var limitsJSON, toolsJSON string
		if err := rows.Scan(&ak.ID, &ak.Key, &ak.Name, &ak.Enabled, &limitsJSON, &toolsJSON, &ak.CreatedAt, &ak.LastUsedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(limitsJSON), &ak.Limits)
		json.Unmarshal([]byte(toolsJSON), &ak.AllowedTools)
		keys = append(keys, &ak)
	}
	return keys, nil
}

// DeleteAPIKey 删除 API Key
func (s *Store) DeleteAPIKey(id string) error {
	_, err := s.db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	return err
}

// UpdateAPIKeyLastUsed 更新 API Key 最后使用时间
func (s *Store) UpdateAPIKeyLastUsed(id string) error {
	_, err := s.db.Exec("UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// GetKeyDailyUsage 获取密钥当日用量
func (s *Store) GetKeyDailyUsage(keyID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM request_logs
		WHERE api_key_id = ? AND date(timestamp) = date('now')
	`, keyID).Scan(&count)
	return count, err
}

// GetToolStats 获取工具使用统计
func (s *Store) GetToolStats(days int) ([]*model.ToolStats, error) {
	rows, err := s.db.Query(`
		SELECT client_tool, COUNT(*) as request_count, MAX(timestamp) as last_used
		FROM request_logs
		WHERE client_tool != '' AND timestamp >= date('now', ?)
		GROUP BY client_tool
		ORDER BY request_count DESC
	`, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*model.ToolStats
	for rows.Next() {
		var ts model.ToolStats
		if err := rows.Scan(&ts.Tool, &ts.RequestCount, &ts.LastUsedAt); err != nil {
			return nil, err
		}
		stats = append(stats, &ts)
	}
	return stats, nil
}

// GetKeyUsageTrend 获取 Key 使用趋势
func (s *Store) GetKeyUsageTrend(keyID string, days int) ([]*model.KeyDailyUsage, error) {
	rows, err := s.db.Query(`
		SELECT
			date(timestamp) as date,
			COUNT(*) as request_count,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as fail_count,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(ROUND(AVG(latency_ms), 2), 0) as avg_latency
		FROM request_logs
		WHERE api_key_id = ? AND timestamp >= date('now', ?)
		GROUP BY date(timestamp)
		ORDER BY date DESC
	`, keyID, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usages []*model.KeyDailyUsage
	for rows.Next() {
		var u model.KeyDailyUsage
		if err := rows.Scan(&u.Date, &u.RequestCount, &u.SuccessCount, &u.FailCount, &u.TotalTokens, &u.AvgLatency); err != nil {
			return nil, err
		}
		usages = append(usages, &u)
	}
	return usages, nil
}
