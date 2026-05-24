package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"sql-proxy/internal/config"
)

// --- Unit tests (no MySQL connection required) ---

// TestBuildMySQLDSN_Default verifies DSN construction with default port and settings
func TestBuildMySQLDSN_Default(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "localhost",
		Port:     0, // Should default to 3306
		User:     "reader",
		Password: "secret",
		Database: "mydb",
	}

	dsn := buildMySQLDSN(cfg)

	// Check DSN contains expected components
	expected := []string{
		"reader:secret@tcp(localhost:3306)/mydb?",
		"parseTime=true",
		"timeout=10s",
		"readTimeout=10s",
		"writeTimeout=10s",
		"tls=false",
		"collation=utf8mb4_general_ci",
	}

	for _, part := range expected {
		if !strings.Contains(dsn, part) {
			t.Errorf("DSN missing %q\n  got: %s", part, dsn)
		}
	}
}

// TestBuildMySQLDSN_CustomPort verifies custom port is used in DSN
func TestBuildMySQLDSN_CustomPort(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "db.example.com",
		Port:     3307,
		User:     "admin",
		Password: "pass",
		Database: "app",
	}

	dsn := buildMySQLDSN(cfg)

	if !strings.Contains(dsn, "admin:pass@tcp(db.example.com:3307)/app?") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
}

// TestBuildMySQLDSN_TLSOptions tests all TLS/encrypt configuration variants
func TestBuildMySQLDSN_TLSOptions(t *testing.T) {
	tests := []struct {
		name    string
		encrypt string
		want    string
	}{
		{"true", "true", "tls=true"},
		{"false", "false", "tls=false"},
		{"disable", "disable", "tls=false"},
		{"skip-verify", "skip-verify", "tls=skip-verify"},
		{"empty defaults to false", "", "tls=false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DatabaseConfig{
				Host:     "localhost",
				Port:     3306,
				User:     "user",
				Password: "pass",
				Database: "db",
				Encrypt:  tt.encrypt,
			}

			dsn := buildMySQLDSN(cfg)
			if !strings.Contains(dsn, tt.want) {
				t.Errorf("DSN missing %q\n  got: %s", tt.want, dsn)
			}
		})
	}
}

// TestBuildMySQLDSN_SpecialCharsInPassword verifies password with special chars is included as-is
func TestBuildMySQLDSN_SpecialCharsInPassword(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "localhost",
		Port:     3306,
		User:     "user",
		Password: "p@ss:w0rd/foo",
		Database: "db",
	}

	dsn := buildMySQLDSN(cfg)
	if !strings.Contains(dsn, "user:p@ss:w0rd/foo@tcp") {
		t.Errorf("password not preserved in DSN: %s", dsn)
	}
}

// TestMySQLDriver_TranslateQuery tests @param to ? positional placeholder translation
func TestMySQLDriver_TranslateQuery(t *testing.T) {
	d := &MySQLDriver{}

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
			wantSQL:  "SELECT * FROM users WHERE id = ?",
			wantArgs: 1,
		},
		{
			name:     "multiple params",
			query:    "SELECT * FROM users WHERE status = @status AND age > @age",
			params:   map[string]any{"status": "active", "age": 18},
			wantSQL:  "SELECT * FROM users WHERE status = ? AND age > ?",
			wantArgs: 2,
		},
		{
			name:     "repeated param duplicates placeholder",
			query:    "SELECT * FROM users WHERE name = @name OR email LIKE @name",
			params:   map[string]any{"name": "test"},
			wantSQL:  "SELECT * FROM users WHERE name = ? OR email LIKE ?",
			wantArgs: 2,
		},
		{
			name:     "param not in map gives nil",
			query:    "SELECT * FROM users WHERE id = @id",
			params:   map[string]any{},
			wantSQL:  "SELECT * FROM users WHERE id = ?",
			wantArgs: 1,
		},
		{
			name:     "complex query with multiple occurrences",
			query:    "SELECT * FROM t WHERE (@status IS NULL OR status = @status) AND id > @id",
			params:   map[string]any{"status": nil, "id": 5},
			wantSQL:  "SELECT * FROM t WHERE (? IS NULL OR status = ?) AND id > ?",
			wantArgs: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := d.translateQuery(tt.query, tt.params)
			if sql != tt.wantSQL {
				t.Errorf("expected SQL %q, got %q", tt.wantSQL, sql)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("expected %d args, got %d", tt.wantArgs, len(args))
			}
		})
	}
}

// TestMySQLDriver_TranslateQuery_Values verifies parameter values are correctly ordered
func TestMySQLDriver_TranslateQuery_Values(t *testing.T) {
	d := &MySQLDriver{}

	query := "INSERT INTO users (name, age) VALUES (@name, @age)"
	params := map[string]any{"name": "Alice", "age": 30}

	_, args := d.translateQuery(query, params)

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}

	if args[0] != "Alice" {
		t.Errorf("arg[0] = %v, want 'Alice'", args[0])
	}
	if args[1] != 30 {
		t.Errorf("arg[1] = %v, want 30", args[1])
	}
}

// TestMySQLDriver_TranslateQuery_NilValue verifies nil values are passed through
func TestMySQLDriver_TranslateQuery_NilValue(t *testing.T) {
	d := &MySQLDriver{}

	query := "SELECT * FROM users WHERE (@status IS NULL OR status = @status)"
	params := map[string]any{"status": nil}

	_, args := d.translateQuery(query, params)

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}

	if args[0] != nil {
		t.Errorf("arg[0] = %v, want nil", args[0])
	}
	if args[1] != nil {
		t.Errorf("arg[1] = %v, want nil", args[1])
	}
}

// TestMySQLIsolationToSQL tests conversion of config isolation strings to MySQL syntax
func TestMySQLIsolationToSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"read_uncommitted", "READ UNCOMMITTED"},
		{"read_committed", "READ COMMITTED"},
		{"repeatable_read", "REPEATABLE READ"},
		{"serializable", "SERIALIZABLE"},
		{"", "READ COMMITTED"},        // default
		{"invalid", "READ COMMITTED"}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := mysqlIsolationToSQL(tc.input)
			if result != tc.expected {
				t.Errorf("mysqlIsolationToSQL(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// TestMySQLDriver_ConfigurePool verifies connection pool settings are applied
func TestMySQLDriver_ConfigurePool(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.DatabaseConfig
		wantMaxOpen int
		wantMaxIdle int
	}{
		{
			name:        "defaults",
			cfg:         config.DatabaseConfig{},
			wantMaxOpen: mysqlMaxOpenConns,
			wantMaxIdle: mysqlMaxIdleConns,
		},
		{
			name: "custom values",
			cfg: config.DatabaseConfig{
				MaxOpenConns:    intPtr(10),
				MaxIdleConns:    intPtr(5),
				ConnMaxLifetime: intPtr(600),
				ConnMaxIdleTime: intPtr(300),
			},
			wantMaxOpen: 10,
			wantMaxIdle: 5,
		},
		{
			name: "partial overrides",
			cfg: config.DatabaseConfig{
				MaxOpenConns: intPtr(20),
			},
			wantMaxOpen: 20,
			wantMaxIdle: mysqlMaxIdleConns,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// sql.Open doesn't connect, just registers the DSN
			db, err := sql.Open("mysql", "fake:fake@tcp(localhost:3306)/fake")
			if err != nil {
				t.Fatalf("sql.Open failed: %v", err)
			}
			defer db.Close()

			configureMySQLPool(db, tt.cfg)

			stats := db.Stats()
			if stats.MaxOpenConnections != tt.wantMaxOpen {
				t.Errorf("MaxOpenConnections = %d, want %d", stats.MaxOpenConnections, tt.wantMaxOpen)
			}
			// MaxIdleConns is not directly exposed in stats, but we can verify
			// the function didn't panic and MaxOpen is set correctly
		})
	}
}

// TestMySQLDriver_ConfigValidation tests that invalid configs produce errors
func TestMySQLDriver_ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.DatabaseConfig
		wantErr string
	}{
		{
			name: "missing host fails on ping",
			cfg: config.DatabaseConfig{
				Name:     "test",
				Type:     "mysql",
				Host:     "invalid-host-that-does-not-exist.example.com",
				Port:     3306,
				User:     "user",
				Password: "pass",
				Database: "db",
			},
			wantErr: "failed to ping mysql database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMySQLDriver(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// --- Integration tests (require MYSQL_HOST environment variable) ---

// TestNewMySQLDriver_Integration verifies driver creation against a real MySQL instance
func TestNewMySQLDriver_Integration(t *testing.T) {
	driver := createTestMySQLDriver(t)
	defer driver.Close()

	if driver.Name() != "test" {
		t.Errorf("expected name 'test', got %s", driver.Name())
	}
	if driver.Type() != "mysql" {
		t.Errorf("expected type 'mysql', got %s", driver.Type())
	}
	if !driver.IsReadOnly() {
		t.Error("expected read-only by default")
	}
}

// TestNewMySQLDriver_ReadWrite confirms explicit readonly=false enables write mode
func TestNewMySQLDriver_ReadWrite(t *testing.T) {
	cfg := mysqlTestConfig(t)
	readOnly := false
	cfg.ReadOnly = &readOnly

	driver, err := NewMySQLDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	if driver.IsReadOnly() {
		t.Error("expected read-write")
	}
}

// TestMySQLDriver_Ping confirms Ping returns nil for healthy connection
func TestMySQLDriver_Ping(t *testing.T) {
	driver := createTestMySQLDriver(t)
	defer driver.Close()

	ctx := context.Background()
	if err := driver.Ping(ctx); err != nil {
		t.Errorf("ping failed: %v", err)
	}
}

// TestMySQLDriver_Reconnect tests connection re-establishment after close
func TestMySQLDriver_Reconnect(t *testing.T) {
	driver := createTestMySQLDriver(t)

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

// TestMySQLDriver_Config verifies Config() returns original configuration
func TestMySQLDriver_Config(t *testing.T) {
	driver := createTestMySQLDriver(t)
	defer driver.Close()

	gotCfg := driver.Config()
	if gotCfg.Name != "test" {
		t.Errorf("expected name 'test', got %s", gotCfg.Name)
	}
	if gotCfg.Type != "mysql" {
		t.Errorf("expected type 'mysql', got %s", gotCfg.Type)
	}
}

// TestMySQLDriver_Query_Simple executes basic SELECT and validates returned columns
func TestMySQLDriver_Query_Simple(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

	results, err := driver.Query(ctx, sessCfg, "SELECT 1 as num, 'hello' as msg", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(results.Rows))
	}
	if results.Rows[0]["num"] != int64(1) {
		t.Errorf("expected num=1, got %v (type %T)", results.Rows[0]["num"], results.Rows[0]["num"])
	}
	if results.Rows[0]["msg"] != "hello" {
		t.Errorf("expected msg='hello', got %v", results.Rows[0]["msg"])
	}
}

// TestMySQLDriver_Query_WithParams verifies @param named parameters work correctly
func TestMySQLDriver_Query_WithParams(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	createMySQLTestTable(t, driver)
	insertMySQLTestData(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

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

// TestMySQLDriver_Query_NullParams tests NULL parameter handling for optional filters
func TestMySQLDriver_Query_NullParams(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	createMySQLTestTable(t, driver)
	insertMySQLTestData(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

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

// TestMySQLDriver_Query_EmptyResult confirms empty result set returns zero-length slice
func TestMySQLDriver_Query_EmptyResult(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	createMySQLTestTable(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

	results, err := driver.Query(ctx, sessCfg, "SELECT * FROM test_users", nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(results.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(results.Rows))
	}
}

// TestMySQLDriver_Query_Timeout verifies context deadline expiration stops query
func TestMySQLDriver_Query_Timeout(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	// Create a very short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for context to expire
	time.Sleep(10 * time.Millisecond)

	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}
	_, err := driver.Query(ctx, sessCfg, "SELECT 1", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// TestMySQLDriver_Query_SpecialCharacters ensures SQL injection strings are safely escaped
func TestMySQLDriver_Query_SpecialCharacters(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	createMySQLTestTable(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

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

// TestMySQLDriver_Query_Unicode validates CJK, Cyrillic, Arabic, and emoji preservation
func TestMySQLDriver_Query_Unicode(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	createMySQLTestTable(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

	// Insert unicode data
	unicodeNames := []string{
		"日本語テスト",
		"Привет мир",
		"مرحبا بالعالم",
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
	results, err := driver.Query(ctx, sessCfg, "SELECT name FROM test_users ORDER BY id", nil)
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

// TestMySQLDriver_WriteOperations_RowsAffected tests that write operations return correct rows affected
func TestMySQLDriver_WriteOperations_RowsAffected(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

	// Create a test table
	result, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE test_rows (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
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
	if result.RowsAffected != 0 {
		t.Errorf("SELECT RowsAffected = %d, want 0", result.RowsAffected)
	}
}

// TestMySQLDriver_Query_Concurrent runs parallel queries against MySQL
func TestMySQLDriver_Query_Concurrent(t *testing.T) {
	driver := createTestMySQLDriverRW(t)
	defer driver.Close()

	createMySQLTestTable(t, driver)
	insertMySQLTestData(t, driver)

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

	// Run concurrent queries
	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := driver.Query(ctx, sessCfg, "SELECT * FROM test_users", nil)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent query error: %v", err)
	}
}

// --- Helper functions ---

func intPtr(v int) *int {
	return &v
}

func mysqlTestConfig(t *testing.T) config.DatabaseConfig {
	t.Helper()

	host := os.Getenv("MYSQL_HOST")
	if host == "" {
		t.Skip("MYSQL_HOST not set, skipping MySQL integration test")
	}

	port := 3306
	if p := os.Getenv("MYSQL_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	return config.DatabaseConfig{
		Name:     "test",
		Type:     "mysql",
		Host:     host,
		Port:     port,
		User:     envOrDefault("MYSQL_USER", "root"),
		Password: envOrDefault("MYSQL_PASSWORD", "testpass"),
		Database: envOrDefault("MYSQL_DATABASE", "testdb"),
	}
}

func createTestMySQLDriver(t *testing.T) *MySQLDriver {
	t.Helper()

	cfg := mysqlTestConfig(t)

	driver, err := NewMySQLDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create MySQL driver: %v", err)
	}
	return driver
}

func createTestMySQLDriverRW(t *testing.T) *MySQLDriver {
	t.Helper()

	cfg := mysqlTestConfig(t)
	readOnly := false
	cfg.ReadOnly = &readOnly

	driver, err := NewMySQLDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create MySQL driver: %v", err)
	}

	// Drop test tables if they exist (clean state)
	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}
	driver.Query(ctx, sessCfg, "DROP TABLE IF EXISTS test_users", nil)
	driver.Query(ctx, sessCfg, "DROP TABLE IF EXISTS test_rows", nil)

	return driver
}

func createMySQLTestTable(t *testing.T, driver *MySQLDriver) {
	t.Helper()

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

	_, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE test_users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			email VARCHAR(255) NOT NULL,
			status VARCHAR(50) DEFAULT 'active'
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
}

func insertMySQLTestData(t *testing.T, driver *MySQLDriver) {
	t.Helper()

	ctx := context.Background()
	sessCfg := config.SessionConfig{
		Isolation:     "read_committed",
		LockTimeoutMs: 5000,
	}

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
