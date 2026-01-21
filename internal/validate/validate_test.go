package validate

import (
	"strings"
	"testing"

	"sql-proxy/internal/config"
	"sql-proxy/internal/workflow"
)

// TestResult_AddError verifies error accumulation marks result as invalid
func TestResult_AddError(t *testing.T) {
	r := &Result{Valid: true}
	r.addError("test error: %s", "details")

	if r.Valid {
		t.Error("expected Valid=false after addError")
	}
	if len(r.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(r.Errors))
	}
	if r.Errors[0] != "test error: details" {
		t.Errorf("unexpected error message: %s", r.Errors[0])
	}
}

// TestResult_AddWarning confirms warnings don't affect valid flag
func TestResult_AddWarning(t *testing.T) {
	r := &Result{Valid: true}
	r.addWarning("test warning: %s", "info")

	if !r.Valid {
		t.Error("warnings should not affect Valid flag")
	}
	if len(r.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(r.Warnings))
	}
	if r.Warnings[0] != "test warning: info" {
		t.Errorf("unexpected warning message: %s", r.Warnings[0])
	}
}

// TestValidateServer tests server port and timeout validation rules
func TestValidateServer(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		port       int
		defTimeout int
		maxTimeout int
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid",
			host:       "localhost",
			port:       8080,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    false,
		},
		{
			name:       "missing host",
			host:       "",
			port:       8080,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "server.host is required",
		},
		{
			name:       "port 0",
			host:       "localhost",
			port:       0,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "server.port is required",
		},
		{
			name:       "port too high",
			host:       "localhost",
			port:       70000,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "port must be 1-65535",
		},
		{
			name:       "zero default timeout",
			host:       "localhost",
			port:       8080,
			defTimeout: 0,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "default_timeout_sec is required",
		},
		{
			name:       "max less than default",
			host:       "localhost",
			port:       8080,
			defTimeout: 60,
			maxTimeout: 30,
			wantErr:    true,
			errMsg:     "must be >= server.default_timeout_sec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Server: config.ServerConfig{
					Host:              tt.host,
					Port:              tt.port,
					DefaultTimeoutSec: tt.defTimeout,
					MaxTimeoutSec:     tt.maxTimeout,
				},
			}

			r := &Result{Valid: true}
			validateServer(cfg, r)

			if tt.wantErr {
				if r.Valid {
					t.Error("expected validation to fail")
				}
				found := false
				for _, err := range r.Errors {
					if strings.Contains(strings.ToLower(err), strings.ToLower(tt.errMsg)) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, r.Errors)
				}
			} else {
				if !r.Valid {
					t.Errorf("expected validation to pass, got errors: %v", r.Errors)
				}
			}
		})
	}
}

// TestValidateDatabase_Empty ensures empty database list is rejected
func TestValidateDatabase_Empty(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{},
	}

	r := &Result{Valid: true}
	validateDatabase(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for empty databases")
	}
}

// TestValidateDatabase_Duplicate ensures duplicate database names are rejected
func TestValidateDatabase_Duplicate(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "db1", Type: "sqlite", Path: ":memory:"},
			{Name: "db1", Type: "sqlite", Path: ":memory:"},
		},
	}

	r := &Result{Valid: true}
	validateDatabase(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for duplicate names")
	}

	found := false
	for _, err := range r.Errors {
		if strings.Contains(err, "duplicate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error about duplicate, got: %v", r.Errors)
	}
}

// TestValidateDatabase_InvalidType ensures unsupported database types are rejected
func TestValidateDatabase_InvalidType(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "db1", Type: "mysql"},
		},
	}

	r := &Result{Valid: true}
	validateDatabase(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for invalid type")
	}
}

// TestValidateDatabase_SQLite tests SQLite-specific validation: path, journal mode, timeout
func TestValidateDatabase_SQLite(t *testing.T) {
	tests := []struct {
		name    string
		dbCfg   config.DatabaseConfig
		wantErr bool
	}{
		{
			name:    "valid",
			dbCfg:   config.DatabaseConfig{Name: "test", Type: "sqlite", Path: ":memory:"},
			wantErr: false,
		},
		{
			name:    "missing path",
			dbCfg:   config.DatabaseConfig{Name: "test", Type: "sqlite"},
			wantErr: true,
		},
		{
			name:    "valid journal mode",
			dbCfg:   config.DatabaseConfig{Name: "test", Type: "sqlite", Path: ":memory:", JournalMode: "wal"},
			wantErr: false,
		},
		{
			name:    "invalid journal mode",
			dbCfg:   config.DatabaseConfig{Name: "test", Type: "sqlite", Path: ":memory:", JournalMode: "invalid"},
			wantErr: true,
		},
		{
			name:    "negative busy timeout",
			dbCfg:   config.DatabaseConfig{Name: "test", Type: "sqlite", Path: ":memory:", BusyTimeoutMs: intPtr(-1)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Databases: []config.DatabaseConfig{tt.dbCfg},
			}

			r := &Result{Valid: true}
			validateDatabase(cfg, r)

			if tt.wantErr {
				if r.Valid {
					t.Error("expected validation to fail")
				}
			} else {
				if !r.Valid {
					t.Errorf("expected validation to pass, got errors: %v", r.Errors)
				}
			}
		})
	}
}

// TestValidateDatabase_SQLServer tests SQL Server validation: host, port, isolation, timeout
func TestValidateDatabase_SQLServer(t *testing.T) {
	tests := []struct {
		name    string
		dbCfg   config.DatabaseConfig
		wantErr bool
	}{
		{
			name: "valid",
			dbCfg: config.DatabaseConfig{
				Name: "test", Type: "sqlserver",
				Host: "localhost", Port: 1433, User: "sa", Password: "pass", Database: "testdb",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			dbCfg: config.DatabaseConfig{
				Name: "test", Type: "sqlserver",
				Port: 1433, User: "sa", Password: "pass", Database: "testdb",
			},
			wantErr: true,
		},
		{
			name: "invalid isolation",
			dbCfg: config.DatabaseConfig{
				Name: "test", Type: "sqlserver",
				Host: "localhost", Port: 1433, User: "sa", Password: "pass", Database: "testdb",
				Isolation: "invalid",
			},
			wantErr: true,
		},
		{
			name: "valid isolation",
			dbCfg: config.DatabaseConfig{
				Name: "test", Type: "sqlserver",
				Host: "localhost", Port: 1433, User: "sa", Password: "pass", Database: "testdb",
				Isolation: "read_committed",
			},
			wantErr: false,
		},
		{
			name: "negative lock timeout",
			dbCfg: config.DatabaseConfig{
				Name: "test", Type: "sqlserver",
				Host: "localhost", Port: 1433, User: "sa", Password: "pass", Database: "testdb",
				LockTimeoutMs: intPtr(-1),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Databases: []config.DatabaseConfig{tt.dbCfg},
			}

			r := &Result{Valid: true}
			validateDatabase(cfg, r)

			if tt.wantErr {
				if r.Valid {
					t.Error("expected validation to fail")
				}
			} else {
				if !r.Valid {
					t.Errorf("expected validation to pass, got errors: %v", r.Errors)
				}
			}
		})
	}
}

// TestValidateDatabase_EnvVarWarning tests unresolved env vars generate warnings
func TestValidateDatabase_EnvVarWarning(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{
				Name: "test", Type: "sqlserver",
				Host: "${DB_HOST}", Port: 1433, User: "sa", Password: "${DB_PASS}", Database: "testdb",
			},
		},
	}

	r := &Result{Valid: true}
	validateDatabase(cfg, r)

	// Should have warnings about unresolved env vars
	if len(r.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(r.Warnings), r.Warnings)
	}
}

// TestValidateLogging tests log level and rotation settings validation
func TestValidateLogging(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		maxSizeMB  int
		maxBackups int
		maxAgeDays int
		wantErr    bool
		errMsg     string
	}{
		{"valid debug", "debug", 100, 5, 30, false, ""},
		{"valid info", "info", 100, 5, 30, false, ""},
		{"valid warn", "warn", 100, 5, 30, false, ""},
		{"valid error", "error", 100, 5, 30, false, ""},
		{"case insensitive", "INFO", 100, 5, 30, false, ""},
		{"invalid level", "invalid", 100, 5, 30, true, "logging.level must be"},
		{"empty level", "", 100, 5, 30, true, "logging.level is required"},
		{"missing max_size", "info", 0, 5, 30, true, "logging.max_size_mb is required"},
		{"missing max_backups", "info", 100, 0, 30, true, "logging.max_backups is required"},
		{"missing max_age_days", "info", 100, 5, 0, true, "logging.max_age_days is required"},
		{"negative max_size", "info", -1, 5, 30, true, "max_size_mb cannot be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Logging: config.LoggingConfig{
					Level:      tt.level,
					MaxSizeMB:  tt.maxSizeMB,
					MaxBackups: tt.maxBackups,
					MaxAgeDays: tt.maxAgeDays,
				},
			}

			r := &Result{Valid: true}
			validateLogging(cfg, r)

			if tt.wantErr {
				if r.Valid {
					t.Error("expected validation to fail")
				}
				if tt.errMsg != "" {
					found := false
					for _, err := range r.Errors {
						if strings.Contains(err, tt.errMsg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected error containing %q, got: %v", tt.errMsg, r.Errors)
					}
				}
			} else {
				if !r.Valid {
					t.Errorf("expected validation to pass, got errors: %v", r.Errors)
				}
			}
		})
	}
}

// TestValidateDebug tests debug config validation rules
func TestValidateDebug(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		debugPort  int
		debugHost  string
		serverPort int
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "disabled - no validation",
			enabled:    false,
			debugPort:  0,
			debugHost:  "localhost",
			serverPort: 8080,
			wantErr:    false,
		},
		{
			name:       "enabled, separate port, with host",
			enabled:    true,
			debugPort:  6060,
			debugHost:  "localhost",
			serverPort: 8080,
			wantErr:    false,
		},
		{
			name:       "enabled, separate port, no host",
			enabled:    true,
			debugPort:  6060,
			debugHost:  "",
			serverPort: 8080,
			wantErr:    false,
		},
		{
			name:       "enabled, shared port (0), no host",
			enabled:    true,
			debugPort:  0,
			debugHost:  "",
			serverPort: 8080,
			wantErr:    false,
		},
		{
			name:       "enabled, shared port (same), no host",
			enabled:    true,
			debugPort:  8080,
			debugHost:  "",
			serverPort: 8080,
			wantErr:    false,
		},
		{
			name:       "error: host set with port 0",
			enabled:    true,
			debugPort:  0,
			debugHost:  "localhost",
			serverPort: 8080,
			wantErr:    true,
			errMsg:     "debug.host cannot be set when debug endpoints share the main server port",
		},
		{
			name:       "error: host set with same port as server",
			enabled:    true,
			debugPort:  8080,
			debugHost:  "127.0.0.1",
			serverPort: 8080,
			wantErr:    true,
			errMsg:     "debug.host cannot be set when debug endpoints share the main server port",
		},
		{
			name:       "error: invalid port (negative)",
			enabled:    true,
			debugPort:  -1,
			debugHost:  "",
			serverPort: 8080,
			wantErr:    true,
			errMsg:     "debug.port must be 0-65535",
		},
		{
			name:       "error: invalid port (too high)",
			enabled:    true,
			debugPort:  70000,
			debugHost:  "",
			serverPort: 8080,
			wantErr:    true,
			errMsg:     "debug.port must be 0-65535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Server: config.ServerConfig{
					Port: tt.serverPort,
				},
				Debug: config.DebugConfig{
					Enabled: tt.enabled,
					Port:    tt.debugPort,
					Host:    tt.debugHost,
				},
			}

			r := &Result{Valid: true}
			validateDebug(cfg, r)

			if tt.wantErr {
				if r.Valid {
					t.Error("expected validation to fail")
				}
				if tt.errMsg != "" {
					found := false
					for _, err := range r.Errors {
						if strings.Contains(err, tt.errMsg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected error containing %q, got: %v", tt.errMsg, r.Errors)
					}
				}
			} else {
				if !r.Valid {
					t.Errorf("expected validation to pass, got errors: %v", r.Errors)
				}
			}
		})
	}
}

// TestRun_ValidConfig tests complete valid configuration passes all checks
func TestRun_ValidConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Logging: validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test_workflow",
				Triggers: []workflow.TriggerConfig{
					{Type: "http", Path: "/api/test", Method: "GET"},
				},
				Steps: []workflow.StepConfig{
					{Name: "fetch", Type: "query", Database: "test", SQL: "SELECT 1"},
					{Type: "response", Template: `{"success": true}`},
				},
			},
		},
	}

	result := Run(cfg)

	if !result.Valid {
		t.Errorf("expected valid config, got errors: %v", result.Errors)
	}
}

// TestRun_InvalidConfig tests configuration with invalid port fails validation
func TestRun_InvalidConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              0, // Invalid
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Logging: validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test_workflow",
				Triggers: []workflow.TriggerConfig{
					{Type: "http", Path: "/api/test", Method: "GET"},
				},
				Steps: []workflow.StepConfig{
					{Name: "fetch", Type: "query", Database: "test", SQL: "SELECT 1"},
					{Type: "response", Template: `{"success": true}`},
				},
			},
		},
	}

	result := Run(cfg)

	if result.Valid {
		t.Error("expected invalid config")
	}
}

// TestRun_DBConnectionTest verifies SQLite :memory: connection succeeds
func TestRun_DBConnectionTest(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Logging: validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test_workflow",
				Triggers: []workflow.TriggerConfig{
					{Type: "http", Path: "/api/test", Method: "GET"},
				},
				Steps: []workflow.StepConfig{
					{Name: "fetch", Type: "query", Database: "test", SQL: "SELECT 1"},
					{Type: "response", Template: `{"success": true}`},
				},
			},
		},
	}

	result := Run(cfg)

	if !result.Valid {
		t.Errorf("expected valid config with successful DB connection, got errors: %v", result.Errors)
	}
}

// TestRun_DBConnectionFail verifies invalid SQLite path fails connection test
func TestRun_DBConnectionFail(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: "/nonexistent/path/to/db.sqlite"},
		},
		Logging: validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test_workflow",
				Triggers: []workflow.TriggerConfig{
					{Type: "http", Path: "/api/test", Method: "GET"},
				},
				Steps: []workflow.StepConfig{
					{Name: "fetch", Type: "query", Database: "test", SQL: "SELECT 1"},
					{Type: "response", Template: `{"success": true}`},
				},
			},
		},
	}

	result := Run(cfg)

	if result.Valid {
		t.Error("expected invalid config due to DB connection failure")
	}
}

func intPtr(i int) *int {
	return &i
}

// validLoggingConfig returns a valid logging config for tests
func validLoggingConfig() config.LoggingConfig {
	return config.LoggingConfig{
		Level:      "info",
		MaxSizeMB:  100,
		MaxBackups: 5,
		MaxAgeDays: 30,
	}
}

// TestRun_SQLServerUnresolvedEnvVar tests that SQL Server with unresolved env vars is skipped during connection test
func TestRun_SQLServerUnresolvedEnvVar(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{
				Name:     "test",
				Type:     "sqlserver",
				Host:     "${DB_HOST}", // Unresolved env var
				Port:     1433,
				User:     "user",
				Password: "pass",
				Database: "db",
			},
		},
		Logging: validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test_workflow",
				Triggers: []workflow.TriggerConfig{
					{Type: "http", Path: "/api/test", Method: "GET"},
				},
				Steps: []workflow.StepConfig{
					{Name: "fetch", Type: "query", Database: "test", SQL: "SELECT 1"},
					{Type: "response", Template: `{"success": true}`},
				},
			},
		},
	}

	result := Run(cfg)

	// Should pass because connection test is skipped for unresolved env vars
	if !result.Valid {
		t.Errorf("expected config to pass (skipping connection test), got errors: %v", result.Errors)
	}
}

// TestRun_SQLServerUnresolvedPassword tests SQL Server with unresolved password env var is skipped
func TestRun_SQLServerUnresolvedPassword(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{
				Name:     "test",
				Type:     "sqlserver",
				Host:     "localhost",
				Port:     1433,
				User:     "user",
				Password: "${DB_PASSWORD}", // Unresolved env var
				Database: "db",
			},
		},
		Logging: validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test_workflow",
				Triggers: []workflow.TriggerConfig{
					{Type: "http", Path: "/api/test", Method: "GET"},
				},
				Steps: []workflow.StepConfig{
					{Name: "fetch", Type: "query", Database: "test", SQL: "SELECT 1"},
					{Type: "response", Template: `{"success": true}`},
				},
			},
		},
	}

	result := Run(cfg)

	// Should pass because connection test is skipped for unresolved env vars
	if !result.Valid {
		t.Errorf("expected config to pass (skipping connection test), got errors: %v", result.Errors)
	}
}

// TestValidateServerCache tests server-level cache configuration validation
func TestValidateServerCache(t *testing.T) {
	tests := []struct {
		name    string
		cache   *config.CacheConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid cache config",
			cache:   &config.CacheConfig{Enabled: true, MaxSizeMB: 256, DefaultTTLSec: 300},
			wantErr: false,
		},
		{
			name:    "disabled cache skips validation",
			cache:   &config.CacheConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:    "nil cache config",
			cache:   nil,
			wantErr: false,
		},
		{
			name:    "negative max size",
			cache:   &config.CacheConfig{Enabled: true, MaxSizeMB: -1},
			wantErr: true,
			errMsg:  "max_size_mb cannot be negative",
		},
		{
			name:    "negative default TTL",
			cache:   &config.CacheConfig{Enabled: true, DefaultTTLSec: -1},
			wantErr: true,
			errMsg:  "default_ttl_sec cannot be negative",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Server: config.ServerConfig{
					Host:              "localhost",
					Port:              8080,
					DefaultTimeoutSec: 30,
					MaxTimeoutSec:     300,
					Cache:             tc.cache,
				},
			}

			r := &Result{Valid: true}
			validateServer(cfg, r)

			if tc.wantErr && r.Valid {
				t.Error("expected error but got none")
			}
			if !tc.wantErr && !r.Valid {
				t.Errorf("unexpected error: %v", r.Errors)
			}
			if tc.wantErr && !strings.Contains(strings.Join(r.Errors, " "), tc.errMsg) {
				t.Errorf("expected error containing %q, got %v", tc.errMsg, r.Errors)
			}
		})
	}
}

// TestValidateRateLimits tests server-level rate limit pool validation
func TestValidateRateLimits(t *testing.T) {
	tests := []struct {
		name       string
		rateLimits []config.RateLimitPoolConfig
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "empty rate limits is valid",
			rateLimits: []config.RateLimitPoolConfig{},
			wantErr:    false,
		},
		{
			name: "valid single pool",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "global", RequestsPerSecond: 100, Burst: 200, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: false,
		},
		{
			name: "valid multiple pools",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "global", RequestsPerSecond: 100, Burst: 200, Key: "{{.trigger.client_ip}}"},
				{Name: "per_user", RequestsPerSecond: 10, Burst: 20, Key: `{{.trigger.headers.Authorization}}`},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "", RequestsPerSecond: 100, Burst: 200, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "duplicate name",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "pool1", RequestsPerSecond: 100, Burst: 200, Key: "{{.trigger.client_ip}}"},
				{Name: "pool1", RequestsPerSecond: 50, Burst: 100, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "duplicate pool name",
		},
		{
			name: "zero requests per second",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "pool", RequestsPerSecond: 0, Burst: 200, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "requests_per_second must be positive",
		},
		{
			name: "negative requests per second",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "pool", RequestsPerSecond: -10, Burst: 200, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "requests_per_second must be positive",
		},
		{
			name: "zero burst",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "pool", RequestsPerSecond: 100, Burst: 0, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "burst must be positive",
		},
		{
			name: "missing key template",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "pool", RequestsPerSecond: 100, Burst: 200, Key: ""},
			},
			wantErr: true,
			errMsg:  "key template is required",
		},
		{
			name: "invalid key template syntax",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "pool", RequestsPerSecond: 100, Burst: 200, Key: "{{.Invalid"},
			},
			wantErr: true,
			errMsg:  "invalid key template",
		},
		{
			name: "reserved _inline: prefix",
			rateLimits: []config.RateLimitPoolConfig{
				{Name: "_inline:test", RequestsPerSecond: 100, Burst: 200, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "reserved for internal use",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				RateLimits: tc.rateLimits,
			}

			r := &Result{Valid: true}
			validateRateLimits(cfg, r)

			if tc.wantErr && r.Valid {
				t.Error("expected error but got none")
			}
			if !tc.wantErr && !r.Valid {
				t.Errorf("unexpected error: %v", r.Errors)
			}
			if tc.wantErr && !strings.Contains(strings.Join(r.Errors, " "), tc.errMsg) {
				t.Errorf("expected error containing %q, got %v", tc.errMsg, r.Errors)
			}
		})
	}
}

// TestRun_NoWorkflowsWarning tests that empty workflows list generates a warning
func TestRun_NoWorkflowsWarning(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Logging:   validLoggingConfig(),
		Workflows: []workflow.WorkflowConfig{}, // Empty
	}

	result := Run(cfg)

	// Should be valid but have a warning
	if !result.Valid {
		t.Errorf("expected valid config, got errors: %v", result.Errors)
	}

	found := false
	for _, warn := range result.Warnings {
		if strings.Contains(warn, "No workflows configured") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about no workflows, got: %v", result.Warnings)
	}
}

// TestValidatePublicIDs tests public ID configuration validation
func TestValidatePublicIDs(t *testing.T) {
	tests := []struct {
		name      string
		publicIDs *config.PublicIDsConfig
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "nil config is valid",
			publicIDs: nil,
			wantErr:   false,
		},
		{
			name: "valid configuration",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "this-is-a-secret-key-that-is-32chars",
				Namespaces: []config.NamespaceConfig{
					{Name: "user", Prefix: "usr"},
					{Name: "order", Prefix: "ord"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing secret key",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "",
				Namespaces: []config.NamespaceConfig{
					{Name: "user", Prefix: "usr"},
				},
			},
			wantErr: true,
			errMsg:  "secret_key is required",
		},
		{
			name: "secret key too short",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "short",
				Namespaces: []config.NamespaceConfig{
					{Name: "user", Prefix: "usr"},
				},
			},
			wantErr: true,
			errMsg:  "at least 32 characters",
		},
		{
			name: "empty namespaces",
			publicIDs: &config.PublicIDsConfig{
				SecretKey:  "this-is-a-secret-key-that-is-32chars",
				Namespaces: []config.NamespaceConfig{},
			},
			wantErr: true,
			errMsg:  "at least one namespace",
		},
		{
			name: "namespace without name",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "this-is-a-secret-key-that-is-32chars",
				Namespaces: []config.NamespaceConfig{
					{Name: "", Prefix: "usr"},
				},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "duplicate namespace names",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "this-is-a-secret-key-that-is-32chars",
				Namespaces: []config.NamespaceConfig{
					{Name: "user", Prefix: "usr"},
					{Name: "user", Prefix: "usr2"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate namespace name",
		},
		{
			name: "duplicate prefixes",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "this-is-a-secret-key-that-is-32chars",
				Namespaces: []config.NamespaceConfig{
					{Name: "user", Prefix: "same"},
					{Name: "order", Prefix: "same"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate prefix",
		},
		{
			name: "no prefix is valid",
			publicIDs: &config.PublicIDsConfig{
				SecretKey: "this-is-a-secret-key-that-is-32chars",
				Namespaces: []config.NamespaceConfig{
					{Name: "user"},
					{Name: "order"},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				PublicIDs: tc.publicIDs,
			}

			r := &Result{Valid: true}
			validatePublicIDs(cfg, r)

			if tc.wantErr && r.Valid {
				t.Error("expected error but got none")
			}
			if !tc.wantErr && !r.Valid {
				t.Errorf("unexpected error: %v", r.Errors)
			}
			if tc.wantErr && !strings.Contains(strings.Join(r.Errors, " "), tc.errMsg) {
				t.Errorf("expected error containing %q, got %v", tc.errMsg, r.Errors)
			}
		})
	}
}

func TestValidatePublicIDFunctionUsageWithoutConfig(t *testing.T) {
	tests := []struct {
		name    string
		wf      workflow.WorkflowConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "publicID in step params without config",
			wf: workflow.WorkflowConfig{
				Name: "test_workflow",
				Steps: []workflow.StepConfig{
					{
						Name:   "step1",
						Type:   "query",
						Params: map[string]string{"public": `{{publicID "user" .id}}`},
					},
				},
			},
			wantErr: true,
			errMsg:  "publicID",
		},
		{
			name: "privateID in response template without config",
			wf: workflow.WorkflowConfig{
				Name: "test_workflow",
				Steps: []workflow.StepConfig{
					{
						Name:     "respond",
						Type:     "response",
						Template: `{"id": {{privateID "user" .trigger.params.public_id}}}`,
					},
				},
			},
			wantErr: true,
			errMsg:  "privateID",
		},
		{
			name: "isValidPublicID in condition without config",
			wf: workflow.WorkflowConfig{
				Name: "test_workflow",
				Steps: []workflow.StepConfig{
					{
						Name:      "check",
						Type:      "query",
						Condition: `{{isValidPublicID "user" .trigger.params.id}}`,
					},
				},
			},
			wantErr: true,
			errMsg:  "isValidPublicID",
		},
		{
			name: "no public ID functions - ok",
			wf: workflow.WorkflowConfig{
				Name: "test_workflow",
				Steps: []workflow.StepConfig{
					{
						Name:     "respond",
						Type:     "response",
						Template: `{"id": {{.trigger.params.id}}}`,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				PublicIDs: nil,
				Workflows: []workflow.WorkflowConfig{tc.wf},
			}

			r := &Result{Valid: true}
			validatePublicIDs(cfg, r)

			if tc.wantErr && r.Valid {
				t.Error("expected error but got none")
			}
			if !tc.wantErr && !r.Valid {
				t.Errorf("unexpected error: %v", r.Errors)
			}
			if tc.wantErr && !strings.Contains(strings.Join(r.Errors, " "), tc.errMsg) {
				t.Errorf("expected error containing %q, got %v", tc.errMsg, r.Errors)
			}
		})
	}
}
