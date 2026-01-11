package scheduler

import (
	"context"
	"testing"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
)

func createTestManager(t *testing.T) *db.Manager {
	t.Helper()

	readOnly := false
	cfg := []config.DatabaseConfig{
		{
			Name:     "test",
			Type:     "sqlite",
			Path:     ":memory:",
			ReadOnly: &readOnly,
		},
	}

	manager, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Create test tables
	driver, _ := manager.Get("test")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			status TEXT DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	for _, name := range []string{"Alice", "Bob", "Charlie"} {
		_, err = driver.Query(ctx, sessCfg,
			"INSERT INTO users (name) VALUES (@name)",
			map[string]any{"name": name},
		)
		if err != nil {
			t.Fatalf("failed to insert user: %v", err)
		}
	}

	return manager
}

// TestNew verifies scheduler creation only registers queries with schedule config
func TestNew(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queries := []config.QueryConfig{
		{
			Name:     "no_schedule",
			Database: "test",
			SQL:      "SELECT 1",
		},
		{
			Name:     "with_schedule",
			Database: "test",
			SQL:      "SELECT COUNT(*) FROM users",
			Schedule: &config.ScheduleConfig{
				Cron: "* * * * *",
			},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
		Version:           "test",
	}

	scheduler := New(manager, queries, serverCfg)
	if scheduler == nil {
		t.Fatal("expected scheduler to be created")
	}

	// Check that cron has 1 entry (only scheduled query)
	entries := scheduler.cron.Entries()
	if len(entries) != 1 {
		t.Errorf("expected 1 cron entry, got %d", len(entries))
	}
}

// TestScheduler_StartStop confirms scheduler starts and stops gracefully
func TestScheduler_StartStop(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queries := []config.QueryConfig{
		{
			Name:     "test",
			Database: "test",
			SQL:      "SELECT 1",
			Schedule: &config.ScheduleConfig{
				Cron: "* * * * *",
			},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
		Version:           "test",
	}

	scheduler := New(manager, queries, serverCfg)
	scheduler.Start()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	scheduler.Stop()
}

// TestScheduler_RunQuery tests direct query execution returns correct count
func TestScheduler_RunQuery(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	query := config.QueryConfig{
		Name:     "count_users",
		Database: "test",
		SQL:      "SELECT COUNT(*) as cnt FROM users",
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
		},
	}

	results, err := scheduler.runQuery(query)
	if err != nil {
		t.Fatalf("runQuery failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 row, got %d", len(results))
	}

	cnt, ok := results[0]["cnt"].(int64)
	if !ok {
		t.Fatalf("expected int64 count, got %T", results[0]["cnt"])
	}

	if cnt != 3 {
		t.Errorf("expected count=3, got %d", cnt)
	}
}

// TestScheduler_RunQueryWithParams tests scheduled query with bound parameter values
func TestScheduler_RunQueryWithParams(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	query := config.QueryConfig{
		Name:     "get_user",
		Database: "test",
		SQL:      "SELECT name FROM users WHERE name = @name",
		Parameters: []config.ParamConfig{
			{Name: "name", Type: "string"},
		},
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
			Params: map[string]string{
				"name": "Alice",
			},
		},
	}

	results, err := scheduler.runQuery(query)
	if err != nil {
		t.Fatalf("runQuery failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 row, got %d", len(results))
	}

	if results[0]["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", results[0]["name"])
	}
}

// TestScheduler_RunQueryError verifies error handling for queries against non-existent tables
func TestScheduler_RunQueryError(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	query := config.QueryConfig{
		Name:     "bad_query",
		Database: "test",
		SQL:      "SELECT * FROM nonexistent_table",
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
		},
	}

	_, err := scheduler.runQuery(query)
	if err == nil {
		t.Error("expected error for bad query")
	}
}

// TestScheduler_BuildParams tests parameter resolution using defaults and schedule overrides
func TestScheduler_BuildParams(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{}
	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	query := config.QueryConfig{
		Name:     "test",
		Database: "test",
		SQL:      "SELECT @name, @status, @count",
		Parameters: []config.ParamConfig{
			{Name: "name", Type: "string", Default: "default_name"},
			{Name: "status", Type: "string"},
			{Name: "count", Type: "int", Default: "10"},
		},
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
			Params: map[string]string{
				"status": "active",
			},
		},
	}

	params := scheduler.buildParams(query)

	// Check that default is used when no schedule param
	if params["name"] != "default_name" {
		t.Errorf("expected default_name, got %v", params["name"])
	}

	// Check that schedule param overrides
	if params["status"] != "active" {
		t.Errorf("expected active, got %v", params["status"])
	}

	// Check int conversion
	if params["count"] != 10 {
		t.Errorf("expected 10, got %v (%T)", params["count"], params["count"])
	}
}

// TestScheduler_ResolveValue_DynamicDates tests dynamic date keywords: now, today, yesterday, tomorrow
func TestScheduler_ResolveValue_DynamicDates(t *testing.T) {
	scheduler := &Scheduler{}
	params := []config.ParamConfig{{Name: "date", Type: "datetime"}}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	tests := []struct {
		input    string
		validate func(any) bool
	}{
		{
			input: "now",
			validate: func(v any) bool {
				t, ok := v.(time.Time)
				return ok && time.Since(t) < time.Second
			},
		},
		{
			input: "today",
			validate: func(v any) bool {
				t, ok := v.(time.Time)
				return ok && t.Equal(today)
			},
		},
		{
			input: "yesterday",
			validate: func(v any) bool {
				t, ok := v.(time.Time)
				return ok && t.Equal(today.AddDate(0, 0, -1))
			},
		},
		{
			input: "tomorrow",
			validate: func(v any) bool {
				t, ok := v.(time.Time)
				return ok && t.Equal(today.AddDate(0, 0, 1))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := scheduler.resolveValue(tt.input, "date", params)
			if !tt.validate(result) {
				t.Errorf("validation failed for %s: got %v (%T)", tt.input, result, result)
			}
		})
	}
}

// TestScheduler_ResolveValue_Types tests type conversion for string, int, and bool parameters
func TestScheduler_ResolveValue_Types(t *testing.T) {
	scheduler := &Scheduler{}

	tests := []struct {
		name      string
		input     string
		paramType string
		want      any
	}{
		{
			name:      "string",
			input:     "hello",
			paramType: "string",
			want:      "hello",
		},
		{
			name:      "int",
			input:     "42",
			paramType: "int",
			want:      42,
		},
		{
			name:      "integer",
			input:     "100",
			paramType: "integer",
			want:      100,
		},
		{
			name:      "bool true",
			input:     "true",
			paramType: "bool",
			want:      true,
		},
		{
			name:      "bool 1",
			input:     "1",
			paramType: "boolean",
			want:      true,
		},
		{
			name:      "bool false",
			input:     "false",
			paramType: "bool",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := []config.ParamConfig{{Name: "test", Type: tt.paramType}}
			result := scheduler.resolveValue(tt.input, "test", params)
			if result != tt.want {
				t.Errorf("expected %v (%T), got %v (%T)", tt.want, tt.want, result, result)
			}
		})
	}
}

// TestScheduler_ResolveValue_DateFormats tests datetime parsing with various input formats
func TestScheduler_ResolveValue_DateFormats(t *testing.T) {
	scheduler := &Scheduler{}
	params := []config.ParamConfig{{Name: "date", Type: "datetime"}}

	tests := []struct {
		input string
		want  time.Time
	}{
		{"2024-01-15", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)},
		{"2024-01-15T10:30:00", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
		{"2024-01-15 10:30:00", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := scheduler.resolveValue(tt.input, "date", params)
			gotTime, ok := result.(time.Time)
			if !ok {
				t.Fatalf("expected time.Time, got %T", result)
			}
			if !gotTime.Equal(tt.want) {
				t.Errorf("expected %v, got %v", tt.want, gotTime)
			}
		})
	}
}

// TestHasScheduledQueries tests detection of scheduled queries in config list
func TestHasScheduledQueries(t *testing.T) {
	tests := []struct {
		name    string
		queries []config.QueryConfig
		want    bool
	}{
		{
			name:    "empty",
			queries: []config.QueryConfig{},
			want:    false,
		},
		{
			name: "no schedules",
			queries: []config.QueryConfig{
				{Name: "q1", SQL: "SELECT 1"},
				{Name: "q2", SQL: "SELECT 2"},
			},
			want: false,
		},
		{
			name: "one schedule",
			queries: []config.QueryConfig{
				{Name: "q1", SQL: "SELECT 1"},
				{Name: "q2", SQL: "SELECT 2", Schedule: &config.ScheduleConfig{Cron: "* * * * *"}},
			},
			want: true,
		},
		{
			name: "all scheduled",
			queries: []config.QueryConfig{
				{Name: "q1", SQL: "SELECT 1", Schedule: &config.ScheduleConfig{Cron: "0 * * * *"}},
				{Name: "q2", SQL: "SELECT 2", Schedule: &config.ScheduleConfig{Cron: "0 8 * * *"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasScheduledQueries(tt.queries)
			if result != tt.want {
				t.Errorf("expected %v, got %v", tt.want, result)
			}
		})
	}
}

// TestScheduler_InvalidCron verifies invalid cron expressions are rejected without panic
func TestScheduler_InvalidCron(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queries := []config.QueryConfig{
		{
			Name:     "bad_cron",
			Database: "test",
			SQL:      "SELECT 1",
			Schedule: &config.ScheduleConfig{
				Cron: "invalid cron expression",
			},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
		Version:           "test",
	}

	// Should not panic, just log error
	scheduler := New(manager, queries, serverCfg)

	// The bad cron should not be added
	entries := scheduler.cron.Entries()
	if len(entries) != 0 {
		t.Errorf("expected 0 cron entries for invalid cron, got %d", len(entries))
	}
}

// TestScheduler_UnknownDatabase tests error for queries referencing non-existent database
func TestScheduler_UnknownDatabase(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	query := config.QueryConfig{
		Name:     "test",
		Database: "nonexistent",
		SQL:      "SELECT 1",
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
		},
	}

	_, err := scheduler.runQuery(query)
	if err == nil {
		t.Error("expected error for unknown database")
	}
}

// TestScheduler_CustomTimeout tests query-specific timeout configuration is applied
func TestScheduler_CustomTimeout(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	// Query with custom timeout
	query := config.QueryConfig{
		Name:       "test",
		Database:   "test",
		SQL:        "SELECT 1",
		TimeoutSec: 120,
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
		},
	}

	// This just verifies it runs without error with custom timeout
	_, err := scheduler.runQuery(query)
	if err != nil {
		t.Errorf("runQuery failed: %v", err)
	}
}

// TestScheduler_ExecuteJob tests job execution wrapper runs query and logs results
func TestScheduler_ExecuteJob(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	// Test successful execution
	query := config.QueryConfig{
		Name:     "test",
		Database: "test",
		SQL:      "SELECT COUNT(*) as cnt FROM users",
		Schedule: &config.ScheduleConfig{
			Cron:       "* * * * *",
			LogResults: true,
		},
	}

	// Should not panic
	scheduler.executeJob(query)
}

// TestScheduler_ExecuteJob_WithFailure tests job execution handles query failures without panic
func TestScheduler_ExecuteJob_WithFailure(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	scheduler := &Scheduler{
		dbManager: manager,
		serverCfg: serverCfg,
	}

	// Test failing query (will retry)
	query := config.QueryConfig{
		Name:     "bad_query",
		Database: "test",
		SQL:      "SELECT * FROM nonexistent",
		Schedule: &config.ScheduleConfig{
			Cron: "* * * * *",
		},
	}

	// Should not panic, just log errors
	scheduler.executeJob(query)
}
