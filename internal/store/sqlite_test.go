package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaopang/fusionapi/internal/model"
)

func tempDB(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return s, func() {
		s.Close()
		os.RemoveAll(dir)
	}
}

// === Migration Tests ===

func TestNew_CreatesDirAndDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "deep", "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("expected database file to be created")
	}
}

func TestMigrate_TablesExist(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	tables := []string{"sources", "request_logs", "api_keys"}
	for _, table := range tables {
		var count int
		err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Fatalf("failed to check table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("expected table %s to exist", table)
		}
	}
}

func TestMigrate_ColumnsExist(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	// Check request_logs has fc_compat_used column
	_, err := s.db.Exec("SELECT fc_compat_used FROM request_logs LIMIT 0")
	if err != nil {
		t.Errorf("expected fc_compat_used column: %v", err)
	}

	// Check request_logs has client_ip column
	_, err = s.db.Exec("SELECT client_ip FROM request_logs LIMIT 0")
	if err != nil {
		t.Errorf("expected client_ip column: %v", err)
	}

	// Check request_logs has client_tool column
	_, err = s.db.Exec("SELECT client_tool FROM request_logs LIMIT 0")
	if err != nil {
		t.Errorf("expected client_tool column: %v", err)
	}

	// Check sources has cpa_config column
	_, err = s.db.Exec("SELECT cpa_config FROM sources LIMIT 0")
	if err != nil {
		t.Errorf("expected cpa_config column: %v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	// Running migrate again should not fail
	if err := s.migrate(); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
}

// === Source CRUD Tests ===

func TestSaveAndGetSource(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	src := &model.Source{
		ID:       "src-1",
		Name:     "Test Source",
		Type:     model.SourceTypeOpenAI,
		BaseURL:  "https://api.openai.com",
		APIKey:   "sk-test",
		Priority: 1,
		Weight:   100,
		Enabled:  true,
		Capabilities: model.Capabilities{
			FunctionCalling:  true,
			ExtendedThinking: false,
			Vision:           true,
			Models:           []string{"gpt-4", "gpt-3.5-turbo"},
		},
	}

	if err := s.SaveSource(src); err != nil {
		t.Fatalf("SaveSource failed: %v", err)
	}

	got, err := s.GetSource("src-1")
	if err != nil {
		t.Fatalf("GetSource failed: %v", err)
	}

	if got.Name != "Test Source" {
		t.Errorf("expected name 'Test Source', got '%s'", got.Name)
	}
	if got.Type != model.SourceTypeOpenAI {
		t.Errorf("expected type openai, got %s", got.Type)
	}
	if !got.Capabilities.FunctionCalling {
		t.Error("expected FunctionCalling=true")
	}
	if !got.Capabilities.Vision {
		t.Error("expected Vision=true")
	}
}

func TestSaveSource_Upsert(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	src := &model.Source{
		ID:      "src-1",
		Name:    "Original",
		Type:    model.SourceTypeOpenAI,
		BaseURL: "https://api.openai.com",
		Enabled: true,
	}
	s.SaveSource(src)

	src.Name = "Updated"
	s.SaveSource(src)

	got, _ := s.GetSource("src-1")
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", got.Name)
	}
}

func TestSaveSource_WithCPA(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	src := &model.Source{
		ID:      "src-cpa",
		Name:    "CPA Source",
		Type:    model.SourceTypeCPA,
		BaseURL: "https://cpa.example.com",
		Enabled: true,
		CPA: &model.CPAConfig{
			Providers:   []string{"gemini", "claude"},
			AccountMode: "single",
			AutoDetect:  true,
		},
	}

	s.SaveSource(src)
	got, _ := s.GetSource("src-cpa")

	if got.CPA == nil {
		t.Fatal("expected CPA config to be saved")
	}
	if len(got.CPA.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(got.CPA.Providers))
	}
	if !got.CPA.AutoDetect {
		t.Error("expected AutoDetect=true")
	}
}

func TestListSources(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		s.SaveSource(&model.Source{
			ID:      "src-" + string(rune('A'+i)),
			Name:    "Source " + string(rune('A'+i)),
			Type:    model.SourceTypeOpenAI,
			BaseURL: "https://api.openai.com",
			Enabled: true,
		})
	}

	sources, err := s.ListSources()
	if err != nil {
		t.Fatalf("ListSources failed: %v", err)
	}
	if len(sources) != 3 {
		t.Errorf("expected 3 sources, got %d", len(sources))
	}
}

func TestDeleteSource(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveSource(&model.Source{ID: "src-del", Name: "ToDelete", Type: model.SourceTypeOpenAI, BaseURL: "https://example.com"})
	s.DeleteSource("src-del")

	_, err := s.GetSource("src-del")
	if err == nil {
		t.Error("expected error after delete")
	}
}

// === Request Log Tests ===

func TestSaveAndQueryLog(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)
	log := &model.RequestLog{
		ID:               "log-1",
		Timestamp:        now,
		SourceID:         "src-1",
		SourceName:       "Test",
		Model:            "gpt-4",
		HasTools:         true,
		HasThinking:      false,
		Stream:           false,
		Success:          true,
		StatusCode:       200,
		LatencyMs:        150,
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		ClientIP:         "127.0.0.1",
		ClientTool:       "cursor",
		APIKeyID:         "key-1",
		FCCompatUsed:     true,
	}

	if err := s.SaveLog(log); err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}

	logs, err := s.QueryLogs(&model.LogQuery{Limit: 10})
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	got := logs[0]
	if got.ID != "log-1" {
		t.Errorf("expected ID 'log-1', got '%s'", got.ID)
	}
	if got.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", got.Model)
	}
	if !got.HasTools {
		t.Error("expected HasTools=true")
	}
	if !got.FCCompatUsed {
		t.Error("expected FCCompatUsed=true")
	}
	if got.ClientTool != "cursor" {
		t.Errorf("expected ClientTool='cursor', got '%s'", got.ClientTool)
	}
	if got.ClientIP != "127.0.0.1" {
		t.Errorf("expected ClientIP='127.0.0.1', got '%s'", got.ClientIP)
	}
}

func TestQueryLogs_FilterBySource(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		sid := "src-A"
		if i >= 3 {
			sid = "src-B"
		}
		s.SaveLog(&model.RequestLog{
			ID:        "log-" + string(rune('0'+i)),
			Timestamp: time.Now(),
			SourceID:  sid,
			Model:     "gpt-4",
			Success:   true,
		})
	}

	logs, _ := s.QueryLogs(&model.LogQuery{SourceID: "src-A"})
	if len(logs) != 3 {
		t.Errorf("expected 3 logs for src-A, got %d", len(logs))
	}
}

func TestQueryLogs_FilterByModel(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveLog(&model.RequestLog{ID: "l1", Timestamp: time.Now(), Model: "gpt-4", Success: true})
	s.SaveLog(&model.RequestLog{ID: "l2", Timestamp: time.Now(), Model: "claude-3", Success: true})

	logs, _ := s.QueryLogs(&model.LogQuery{Model: "gpt-4"})
	if len(logs) != 1 {
		t.Errorf("expected 1 log for gpt-4, got %d", len(logs))
	}
}

func TestQueryLogs_FilterBySuccess(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveLog(&model.RequestLog{ID: "l1", Timestamp: time.Now(), Model: "gpt-4", Success: true})
	s.SaveLog(&model.RequestLog{ID: "l2", Timestamp: time.Now(), Model: "gpt-4", Success: false})

	success := true
	logs, _ := s.QueryLogs(&model.LogQuery{Success: &success})
	if len(logs) != 1 {
		t.Errorf("expected 1 successful log, got %d", len(logs))
	}
}

func TestQueryLogs_FilterByClientTool(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveLog(&model.RequestLog{ID: "l1", Timestamp: time.Now(), Model: "gpt-4", ClientTool: "cursor"})
	s.SaveLog(&model.RequestLog{ID: "l2", Timestamp: time.Now(), Model: "gpt-4", ClientTool: "copilot"})

	logs, _ := s.QueryLogs(&model.LogQuery{ClientTool: "cursor"})
	if len(logs) != 1 {
		t.Errorf("expected 1 log for cursor, got %d", len(logs))
	}
}

func TestQueryLogs_Pagination(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	for i := 0; i < 10; i++ {
		s.SaveLog(&model.RequestLog{
			ID:        "log-" + string(rune('A'+i)),
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			Model:     "gpt-4",
		})
	}

	// Page 1
	logs, _ := s.QueryLogs(&model.LogQuery{Limit: 3, Offset: 0})
	if len(logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(logs))
	}

	// Page 2
	logs, _ = s.QueryLogs(&model.LogQuery{Limit: 3, Offset: 3})
	if len(logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(logs))
	}
}

// === CleanOldLogs ===

func TestCleanOldLogs(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	// Insert an old log (10 days ago)
	s.SaveLog(&model.RequestLog{
		ID:        "old-log",
		Timestamp: time.Now().AddDate(0, 0, -10),
		Model:     "gpt-4",
	})
	// Insert a recent log
	s.SaveLog(&model.RequestLog{
		ID:        "new-log",
		Timestamp: time.Now(),
		Model:     "gpt-4",
	})

	deleted, err := s.CleanOldLogs(7)
	if err != nil {
		t.Fatalf("CleanOldLogs failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	logs, _ := s.QueryLogs(&model.LogQuery{Limit: 100})
	if len(logs) != 1 {
		t.Errorf("expected 1 remaining log, got %d", len(logs))
	}
	if logs[0].ID != "new-log" {
		t.Errorf("expected 'new-log' to remain, got '%s'", logs[0].ID)
	}
}

// === DailyStats ===

func TestGetDailyStats(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveLog(&model.RequestLog{ID: "l1", Timestamp: time.Now(), Model: "gpt-4", Success: true, TotalTokens: 100, LatencyMs: 200})
	s.SaveLog(&model.RequestLog{ID: "l2", Timestamp: time.Now(), Model: "gpt-4", Success: false, TotalTokens: 50, LatencyMs: 500})

	stats, err := s.GetDailyStats(7)
	if err != nil {
		t.Fatalf("GetDailyStats failed: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 day of stats, got %d", len(stats))
	}
	if stats[0].TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", stats[0].TotalRequests)
	}
}

// === API Key CRUD ===

func TestSaveAndGetAPIKey(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)
	key := &model.APIKey{
		ID:         "key-1",
		Key:        "sk-fa-testkey123",
		Name:       "Test Key",
		Enabled:    true,
		Limits:     model.KeyLimits{RPM: 60, DailyQuota: 1000},
		CreatedAt:  now,
		LastUsedAt: now,
	}

	if err := s.SaveAPIKey(key); err != nil {
		t.Fatalf("SaveAPIKey failed: %v", err)
	}

	got, err := s.GetAPIKey("key-1")
	if err != nil {
		t.Fatalf("GetAPIKey failed: %v", err)
	}
	if got.Name != "Test Key" {
		t.Errorf("expected name 'Test Key', got '%s'", got.Name)
	}
	if got.Limits.RPM != 60 {
		t.Errorf("expected RPM=60, got %d", got.Limits.RPM)
	}
}

func TestGetAPIKeyByKey(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)
	key := &model.APIKey{
		ID:         "key-2",
		Key:        "sk-fa-unique",
		Name:       "By Key",
		Enabled:    true,
		CreatedAt:  now,
		LastUsedAt: now,
	}
	s.SaveAPIKey(key)

	got, err := s.GetAPIKeyByKey("sk-fa-unique")
	if err != nil {
		t.Fatalf("GetAPIKeyByKey failed: %v", err)
	}
	if got.ID != "key-2" {
		t.Errorf("expected ID 'key-2', got '%s'", got.ID)
	}
}

func TestDeleteAPIKey(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveAPIKey(&model.APIKey{ID: "key-del", Key: "sk-fa-del", CreatedAt: time.Now(), LastUsedAt: time.Now()})
	s.DeleteAPIKey("key-del")

	_, err := s.GetAPIKey("key-del")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestListAPIKeys(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		s.SaveAPIKey(&model.APIKey{
			ID:         "key-" + string(rune('A'+i)),
			Key:        "sk-fa-" + string(rune('A'+i)),
			Name:       "Key " + string(rune('A'+i)),
			CreatedAt:  now,
			LastUsedAt: now,
		})
	}

	keys, err := s.ListAPIKeys()
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

// === GetSourceStats ===

func TestGetSourceStats(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	s.SaveLog(&model.RequestLog{ID: "l1", Timestamp: time.Now(), SourceID: "s1", SourceName: "Source1", Model: "gpt-4", Success: true, TotalTokens: 100, LatencyMs: 200})
	s.SaveLog(&model.RequestLog{ID: "l2", Timestamp: time.Now(), SourceID: "s1", SourceName: "Source1", Model: "gpt-4", Success: true, TotalTokens: 200, LatencyMs: 300})

	stats, err := s.GetSourceStats(7)
	if err != nil {
		t.Fatalf("GetSourceStats failed: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 source stat, got %d", len(stats))
	}
	if stats[0].RequestCount != 2 {
		t.Errorf("expected 2 requests, got %d", stats[0].RequestCount)
	}
}
