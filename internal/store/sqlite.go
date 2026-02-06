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
			prompt_tokens, completion_tokens, total_tokens, error, failover_from)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.Timestamp, log.SourceID, log.SourceName, log.Model,
		log.HasTools, log.HasThinking, log.Stream, log.Success, log.StatusCode, log.LatencyMs,
		log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.Error, log.FailoverFrom)
	return err
}

// QueryLogs 查询日志
func (s *Store) QueryLogs(query *model.LogQuery) ([]*model.RequestLog, error) {
	sql := "SELECT id, timestamp, source_id, source_name, model, has_tools, has_thinking, stream, success, status_code, latency_ms, prompt_tokens, completion_tokens, total_tokens, error, failover_from FROM request_logs WHERE 1=1"
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
			&log.PromptTokens, &log.CompletionTokens, &log.TotalTokens, &log.Error, &log.FailoverFrom); err != nil {
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
