package validate

import (
	"strings"
	"testing"

	"sql-proxy/internal/config"
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
		port       int
		defTimeout int
		maxTimeout int
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid",
			port:       8080,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    false,
		},
		{
			name:       "port 0",
			port:       0,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "port must be 1-65535",
		},
		{
			name:       "port too high",
			port:       70000,
			defTimeout: 30,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "port must be 1-65535",
		},
		{
			name:       "zero default timeout",
			port:       8080,
			defTimeout: 0,
			maxTimeout: 300,
			wantErr:    true,
			errMsg:     "at least 1 second",
		},
		{
			name:       "max less than default",
			port:       8080,
			defTimeout: 60,
			maxTimeout: 30,
			wantErr:    true,
			errMsg:     "must be >= default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Server: config.ServerConfig{
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

// TestValidateLogging tests log level validation accepts only debug/info/warn/error
func TestValidateLogging(t *testing.T) {
	tests := []struct {
		level   string
		wantErr bool
	}{
		{"debug", false},
		{"info", false},
		{"warn", false},
		{"error", false},
		{"INFO", false}, // Should be case-insensitive
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			cfg := &config.Config{
				Logging: config.LoggingConfig{Level: tt.level},
			}

			r := &Result{Valid: true}
			validateLogging(cfg, r)

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

// TestValidateQueries_NoQueries tests empty queries list generates warning
func TestValidateQueries_NoQueries(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	// Should have warning about no queries
	if len(r.Warnings) == 0 {
		t.Error("expected warning about no queries")
	}
}

// TestValidateQueries_DuplicateName ensures duplicate query names are rejected
func TestValidateQueries_DuplicateName(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/a", Method: "GET", SQL: "SELECT 1"},
			{Name: "q1", Database: "test", Path: "/api/b", Method: "GET", SQL: "SELECT 2"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for duplicate query names")
	}
}

// TestValidateQueries_DuplicatePath ensures duplicate endpoint paths are rejected
func TestValidateQueries_DuplicatePath(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
			{Name: "q2", Database: "test", Path: "/api/test", Method: "GET", SQL: "SELECT 2"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for duplicate paths")
	}
}

// TestValidateQueries_InvalidPath ensures path must start with leading /
func TestValidateQueries_InvalidPath(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "api/test", Method: "GET", SQL: "SELECT 1"}, // Missing leading /
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for path without leading /")
	}
}

// TestValidateQueries_InvalidMethod ensures only GET/POST methods are allowed
func TestValidateQueries_InvalidMethod(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "DELETE", SQL: "SELECT 1"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for invalid method")
	}
}

// TestValidateQueries_UnknownDatabase ensures query must reference existing database
func TestValidateQueries_UnknownDatabase(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "nonexistent", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for unknown database")
	}
}

// TestValidateQueries_WriteOnReadOnly ensures write SQL rejected on read-only database
func TestValidateQueries_WriteOnReadOnly(t *testing.T) {
	readOnly := true
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "POST", SQL: "INSERT INTO users VALUES (1)"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	if r.Valid {
		t.Error("expected validation to fail for write on read-only")
	}

	found := false
	for _, err := range r.Errors {
		if strings.Contains(err, "INSERT") && strings.Contains(err, "read-only") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error about INSERT on read-only, got: %v", r.Errors)
	}
}

// TestValidateQueries_WriteOnReadWrite confirms write SQL allowed on write-enabled database
func TestValidateQueries_WriteOnReadWrite(t *testing.T) {
	readOnly := false
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "POST", SQL: "INSERT INTO users VALUES (1)"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	// Should pass - write on write-enabled is OK
	if !r.Valid {
		t.Errorf("expected validation to pass for write on read-write, got errors: %v", r.Errors)
	}
}

// TestValidateQueries_UnusedDatabase tests unused database generates warning
func TestValidateQueries_UnusedDatabase(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "used", Type: "sqlite", Path: ":memory:"},
			{Name: "unused", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "used", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
		},
	}

	r := &Result{Valid: true}
	validateQueries(cfg, r)

	// Should have warning about unused database
	found := false
	for _, warn := range r.Warnings {
		if strings.Contains(warn, "unused") && strings.Contains(warn, "not used") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about unused database, got: %v", r.Warnings)
	}
}

// TestValidateParams tests SQL/parameter cross-validation for mismatches and reserved names
func TestValidateParams(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		params      []config.ParamConfig
		wantErr     bool
		wantWarning bool
	}{
		{
			name:   "matching params",
			sql:    "SELECT * FROM users WHERE id = @id",
			params: []config.ParamConfig{{Name: "id", Type: "int"}},
		},
		{
			name:        "sql param not defined",
			sql:         "SELECT * FROM users WHERE id = @id",
			params:      []config.ParamConfig{},
			wantWarning: true,
		},
		{
			name:        "defined param not used",
			sql:         "SELECT * FROM users",
			params:      []config.ParamConfig{{Name: "id", Type: "int"}},
			wantWarning: true,
		},
		{
			name:    "reserved param name",
			sql:     "SELECT * FROM users",
			params:  []config.ParamConfig{{Name: "_timeout", Type: "int"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := config.QueryConfig{
				Name:       "test",
				SQL:        tt.sql,
				Parameters: tt.params,
			}

			r := &Result{Valid: true}
			validateParams(q, "queries[0]", r)

			if tt.wantErr && r.Valid {
				t.Error("expected validation to fail")
			}
			if !tt.wantErr && !r.Valid {
				t.Errorf("unexpected errors: %v", r.Errors)
			}
			if tt.wantWarning && len(r.Warnings) == 0 {
				t.Error("expected warning")
			}
		})
	}
}

// TestValidateSchedule tests cron expression and required parameter validation
func TestValidateSchedule(t *testing.T) {
	tests := []struct {
		name     string
		query    config.QueryConfig
		wantErr  bool
		errCount int
	}{
		{
			name: "valid cron",
			query: config.QueryConfig{
				Name:     "test",
				SQL:      "SELECT 1",
				Schedule: &config.ScheduleConfig{Cron: "0 * * * *"},
			},
		},
		{
			name: "invalid cron",
			query: config.QueryConfig{
				Name:     "test",
				SQL:      "SELECT 1",
				Schedule: &config.ScheduleConfig{Cron: "invalid"},
			},
			wantErr: true,
		},
		{
			name: "empty cron",
			query: config.QueryConfig{
				Name:     "test",
				SQL:      "SELECT 1",
				Schedule: &config.ScheduleConfig{Cron: ""},
			},
			wantErr: true,
		},
		{
			name: "required param missing in schedule",
			query: config.QueryConfig{
				Name: "test",
				SQL:  "SELECT * FROM users WHERE id = @id",
				Parameters: []config.ParamConfig{
					{Name: "id", Type: "int", Required: true},
				},
				Schedule: &config.ScheduleConfig{
					Cron:   "0 * * * *",
					Params: map[string]string{},
				},
			},
			wantErr: true,
		},
		{
			name: "required param with default ok",
			query: config.QueryConfig{
				Name: "test",
				SQL:  "SELECT * FROM users WHERE id = @id",
				Parameters: []config.ParamConfig{
					{Name: "id", Type: "int", Required: true, Default: "1"},
				},
				Schedule: &config.ScheduleConfig{
					Cron:   "0 * * * *",
					Params: map[string]string{},
				},
			},
			wantErr: false,
		},
		{
			name: "required param provided in schedule",
			query: config.QueryConfig{
				Name: "test",
				SQL:  "SELECT * FROM users WHERE id = @id",
				Parameters: []config.ParamConfig{
					{Name: "id", Type: "int", Required: true},
				},
				Schedule: &config.ScheduleConfig{
					Cron: "0 * * * *",
					Params: map[string]string{
						"id": "42",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{Valid: true}
			validateSchedule(tt.query, "queries[0]", r)

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
		Logging: config.LoggingConfig{Level: "info"},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
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
		Logging: config.LoggingConfig{Level: "info"},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
		},
	}

	result := Run(cfg)

	if result.Valid {
		t.Error("expected invalid config")
	}
}

// TestRun_DBConnectionTest verifies SQLite :memory: connection succeeds
func TestRun_DBConnectionTest(t *testing.T) {
	// Valid config with SQLite should pass connection test
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
		Logging: config.LoggingConfig{Level: "info"},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
		},
	}

	result := Run(cfg)

	if !result.Valid {
		t.Errorf("expected valid config with successful DB connection, got errors: %v", result.Errors)
	}
}

// TestRun_DBConnectionFail verifies invalid SQLite path fails connection test
func TestRun_DBConnectionFail(t *testing.T) {
	// Invalid SQLite path should fail connection test
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
		Logging: config.LoggingConfig{Level: "info"},
		Queries: []config.QueryConfig{
			{Name: "q1", Database: "test", Path: "/api/test", Method: "GET", SQL: "SELECT 1"},
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
