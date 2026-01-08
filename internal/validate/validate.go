package validate

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
)

// Result holds validation results
type Result struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

func (r *Result) AddError(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
	r.Valid = false
}

func (r *Result) AddWarning(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Config validates the configuration file without starting the service
func Config(cfg *config.Config) *Result {
	result := &Result{Valid: true}

	// Validate server config
	validateServer(cfg, result)

	// Validate database config
	validateDatabase(cfg, result)

	// Validate logging config
	validateLogging(cfg, result)

	// Validate metrics config
	validateMetrics(cfg, result)

	// Validate queries
	validateQueries(cfg, result)

	return result
}

// ConfigWithDB validates config and tests database connectivity
func ConfigWithDB(cfg *config.Config) *Result {
	result := Config(cfg)
	if !result.Valid {
		return result
	}

	// Test database connection
	database, err := db.New(cfg.Database)
	if err != nil {
		result.AddError("Database connection failed: %v", err)
		return result
	}
	defer database.Close()

	// Test a simple query
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Ping(ctx); err != nil {
		result.AddError("Database ping failed: %v", err)
		return result
	}

	return result
}

func validateServer(cfg *config.Config, result *Result) {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		result.AddError("Server port must be between 1 and 65535, got: %d", cfg.Server.Port)
	}

	if cfg.Server.DefaultTimeoutSec < 1 {
		result.AddError("Default timeout must be at least 1 second")
	}

	if cfg.Server.MaxTimeoutSec < cfg.Server.DefaultTimeoutSec {
		result.AddError("Max timeout (%d) must be >= default timeout (%d)",
			cfg.Server.MaxTimeoutSec, cfg.Server.DefaultTimeoutSec)
	}

	if cfg.Server.MaxTimeoutSec > 3600 {
		result.AddWarning("Max timeout > 1 hour (%d seconds) - consider if this is intentional",
			cfg.Server.MaxTimeoutSec)
	}
}

func validateDatabase(cfg *config.Config, result *Result) {
	if cfg.Database.Host == "" {
		result.AddError("Database host is required")
	}

	if cfg.Database.User == "" {
		result.AddError("Database user is required")
	}

	if cfg.Database.Database == "" {
		result.AddError("Database name is required")
	}

	if cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
		result.AddError("Database port must be between 1 and 65535")
	}

	// Check for potential env var issues
	if strings.HasPrefix(cfg.Database.Host, "${") {
		result.AddWarning("Database host appears to be an unresolved env var: %s", cfg.Database.Host)
	}
	if strings.HasPrefix(cfg.Database.Password, "${") {
		result.AddWarning("Database password appears to be an unresolved env var")
	}
}

func validateLogging(cfg *config.Config, result *Result) {
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "warning": true, "error": true,
	}
	if !validLevels[strings.ToLower(cfg.Logging.Level)] {
		result.AddError("Invalid log level: %s (must be debug, info, warn, or error)", cfg.Logging.Level)
	}
}

func validateMetrics(cfg *config.Config, result *Result) {
	if cfg.Metrics.Enabled {
		if cfg.Metrics.FilePath == "" {
			result.AddError("Metrics enabled but file_path is not set")
		}

		if cfg.Metrics.IntervalSec < 10 {
			result.AddWarning("Metrics interval < 10 seconds may cause high I/O")
		}
	}
}

func validateQueries(cfg *config.Config, result *Result) {
	if len(cfg.Queries) == 0 {
		result.AddWarning("No queries configured - service will have no query endpoints")
	}

	paths := make(map[string]string) // path -> query name
	names := make(map[string]bool)

	for i, q := range cfg.Queries {
		prefix := fmt.Sprintf("Query #%d (%s)", i+1, q.Name)

		// Check for duplicate names
		if names[q.Name] {
			result.AddError("%s: duplicate query name", prefix)
		}
		names[q.Name] = true

		// Check for duplicate paths
		if existingName, exists := paths[q.Path]; exists {
			result.AddError("%s: path '%s' already used by query '%s'", prefix, q.Path, existingName)
		}
		paths[q.Path] = q.Name

		// Validate path format
		if !strings.HasPrefix(q.Path, "/") {
			result.AddError("%s: path must start with '/', got: %s", prefix, q.Path)
		}

		// Validate method
		if q.Method != "GET" && q.Method != "POST" {
			result.AddError("%s: method must be GET or POST, got: %s", prefix, q.Method)
		}

		// Check SQL for basic issues
		validateSQL(q, prefix, result)

		// Validate parameters
		validateParameters(q, prefix, result)

		// Check timeout
		if q.TimeoutSec < 0 {
			result.AddError("%s: timeout_sec cannot be negative", prefix)
		}
		if q.TimeoutSec > cfg.Server.MaxTimeoutSec {
			result.AddWarning("%s: query timeout (%d) exceeds max_timeout_sec (%d) and will be capped",
				prefix, q.TimeoutSec, cfg.Server.MaxTimeoutSec)
		}
	}
}

func validateSQL(q config.QueryConfig, prefix string, result *Result) {
	sql := strings.ToUpper(q.SQL)

	// Check for empty SQL
	if strings.TrimSpace(q.SQL) == "" {
		result.AddError("%s: SQL is empty", prefix)
		return
	}

	// Warn about write operations
	writeKeywords := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "CREATE ", "ALTER ", "TRUNCATE "}
	for _, kw := range writeKeywords {
		if strings.Contains(sql, kw) {
			result.AddWarning("%s: SQL contains '%s' - this service is intended for read-only queries",
				prefix, strings.TrimSpace(kw))
		}
	}

	// Check for parameters referenced in SQL
	paramRegex := regexp.MustCompile(`@(\w+)`)
	matches := paramRegex.FindAllStringSubmatch(q.SQL, -1)

	sqlParams := make(map[string]bool)
	for _, match := range matches {
		sqlParams[match[1]] = true
	}

	// Check that all required parameters in config are used in SQL
	for _, p := range q.Parameters {
		if !sqlParams[p.Name] {
			result.AddWarning("%s: parameter '%s' defined but not used in SQL (@%s)",
				prefix, p.Name, p.Name)
		}
	}

	// Check that all SQL parameters have config definitions
	configParams := make(map[string]bool)
	for _, p := range q.Parameters {
		configParams[p.Name] = true
	}
	for paramName := range sqlParams {
		if !configParams[paramName] {
			result.AddWarning("%s: SQL references @%s but no parameter definition found",
				prefix, paramName)
		}
	}
}

func validateParameters(q config.QueryConfig, prefix string, result *Result) {
	paramNames := make(map[string]bool)
	validTypes := map[string]bool{
		"string": true, "int": true, "integer": true,
		"float": true, "double": true,
		"bool": true, "boolean": true,
		"datetime": true, "date": true,
	}

	for j, p := range q.Parameters {
		paramPrefix := fmt.Sprintf("%s param #%d (%s)", prefix, j+1, p.Name)

		if p.Name == "" {
			result.AddError("%s: parameter name is empty", paramPrefix)
			continue
		}

		// Reserved parameter names
		if p.Name == "_timeout" {
			result.AddError("%s: '_timeout' is a reserved parameter name", paramPrefix)
		}

		// Duplicate check
		if paramNames[p.Name] {
			result.AddError("%s: duplicate parameter name", paramPrefix)
		}
		paramNames[p.Name] = true

		// Type check
		if !validTypes[strings.ToLower(p.Type)] {
			result.AddError("%s: invalid type '%s' (must be string, int, float, bool, or datetime)",
				paramPrefix, p.Type)
		}

		// Default value for required param
		if p.Required && p.Default != "" {
			result.AddWarning("%s: has both required=true and a default value - default will be ignored",
				paramPrefix)
		}
	}
}
