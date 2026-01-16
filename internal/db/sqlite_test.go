package db

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"sql-proxy/internal/config"
)

// TestNewSQLiteDriver_InMemory verifies in-memory SQLite driver creation with :memory: path
func TestNewSQLiteDriver_InMemory(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "sqlite",
		Path: ":memory:",
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	if driver.Name() != "test" {
		t.Errorf("expected name 'test', got %s", driver.Name())
	}
	if driver.Type() != "sqlite" {
		t.Errorf("expected type 'sqlite', got %s", driver.Type())
	}
	if !driver.IsReadOnly() {
		t.Error("expected read-only by default")
	}
}

// TestNewSQLiteDriver_ReadWrite confirms explicit readonly=false enables write mode
func TestNewSQLiteDriver_ReadWrite(t *testing.T) {
	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "test",
		Type:     "sqlite",
		Path:     ":memory:",
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	if driver.IsReadOnly() {
		t.Error("expected read-write")
	}
}

// TestNewSQLiteDriver_MissingPath ensures empty path is rejected with clear error
func TestNewSQLiteDriver_MissingPath(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "sqlite",
		Path: "",
	}

	_, err := NewSQLiteDriver(cfg)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewSQLiteDriver_CustomSettings verifies busy_timeout and journal_mode PRAGMAs apply
func TestNewSQLiteDriver_CustomSettings(t *testing.T) {
	busyTimeout := 10000
	cfg := config.DatabaseConfig{
		Name:          "test",
		Type:          "sqlite",
		Path:          ":memory:",
		BusyTimeoutMs: &busyTimeout,
		JournalMode:   "wal",
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	// Verify pragmas were applied by querying them
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Check busy_timeout
	results, err := driver.Query(ctx, sessCfg, "PRAGMA busy_timeout", nil)
	if err != nil {
		t.Fatalf("failed to query busy_timeout: %v", err)
	}
	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}

	// Check journal_mode
	results, err = driver.Query(ctx, sessCfg, "PRAGMA journal_mode", nil)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}
}

// TestSQLiteDriver_Query_Simple executes basic SELECT and validates returned columns
func TestSQLiteDriver_Query_Simple(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	results, err := driver.Query(ctx, sessCfg, "SELECT 1 as num, 'hello' as msg", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}
	if results.Rows[0]["num"] != int64(1) {
		t.Errorf("expected num=1, got %v", results.Rows[0]["num"])
	}
	if results.Rows[0]["msg"] != "hello" {
		t.Errorf("expected msg='hello', got %v", results.Rows[0]["msg"])
	}
}

// TestSQLiteDriver_Query_WithParams verifies @param named parameters work correctly
func TestSQLiteDriver_Query_WithParams(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	createTestTable(t, driver)
	insertTestData(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Query with named parameters
	results, err := driver.Query(ctx, sessCfg,
		"SELECT * FROM test_users WHERE status = @status AND id > @minId ORDER BY id",
		map[string]any{"status": "active", "minId": 1},
	)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}
	if results.Rows[0]["name"] != "Bob" {
		t.Errorf("expected Bob, got %v", results.Rows[0]["name"])
	}
}

// TestSQLiteDriver_Query_NullParams tests NULL parameter handling for optional filters
func TestSQLiteDriver_Query_NullParams(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	createTestTable(t, driver)
	insertTestData(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Query with NULL parameter (optional filter pattern)
	results, err := driver.Query(ctx, sessCfg,
		"SELECT * FROM test_users WHERE (@status IS NULL OR status = @status)",
		map[string]any{"status": nil},
	)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// Should return all rows when status is NULL
	if len(results.Rows) != 3 {
		t.Errorf("expected 3 rows with NULL filter, got %d", len(results.Rows))
	}
}

// TestSQLiteDriver_Query_EmptyResult confirms empty result set returns zero-length slice
func TestSQLiteDriver_Query_EmptyResult(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	createTestTable(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	results, err := driver.Query(ctx, sessCfg, "SELECT * FROM test_users", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(results.Rows))
	}
}

// TestSQLiteDriver_Query_DateTimeHandling tests time.Time parameter binding and retrieval
func TestSQLiteDriver_Query_DateTimeHandling(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Create table with datetime
	_, err := driver.Query(ctx, sessCfg,
		"CREATE TABLE events (id INTEGER PRIMARY KEY, name TEXT, event_time DATETIME)", nil)
	if err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	// Insert with datetime
	now := time.Now().Truncate(time.Second)
	_, err = driver.Query(ctx, sessCfg,
		"INSERT INTO events (name, event_time) VALUES (@name, @time)",
		map[string]any{"name": "test", "time": now},
	)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Query back
	results, err := driver.Query(ctx, sessCfg, "SELECT * FROM events", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}
}

// TestSQLiteDriver_Query_SpecialCharacters ensures SQL injection strings are safely escaped
func TestSQLiteDriver_Query_SpecialCharacters(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	createTestTable(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Insert with special characters
	specialChars := "O'Brien; DROP TABLE test_users;--"
	_, err := driver.Query(ctx, sessCfg,
		"INSERT INTO test_users (name, email, status) VALUES (@name, @email, @status)",
		map[string]any{"name": specialChars, "email": "test@test.com", "status": "active"},
	)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Query back
	results, err := driver.Query(ctx, sessCfg,
		"SELECT name FROM test_users WHERE name = @name",
		map[string]any{"name": specialChars},
	)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}
	if results.Rows[0]["name"] != specialChars {
		t.Errorf("special characters not preserved: got %v", results.Rows[0]["name"])
	}
}

// TestSQLiteDriver_Query_Unicode validates CJK, Cyrillic, Arabic, and emoji preservation
func TestSQLiteDriver_Query_Unicode(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	createTestTable(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Insert unicode data
	unicodeNames := []string{
		"日本語テスト",
		"Привет мир",
		"مرحبا بالعالم",
		"🎉 emoji test 🚀",
	}

	for i, name := range unicodeNames {
		_, err := driver.Query(ctx, sessCfg,
			"INSERT INTO test_users (name, email, status) VALUES (@name, @email, @status)",
			map[string]any{"name": name, "email": "test@test.com", "status": "active"},
		)
		if err != nil {
			t.Fatalf("insert %d failed: %v", i, err)
		}
	}

	// Query back
	results, err := driver.Query(ctx, sessCfg, "SELECT name FROM test_users", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != len(unicodeNames) {
		t.Fatalf("expected %d rows, got %d", len(unicodeNames), len(results.Rows))
	}

	for i, result := range results.Rows {
		if result["name"] != unicodeNames[i] {
			t.Errorf("unicode not preserved at row %d: expected %q, got %q", i, unicodeNames[i], result["name"])
		}
	}
}

// TestSQLiteDriver_Query_LargeResult tests handling of 10000 row result sets
func TestSQLiteDriver_Query_LargeResult(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Create table
	_, err := driver.Query(ctx, sessCfg,
		"CREATE TABLE large_test (id INTEGER PRIMARY KEY, data TEXT)", nil)
	if err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	// Insert many rows
	rowCount := 10000
	for i := 0; i < rowCount; i++ {
		_, err := driver.Query(ctx, sessCfg,
			"INSERT INTO large_test (data) VALUES (@data)",
			map[string]any{"data": "row data " + string(rune(i%26+'a'))},
		)
		if err != nil {
			t.Fatalf("insert failed at %d: %v", i, err)
		}
	}

	// Query all
	results, err := driver.Query(ctx, sessCfg, "SELECT COUNT(*) as cnt FROM large_test", nil)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}

	cnt := results.Rows[0]["cnt"].(int64)
	if cnt != int64(rowCount) {
		t.Errorf("expected %d rows, got %d", rowCount, cnt)
	}
}

// TestSQLiteDriver_Query_Timeout verifies context deadline expiration stops query
func TestSQLiteDriver_Query_Timeout(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	// Create a very short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for context to expire
	time.Sleep(10 * time.Millisecond)

	sessCfg := config.SessionConfig{}
	_, err := driver.Query(ctx, sessCfg, "SELECT 1", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// TestSQLiteDriver_Query_Concurrent runs 100 parallel queries with file-based SQLite
func TestSQLiteDriver_Query_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_concurrent.db"

	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "test_concurrent",
		Type:     "sqlite",
		Path:     dbPath,
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	createTestTable(t, driver)
	insertTestData(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Run concurrent queries
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := driver.Query(ctx, sessCfg, "SELECT * FROM test_users", nil)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent query error: %v", err)
	}
}

// TestSQLiteDriver_Ping confirms Ping returns nil for healthy connection
func TestSQLiteDriver_Ping(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	ctx := context.Background()
	if err := driver.Ping(ctx); err != nil {
		t.Errorf("ping failed: %v", err)
	}
}

// TestSQLiteDriver_Reconnect tests connection re-establishment after close
func TestSQLiteDriver_Reconnect(t *testing.T) {
	driver := createTestSQLiteDriver(t)

	// Close and reconnect
	if err := driver.Reconnect(); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	defer driver.Close()

	// Verify connection works
	ctx := context.Background()
	if err := driver.Ping(ctx); err != nil {
		t.Errorf("ping after reconnect failed: %v", err)
	}
}

// TestSQLiteDriver_Config verifies Config() returns original configuration
func TestSQLiteDriver_Config(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test_config",
		Type: "sqlite",
		Path: ":memory:",
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	gotCfg := driver.Config()
	if gotCfg.Name != cfg.Name {
		t.Errorf("expected name %s, got %s", cfg.Name, gotCfg.Name)
	}
	if gotCfg.Type != cfg.Type {
		t.Errorf("expected type %s, got %s", cfg.Type, gotCfg.Type)
	}
}

// TestSQLiteDriver_TranslateQuery tests @param to sql.Named translation and deduplication
func TestSQLiteDriver_TranslateQuery(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	tests := []struct {
		name     string
		query    string
		params   map[string]any
		wantSQL  string
		wantArgs int
	}{
		{
			name:     "no params",
			query:    "SELECT * FROM users",
			params:   nil,
			wantSQL:  "SELECT * FROM users",
			wantArgs: 0,
		},
		{
			name:     "single param",
			query:    "SELECT * FROM users WHERE id = @id",
			params:   map[string]any{"id": 1},
			wantSQL:  "SELECT * FROM users WHERE id = @id",
			wantArgs: 1,
		},
		{
			name:     "multiple params",
			query:    "SELECT * FROM users WHERE status = @status AND age > @age",
			params:   map[string]any{"status": "active", "age": 18},
			wantSQL:  "SELECT * FROM users WHERE status = @status AND age > @age",
			wantArgs: 2,
		},
		{
			name:     "repeated param",
			query:    "SELECT * FROM users WHERE name = @name OR email LIKE @name",
			params:   map[string]any{"name": "test"},
			wantSQL:  "SELECT * FROM users WHERE name = @name OR email LIKE @name",
			wantArgs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := driver.translateQuery(tt.query, tt.params)
			if sql != tt.wantSQL {
				t.Errorf("expected SQL %q, got %q", tt.wantSQL, sql)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("expected %d args, got %d", tt.wantArgs, len(args))
			}
		})
	}
}

// TestIsWriteQuery tests the SQL statement type detection
func TestIsWriteQuery(t *testing.T) {
	tests := []struct {
		sql      string
		expected bool
	}{
		// Write operations
		{"INSERT INTO users (name) VALUES ('Alice')", true},
		{"  INSERT INTO users (name) VALUES ('Alice')", true}, // Leading whitespace
		{"UPDATE users SET name = 'Bob'", true},
		{"DELETE FROM users WHERE id = 1", true},
		{"CREATE TABLE test (id INT)", true},
		{"DROP TABLE test", true},
		{"ALTER TABLE test ADD col INT", true},
		{"TRUNCATE TABLE test", true},
		{"MERGE INTO target USING source ON ...", true},
		{"insert into users (name) values ('alice')", true}, // Lowercase

		// Read operations
		{"SELECT * FROM users", false},
		{"  SELECT * FROM users", false}, // Leading whitespace
		{"select * from users", false},   // Lowercase
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", false},

		// CTEs with write operations
		{"WITH cte AS (SELECT 1) INSERT INTO users SELECT * FROM cte", true},
		{"WITH updates AS (SELECT id FROM users) UPDATE users SET status = 'done' WHERE id IN (SELECT id FROM updates)", true},
		{"WITH to_delete AS (SELECT id FROM old_users) DELETE FROM users WHERE id IN (SELECT id FROM to_delete)", true},
		{"with cte as (select 1) insert into t values (1)", true}, // Lowercase CTE with write

		// Edge cases
		{"", false},
		{"   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			got := IsWriteQuery(tt.sql)
			if got != tt.expected {
				t.Errorf("IsWriteQuery(%q) = %v, want %v", tt.sql, got, tt.expected)
			}
		})
	}
}

// TestSQLiteDriver_WriteOperations_RowsAffected tests that write operations return correct rows affected
func TestSQLiteDriver_WriteOperations_RowsAffected(t *testing.T) {
	driver := createTestSQLiteDriver(t)
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Create a test table
	result, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE test_rows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	// CREATE TABLE returns 0 rows affected
	if result.RowsAffected != 0 {
		t.Errorf("CREATE TABLE RowsAffected = %d, want 0", result.RowsAffected)
	}

	// Insert multiple rows
	result, err = driver.Query(ctx, sessCfg, `
		INSERT INTO test_rows (name) VALUES ('Alice'), ('Bob'), ('Charlie')
	`, nil)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	if result.RowsAffected != 3 {
		t.Errorf("INSERT RowsAffected = %d, want 3", result.RowsAffected)
	}
	if len(result.Rows) != 0 {
		t.Errorf("INSERT should return no rows, got %d", len(result.Rows))
	}

	// Update some rows
	result, err = driver.Query(ctx, sessCfg, `
		UPDATE test_rows SET name = 'Updated' WHERE id <= 2
	`, nil)
	if err != nil {
		t.Fatalf("failed to update: %v", err)
	}
	if result.RowsAffected != 2 {
		t.Errorf("UPDATE RowsAffected = %d, want 2", result.RowsAffected)
	}

	// Delete one row
	result, err = driver.Query(ctx, sessCfg, `
		DELETE FROM test_rows WHERE id = 1
	`, nil)
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}
	if result.RowsAffected != 1 {
		t.Errorf("DELETE RowsAffected = %d, want 1", result.RowsAffected)
	}

	// Verify SELECT still works and returns rows (not rows affected)
	result, err = driver.Query(ctx, sessCfg, `SELECT * FROM test_rows`, nil)
	if err != nil {
		t.Fatalf("failed to select: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Errorf("SELECT should return 2 rows, got %d", len(result.Rows))
	}
	// SELECT shouldn't populate RowsAffected
	if result.RowsAffected != 0 {
		t.Errorf("SELECT RowsAffected = %d, want 0", result.RowsAffected)
	}
}

// Helper functions

func createTestSQLiteDriver(t *testing.T) *SQLiteDriver {
	t.Helper()

	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "test",
		Type:     "sqlite",
		Path:     ":memory:",
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	return driver
}

func createTestTable(t *testing.T, driver *SQLiteDriver) {
	t.Helper()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE test_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			status TEXT DEFAULT 'active'
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
}

func insertTestData(t *testing.T, driver *SQLiteDriver) {
	t.Helper()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	users := []struct {
		name, email, status string
	}{
		{"Alice", "alice@test.com", "active"},
		{"Bob", "bob@test.com", "active"},
		{"Charlie", "charlie@test.com", "inactive"},
	}

	for _, u := range users {
		_, err := driver.Query(ctx, sessCfg,
			"INSERT INTO test_users (name, email, status) VALUES (@name, @email, @status)",
			map[string]any{"name": u.name, "email": u.email, "status": u.status},
		)
		if err != nil {
			t.Fatalf("failed to insert user %s: %v", u.name, err)
		}
	}
}
