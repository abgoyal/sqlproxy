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

workflows:
  - name: "test_workflow"
    triggers:
      - type: "http"
        path: "/api/test"
        method: "GET"
    steps:
      - name: "fetch"
        type: "query"
        database: "primary"
        sql: "SELECT 1"
      - type: "response"
        template: '{"success": true}'
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
	if len(cfg.Workflows) != 1 {
		t.Errorf("expected 1 workflow, got %d", len(cfg.Workflows))
	}
}

// TestLoad_EnvironmentVariables verifies ${VAR} in variables.values is expanded from environment.
// Note: ${VAR} syntax is ONLY valid in variables.values section.
// Use {{.vars.X}} elsewhere to access imported variables.
func TestLoad_EnvironmentVariables(t *testing.T) {
	os.Setenv("TEST_DB_HOST", "testhost.example.com")
	os.Setenv("TEST_DB_PORT", "1433")
	defer os.Unsetenv("TEST_DB_HOST")
	defer os.Unsetenv("TEST_DB_PORT")

	content := `
# Variables section - ONLY place where ${VAR} syntax works
variables:
  values:
    db_host: "${TEST_DB_HOST}"
    db_port: "${TEST_DB_PORT}"

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

workflows:
  - name: "test"
    triggers:
      - type: "http"
        path: "/test"
        method: "GET"
    steps:
      - name: "fetch"
        type: "query"
        database: "primary"
        sql: "SELECT 1 as test"
      - type: "response"
        template: '{"success": true}'
`
	cfg := loadFromString(t, content)

	// Variables should be expanded from environment
	if cfg.Variables.Values["db_host"] != "testhost.example.com" {
		t.Errorf("expected db_host to be expanded, got %q", cfg.Variables.Values["db_host"])
	}
	if cfg.Variables.Values["db_port"] != "1433" {
		t.Errorf("expected db_port to be expanded, got %q", cfg.Variables.Values["db_port"])
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
`
	expectLoadError(t, content, "logging.level must be debug, info, warn, or error")
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
`
	expectLoadError(t, content, "invalid isolation level")
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
		name     string
		readonly *bool
		wantIso  string
		wantLock int
		wantDead string
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

// TestLoad_VariablesSection verifies the variables section with values
func TestLoad_VariablesSection(t *testing.T) {
	os.Setenv("TEST_API_KEY", "secret-key-123")
	defer os.Unsetenv("TEST_API_KEY")

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

variables:
  values:
    api_key: "${TEST_API_KEY}"
    app_name: "myapp"
    max_retries: "${UNDEFINED_VAR:3}"
`
	cfg := loadFromString(t, content)

	if cfg.Variables.Values["api_key"] != "secret-key-123" {
		t.Errorf("expected api_key to be expanded, got %q", cfg.Variables.Values["api_key"])
	}
	if cfg.Variables.Values["app_name"] != "myapp" {
		t.Errorf("expected app_name to be 'myapp', got %q", cfg.Variables.Values["app_name"])
	}
	if cfg.Variables.Values["max_retries"] != "3" {
		t.Errorf("expected max_retries to have default value '3', got %q", cfg.Variables.Values["max_retries"])
	}
}

// TestLoad_VariablesDefaultValues verifies ${VAR:default} syntax works correctly
func TestLoad_VariablesDefaultValues(t *testing.T) {
	// Ensure the variable is not set
	os.Unsetenv("UNSET_VAR_FOR_TEST")

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

variables:
  values:
    with_default: "${UNSET_VAR_FOR_TEST:fallback_value}"
    empty_default: "${UNSET_VAR_FOR_TEST:}"
`
	cfg := loadFromString(t, content)

	if cfg.Variables.Values["with_default"] != "fallback_value" {
		t.Errorf("expected with_default to be 'fallback_value', got %q", cfg.Variables.Values["with_default"])
	}
	if cfg.Variables.Values["empty_default"] != "" {
		t.Errorf("expected empty_default to be empty string, got %q", cfg.Variables.Values["empty_default"])
	}
}

// TestLoad_VariablesEnvFileSupport verifies loading variables from env file
func TestLoad_VariablesEnvFileSupport(t *testing.T) {
	tmpDir := t.TempDir()

	// Create env file
	envContent := `# This is a comment
DB_HOST=file-host.example.com
DB_PORT=5432
QUOTED_VAR="quoted value"
SINGLE_QUOTED='single quoted'
`
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}

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

variables:
  env_file: ".env"
  values:
    db_host: "${DB_HOST}"
    db_port: "${DB_PORT}"
    quoted: "${QUOTED_VAR}"
    single_quoted: "${SINGLE_QUOTED}"
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Variables.Values["db_host"] != "file-host.example.com" {
		t.Errorf("expected db_host from env file, got %q", cfg.Variables.Values["db_host"])
	}
	if cfg.Variables.Values["db_port"] != "5432" {
		t.Errorf("expected db_port from env file, got %q", cfg.Variables.Values["db_port"])
	}
	if cfg.Variables.Values["quoted"] != "quoted value" {
		t.Errorf("expected quoted from env file without quotes, got %q", cfg.Variables.Values["quoted"])
	}
	if cfg.Variables.Values["single_quoted"] != "single quoted" {
		t.Errorf("expected single_quoted from env file without quotes, got %q", cfg.Variables.Values["single_quoted"])
	}
}

// TestLoad_VariablesEnvOverridesFile verifies actual env vars override env file values
func TestLoad_VariablesEnvOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create env file with a value
	envContent := `OVERRIDE_TEST=from-file`
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}

	// Set actual environment variable to override
	os.Setenv("OVERRIDE_TEST", "from-env")
	defer os.Unsetenv("OVERRIDE_TEST")

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

variables:
  env_file: ".env"
  values:
    test_value: "${OVERRIDE_TEST}"
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Actual env var should override file value
	if cfg.Variables.Values["test_value"] != "from-env" {
		t.Errorf("expected env var to override file, got %q", cfg.Variables.Values["test_value"])
	}
}

// TestLoad_UndefinedVariable verifies that referencing an undefined variable in templates causes an error
func TestLoad_UndefinedVariable(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: 8080
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: "{{.vars.undefined_db_path}}"

logging:
  level: "info"

variables:
  values:
    defined_var: "some_value"
`
	// The undefined variable should cause an error during config loading
	expectLoadError(t, content, "undefined_db_path")
}

// TestLoad_UndefinedVariableInNumericField verifies undefined variable error in pre-rendered numeric fields
func TestLoad_UndefinedVariableInNumericField(t *testing.T) {
	content := `
server:
  host: "127.0.0.1"
  port: "{{.vars.undefined_port}}"
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "primary"
    type: "sqlite"
    path: ":memory:"

logging:
  level: "info"
`
	// The undefined variable in port (numeric field) should leave template unexpanded
	// which will cause YAML parsing to fail (string instead of int)
	expectLoadError(t, content, "")
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
`
}

func boolPtr(b bool) *bool {
	return &b
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

// TestRateLimitConfig_IsPoolReference verifies IsPoolReference returns true only when Pool is set
func TestRateLimitConfig_IsPoolReference(t *testing.T) {
	tests := []struct {
		name     string
		config   config.RateLimitConfig
		expected bool
	}{
		{
			name:     "empty config",
			config:   config.RateLimitConfig{},
			expected: false,
		},
		{
			name:     "pool set",
			config:   config.RateLimitConfig{Pool: "global"},
			expected: true,
		},
		{
			name:     "pool empty string",
			config:   config.RateLimitConfig{Pool: ""},
			expected: false,
		},
		{
			name:     "inline only - no pool",
			config:   config.RateLimitConfig{RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
			expected: false,
		},
		{
			name:     "pool with inline values (invalid config but tests method)",
			config:   config.RateLimitConfig{Pool: "test", RequestsPerSecond: 10, Burst: 20},
			expected: true, // Pool is set, so IsPoolReference returns true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsPoolReference()
			if result != tt.expected {
				t.Errorf("IsPoolReference() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestRateLimitConfig_IsInline verifies IsInline returns true only when both RequestsPerSecond and Burst are positive
func TestRateLimitConfig_IsInline(t *testing.T) {
	tests := []struct {
		name     string
		config   config.RateLimitConfig
		expected bool
	}{
		{
			name:     "empty config",
			config:   config.RateLimitConfig{},
			expected: false,
		},
		{
			name:     "only requests_per_second",
			config:   config.RateLimitConfig{RequestsPerSecond: 10},
			expected: false, // Burst is 0, so not valid inline
		},
		{
			name:     "only burst",
			config:   config.RateLimitConfig{Burst: 20},
			expected: false, // RequestsPerSecond is 0, so not valid inline
		},
		{
			name:     "both positive",
			config:   config.RateLimitConfig{RequestsPerSecond: 10, Burst: 20},
			expected: true,
		},
		{
			name:     "both positive with key",
			config:   config.RateLimitConfig{RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
			expected: true, // Key is optional
		},
		{
			name:     "requests_per_second zero",
			config:   config.RateLimitConfig{RequestsPerSecond: 0, Burst: 20},
			expected: false,
		},
		{
			name:     "burst zero",
			config:   config.RateLimitConfig{RequestsPerSecond: 10, Burst: 0},
			expected: false,
		},
		{
			name:     "negative requests_per_second",
			config:   config.RateLimitConfig{RequestsPerSecond: -1, Burst: 20},
			expected: false,
		},
		{
			name:     "negative burst",
			config:   config.RateLimitConfig{RequestsPerSecond: 10, Burst: -1},
			expected: false,
		},
		{
			name:     "pool with inline values (tests method behavior)",
			config:   config.RateLimitConfig{Pool: "test", RequestsPerSecond: 10, Burst: 20},
			expected: true, // IsInline checks only inline fields, ignores Pool
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsInline()
			if result != tt.expected {
				t.Errorf("IsInline() = %v, want %v", result, tt.expected)
			}
		})
	}
}
