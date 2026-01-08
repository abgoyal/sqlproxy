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

func (r *Result) addError(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
	r.Valid = false
}

func (r *Result) addWarning(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// Run validates config format, then tests DB connection if config is complete
func Run(cfg *config.Config) *Result {
	r := &Result{Valid: true}

	// Validate format
	validateServer(cfg, r)
	validateDatabase(cfg, r)
	validateLogging(cfg, r)
	validateQueries(cfg, r)

	// If format is valid and DB config is complete, test connection
	if r.Valid && cfg.Database.Host != "" && cfg.Database.User != "" {
		testDBConnection(cfg, r)
	}

	return r
}

func validateServer(cfg *config.Config, r *Result) {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		r.addError("Server port must be 1-65535, got: %d", cfg.Server.Port)
	}
	if cfg.Server.DefaultTimeoutSec < 1 {
		r.addError("Default timeout must be at least 1 second")
	}
	if cfg.Server.MaxTimeoutSec < cfg.Server.DefaultTimeoutSec {
		r.addError("Max timeout (%d) must be >= default timeout (%d)",
			cfg.Server.MaxTimeoutSec, cfg.Server.DefaultTimeoutSec)
	}
}

func validateDatabase(cfg *config.Config, r *Result) {
	if cfg.Database.Host == "" {
		r.addError("Database host is required")
	}
	if cfg.Database.User == "" {
		r.addError("Database user is required")
	}
	if cfg.Database.Database == "" {
		r.addError("Database name is required")
	}

	// Check for unresolved env vars
	if strings.HasPrefix(cfg.Database.Host, "${") {
		r.addWarning("Database host appears to be an unresolved env var: %s", cfg.Database.Host)
	}
	if strings.HasPrefix(cfg.Database.Password, "${") {
		r.addWarning("Database password appears to be an unresolved env var")
	}
}

func validateLogging(cfg *config.Config, r *Result) {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(cfg.Logging.Level)] {
		r.addError("Invalid log level: %s (must be debug, info, warn, or error)", cfg.Logging.Level)
	}
}

func validateQueries(cfg *config.Config, r *Result) {
	if len(cfg.Queries) == 0 {
		r.addWarning("No queries configured - service will have no query endpoints")
		return
	}

	paths := make(map[string]string)
	names := make(map[string]bool)

	for i, q := range cfg.Queries {
		prefix := fmt.Sprintf("Query #%d (%s)", i+1, q.Name)

		if names[q.Name] {
			r.addError("%s: duplicate query name", prefix)
		}
		names[q.Name] = true

		if existing, ok := paths[q.Path]; ok {
			r.addError("%s: path '%s' already used by '%s'", prefix, q.Path, existing)
		}
		paths[q.Path] = q.Name

		if !strings.HasPrefix(q.Path, "/") {
			r.addError("%s: path must start with '/'", prefix)
		}

		if q.Method != "GET" && q.Method != "POST" {
			r.addError("%s: method must be GET or POST", prefix)
		}

		if strings.TrimSpace(q.SQL) == "" {
			r.addError("%s: SQL is empty", prefix)
		}

		// Warn about write operations
		sqlUpper := strings.ToUpper(q.SQL)
		for _, kw := range []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE "} {
			if strings.Contains(sqlUpper, kw) {
				r.addWarning("%s: SQL contains %s- this service is for read-only queries", prefix, strings.TrimSpace(kw))
			}
		}

		// Check SQL params match config params
		validateParams(q, prefix, r)
	}
}

func validateParams(q config.QueryConfig, prefix string, r *Result) {
	// Find @params in SQL
	re := regexp.MustCompile(`@(\w+)`)
	matches := re.FindAllStringSubmatch(q.SQL, -1)

	sqlParams := make(map[string]bool)
	for _, m := range matches {
		sqlParams[m[1]] = true
	}

	configParams := make(map[string]bool)
	for _, p := range q.Parameters {
		configParams[p.Name] = true

		if p.Name == "_timeout" {
			r.addError("%s: '_timeout' is a reserved parameter name", prefix)
		}
	}

	// Warn about mismatches
	for name := range sqlParams {
		if !configParams[name] {
			r.addWarning("%s: SQL references @%s but no parameter definition found", prefix, name)
		}
	}
	for name := range configParams {
		if !sqlParams[name] {
			r.addWarning("%s: parameter '%s' defined but not used in SQL", prefix, name)
		}
	}
}

func testDBConnection(cfg *config.Config, r *Result) {
	database, err := db.New(cfg.Database)
	if err != nil {
		r.addError("Database connection failed: %v", err)
		return
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := database.Ping(ctx); err != nil {
		r.addError("Database ping failed: %v", err)
	}
}
