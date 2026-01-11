package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sql-proxy/internal/config"
	"sql-proxy/internal/validate"
)

// TestLoad_ValidConfig verifies a complete valid YAML config loads with all fields correctly populated
func TestLoad_ValidConfig(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test_query"
    database: "primary"
    path: "/api/test"
    method: "GET"
    sql: "SELECT 1"
`
	cfg := loadFromString(t, content)

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if len(cfg.Databases) != 1 {
		t.Errorf("expected 1 database, got %d", len(cfg.Databases))
	}
	if len(cfg.Queries) != 1 {
		t.Errorf("expected 1 query, got %d", len(cfg.Queries))
	}
}

// TestLoad_EnvironmentVariables verifies ${VAR} placeholders in config are expanded from environment
func TestLoad_EnvironmentVariables(t *testing.T) {
	os.Setenv("TEST_DB_HOST", "testhost.example.com")
	os.Setenv("TEST_DB_PORT", "1433")
	defer os.Unsetenv("TEST_DB_HOST")
	defer os.Unsetenv("TEST_DB_PORT")

	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT '${TEST_DB_HOST}' as host"
`
	cfg := loadFromString(t, content)

	// The SQL should have the env var expanded
	expectedSQL := "SELECT 'testhost.example.com' as host"
	gotSQL := strings.TrimSpace(cfg.Queries[0].SQL)
	if gotSQL != expectedSQL {
		t.Errorf("expected env var to be expanded, got %q", cfg.Queries[0].SQL)
	}
}

// TestLoad_MissingServerHost ensures config loading fails when server.host is omitted
func TestLoad_MissingServerHost(t *testing.T) {
	content := `
server:
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "server.host is required")
}

// TestLoad_InvalidPort validates server.port must be in range 1-65535
func TestLoad_InvalidPort(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr string
	}{
		{"zero", 0, "server.port is required"},
		{"negative", -1, "server.port must be 1-65535"},
		{"too_high", 70000, "server.port must be 1-65535"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := configWithPort(tt.port)
			expectLoadError(t, content, tt.wantErr)
		})
	}
}

// TestLoad_InvalidTimeout checks timeout validation: positive values, max >= default
func TestLoad_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name       string
		defaultSec int
		maxSec     int
		wantErr    string
	}{
		{"zero_default", 0, 300, "server.default_timeout_sec is required"},
		{"zero_max", 30, 0, "server.max_timeout_sec is required"},
		{"max_less_than_default", 60, 30, "must be >= server.default_timeout_sec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := configWithTimeouts(tt.defaultSec, tt.maxSec)
			expectLoadError(t, content, tt.wantErr)
		})
	}
}

// TestLoad_NoDatabases ensures at least one database connection is required
func TestLoad_NoDatabases(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases: []

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "at least one database connection is required")
}

// TestLoad_DuplicateDatabaseNames ensures database names must be unique across connections
func TestLoad_DuplicateDatabaseNames(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "duplicate database name")
}

// TestLoad_InvalidDatabaseType rejects unsupported database types like mysql
func TestLoad_InvalidDatabaseType(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "mysql"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "invalid type 'mysql'")
}

// TestLoad_SQLiteMissingPath ensures SQLite databases require a path field
func TestLoad_SQLiteMissingPath(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "path is required for sqlite")
}

// TestLoad_SQLServerMissingFields validates SQL Server requires host, port, user, password, database
func TestLoad_SQLServerMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		wantErr string
	}{
		{"missing_host", "host", "host is required for sqlserver"},
		{"missing_port", "port", "port is required for sqlserver"},
		{"missing_user", "user", "user is required for sqlserver"},
		{"missing_password", "password", "password is required for sqlserver"},
		{"missing_database", "database", "database is required for sqlserver"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := sqlServerConfigMissing(tt.field)
			expectLoadError(t, content, tt.wantErr)
		})
	}
}

// TestLoad_InvalidLogLevel rejects log levels other than debug/info/warn/error
func TestLoad_InvalidLogLevel(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "verbose"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "logging.level must be debug, info, warn, or error")
}

// TestLoad_QueryMissingName ensures every query must have a name field
func TestLoad_QueryMissingName(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "name is required")
}

// TestLoad_QueryUnknownDatabase rejects queries referencing non-existent database connections
func TestLoad_QueryUnknownDatabase(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "nonexistent"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "unknown database 'nonexistent'")
}

// TestLoad_QueryInvalidMethod ensures query method must be GET or POST only
func TestLoad_QueryInvalidMethod(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "DELETE"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "method must be GET or POST")
}

// TestLoad_QueryNegativeTimeout rejects negative timeout_sec values on queries
func TestLoad_QueryNegativeTimeout(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    timeout_sec: -1
    sql: "SELECT 1"
`
	expectLoadError(t, content, "timeout_sec cannot be negative")
}

// TestLoad_InvalidIsolationLevel rejects invalid SQL Server isolation level names
func TestLoad_InvalidIsolationLevel(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlserver"
    host: "localhost"
    port: 1433
    user: "test"
    password: "test"
    database: "testdb"
    isolation: "invalid_level"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
	expectLoadError(t, content, "invalid isolation level")
}

// TestLoad_ScheduleOnlyQuery verifies queries can have schedule without HTTP path
func TestLoad_ScheduleOnlyQuery(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "scheduled_only"
    database: "primary"
    sql: "SELECT COUNT(*) FROM users"
    schedule:
      cron: "0 * * * *"
`
	cfg := loadFromString(t, content)

	if cfg.Queries[0].Path != "" {
		t.Errorf("expected empty path for schedule-only query")
	}
	if cfg.Queries[0].Schedule == nil {
		t.Error("expected schedule to be set")
	}
}

// TestDatabaseConfig_IsReadOnly verifies readonly defaults to true when nil
func TestDatabaseConfig_IsReadOnly(t *testing.T) {
	tests := []struct {
		name     string
		readonly *bool
		want     bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DatabaseConfig{ReadOnly: tt.readonly}
			if got := cfg.IsReadOnly(); got != tt.want {
				t.Errorf("IsReadOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDatabaseConfig_DefaultSessionConfig checks implicit defaults based on readonly flag
func TestDatabaseConfig_DefaultSessionConfig(t *testing.T) {
	tests := []struct {
		name      string
		readonly  *bool
		wantIso   string
		wantLock  int
		wantDead  string
	}{
		{
			"readonly defaults",
			boolPtr(true),
			"read_uncommitted",
			5000,
			"low",
		},
		{
			"readwrite defaults",
			boolPtr(false),
			"read_committed",
			5000,
			"low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DatabaseConfig{ReadOnly: tt.readonly}
			sess := cfg.DefaultSessionConfig()

			if sess.Isolation != tt.wantIso {
				t.Errorf("Isolation = %s, want %s", sess.Isolation, tt.wantIso)
			}
			if sess.LockTimeoutMs != tt.wantLock {
				t.Errorf("LockTimeoutMs = %d, want %d", sess.LockTimeoutMs, tt.wantLock)
			}
			if sess.DeadlockPriority != tt.wantDead {
				t.Errorf("DeadlockPriority = %s, want %s", sess.DeadlockPriority, tt.wantDead)
			}
		})
	}
}

// TestResolveSessionConfig validates priority: query overrides > db overrides > defaults
func TestResolveSessionConfig(t *testing.T) {
	dbCfg := config.DatabaseConfig{
		ReadOnly:         boolPtr(true),
		Isolation:        "read_committed",    // Override implicit default
		LockTimeoutMs:    intPtr(10000),       // Override implicit default
		DeadlockPriority: "",                  // Use implicit default
	}

	queryCfg := config.QueryConfig{
		Isolation:        "repeatable_read",   // Override database setting
		LockTimeoutMs:    nil,                 // Use database setting
		DeadlockPriority: "high",              // Override implicit default
	}

	sess := config.ResolveSessionConfig(dbCfg, queryCfg)

	if sess.Isolation != "repeatable_read" {
		t.Errorf("expected isolation repeatable_read, got %s", sess.Isolation)
	}
	if sess.LockTimeoutMs != 10000 {
		t.Errorf("expected lock timeout 10000, got %d", sess.LockTimeoutMs)
	}
	if sess.DeadlockPriority != "high" {
		t.Errorf("expected deadlock priority high, got %s", sess.DeadlockPriority)
	}
}

// TestValidIsolationLevels checks the ValidIsolationLevels map contains correct entries
func TestValidIsolationLevels(t *testing.T) {
	valid := []string{"read_uncommitted", "read_committed", "repeatable_read", "serializable", "snapshot"}
	invalid := []string{"", "invalid", "READ_COMMITTED", "ReadCommitted"}

	for _, level := range valid {
		if !config.ValidIsolationLevels[level] {
			t.Errorf("expected %s to be valid", level)
		}
	}
	for _, level := range invalid {
		if config.ValidIsolationLevels[level] {
			t.Errorf("expected %s to be invalid", level)
		}
	}
}

// TestValidDeadlockPriorities checks the ValidDeadlockPriorities map for low/normal/high
func TestValidDeadlockPriorities(t *testing.T) {
	valid := []string{"low", "normal", "high"}
	invalid := []string{"", "LOW", "medium", "critical"}

	for _, p := range valid {
		if !config.ValidDeadlockPriorities[p] {
			t.Errorf("expected %s to be valid", p)
		}
	}
	for _, p := range invalid {
		if config.ValidDeadlockPriorities[p] {
			t.Errorf("expected %s to be invalid", p)
		}
	}
}

// TestValidJournalModes checks ValidJournalModes for SQLite: wal/delete/truncate/memory/off
func TestValidJournalModes(t *testing.T) {
	valid := []string{"wal", "delete", "truncate", "memory", "off"}
	invalid := []string{"", "WAL", "persist", "none"}

	for _, mode := range valid {
		if !config.ValidJournalModes[mode] {
			t.Errorf("expected %s to be valid", mode)
		}
	}
	for _, mode := range invalid {
		if config.ValidJournalModes[mode] {
			t.Errorf("expected %s to be invalid", mode)
		}
	}
}

// TestValidDatabaseTypes checks ValidDatabaseTypes contains sqlserver and sqlite only
func TestValidDatabaseTypes(t *testing.T) {
	valid := []string{"sqlserver", "sqlite"}
	invalid := []string{"", "mysql", "postgres", "SQLite", "SQLSERVER"}

	for _, typ := range valid {
		if !config.ValidDatabaseTypes[typ] {
			t.Errorf("expected %s to be valid", typ)
		}
	}
	for _, typ := range invalid {
		if config.ValidDatabaseTypes[typ] {
			t.Errorf("expected %s to be invalid", typ)
		}
	}
}

// Helper functions

func loadFromString(t *testing.T, content string) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	return cfg
}

func expectLoadError(t *testing.T, content, wantErr string) {
	t.Helper()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		// YAML parse error
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(wantErr)) {
			t.Errorf("expected error containing %q, got %q", wantErr, err.Error())
		}
		return
	}

	// Validate the config
	result := validate.Run(cfg)
	if result.Valid {
		t.Fatal("expected validation error, got valid config")
	}

	// Check that the expected error is in the validation errors (case-insensitive)
	allErrors := strings.ToLower(strings.Join(result.Errors, " | "))
	if !strings.Contains(allErrors, strings.ToLower(wantErr)) {
		t.Errorf("expected error containing %q, got errors: %v", wantErr, result.Errors)
	}
}

func configWithPort(port int) string {
	return `
server:
  host: "127.0.0.1"
  port: ` + itoa(port) + `
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
}

func configWithTimeouts(defaultSec, maxSec int) string {
	return `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: ` + itoa(defaultSec) + `
  max_timeout_sec: ` + itoa(maxSec) + `

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
}

func sqlServerConfigMissing(field string) string {
	host := "localhost"
	port := "1433"
	user := "user"
	password := "pass"
	database := "testdb"

	switch field {
	case "host":
		host = ""
	case "port":
		port = "0"
	case "user":
		user = ""
	case "password":
		password = ""
	case "database":
		database = ""
	}

	return `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlserver"
    host: "` + host + `"
    port: ` + port + `
    user: "` + user + `"
    password: "` + password + `"
    database: "` + database + `"

logging:
  level: "info"
  file_path: ""
  max_size_mb: 100
  max_backups: 5
  max_age_days: 30

metrics:
  enabled: true

queries:
  - name: "test"
    database: "primary"
    path: "/test"
    method: "GET"
    sql: "SELECT 1"
`
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

func itoa(i int) string {
	if i < 0 {
		return "-" + itoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// TestIsArrayType verifies IsArrayType correctly identifies array types
func TestIsArrayType(t *testing.T) {
	tests := []struct {
		typeName string
		expected bool
	}{
		{"int[]", true},
		{"string[]", true},
		{"float[]", true},
		{"bool[]", true},
		{"int", false},
		{"string", false},
		{"json", false},
		{"[]", false}, // Too short to be a valid array type
		{"a[]", true},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			result := config.IsArrayType(tt.typeName)
			if result != tt.expected {
				t.Errorf("IsArrayType(%q) = %v, want %v", tt.typeName, result, tt.expected)
			}
		})
	}
}

// TestArrayBaseType verifies ArrayBaseType extracts the base type from array types
func TestArrayBaseType(t *testing.T) {
	tests := []struct {
		typeName string
		expected string
	}{
		{"int[]", "int"},
		{"string[]", "string"},
		{"float[]", "float"},
		{"bool[]", "bool"},
		{"int", "int"},       // Non-array returns as-is
		{"string", "string"}, // Non-array returns as-is
		{"json", "json"},     // Non-array returns as-is
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			result := config.ArrayBaseType(tt.typeName)
			if result != tt.expected {
				t.Errorf("ArrayBaseType(%q) = %q, want %q", tt.typeName, result, tt.expected)
			}
		})
	}
}

// TestValidParameterTypes verifies all expected parameter types are in ValidParameterTypes
func TestValidParameterTypes(t *testing.T) {
	expectedTypes := []string{
		"string", "int", "integer", "float", "double",
		"bool", "boolean", "datetime", "date",
		"json", "int[]", "string[]", "float[]", "bool[]",
	}

	for _, typ := range expectedTypes {
		if !config.ValidParameterTypes[typ] {
			t.Errorf("ValidParameterTypes missing expected type: %s", typ)
		}
	}

	// Verify invalid types are not in the map
	invalidTypes := []string{"object", "array", "map", "list", "unknown"}
	for _, typ := range invalidTypes {
		if config.ValidParameterTypes[typ] {
			t.Errorf("ValidParameterTypes should not contain: %s", typ)
		}
	}
}
