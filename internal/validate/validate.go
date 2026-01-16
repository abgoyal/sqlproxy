package validate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/tmpl"
	"sql-proxy/internal/workflow"
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

// Run validates config format, then tests DB connections if config is complete
func Run(cfg *config.Config) *Result {
	r := &Result{Valid: true}

	// Validate format
	validateServer(cfg, r)
	validateDatabase(cfg, r)
	validateLogging(cfg, r)
	validateDebug(cfg, r)
	validateRateLimits(cfg, r)

	// Validate workflows
	if len(cfg.Workflows) == 0 {
		r.addWarning("No workflows configured - service will have no endpoints")
	} else {
		validateWorkflows(cfg, r)
	}

	// If format is valid, test database connections
	if r.Valid {
		testDBConnections(cfg, r)
	}

	return r
}

func validateServer(cfg *config.Config, r *Result) {
	// Host validation
	if cfg.Server.Host == "" {
		r.addError("server.host is required")
	}

	// Port validation
	if cfg.Server.Port == 0 {
		r.addError("server.port is required")
	} else if cfg.Server.Port < 0 || cfg.Server.Port > 65535 {
		r.addError("server.port must be 1-65535, got: %d", cfg.Server.Port)
	}

	// Timeout validation
	if cfg.Server.DefaultTimeoutSec == 0 {
		r.addError("server.default_timeout_sec is required")
	} else if cfg.Server.DefaultTimeoutSec < 1 {
		r.addError("server.default_timeout_sec must be at least 1 second")
	}
	if cfg.Server.MaxTimeoutSec == 0 {
		r.addError("server.max_timeout_sec is required")
	} else if cfg.Server.MaxTimeoutSec < cfg.Server.DefaultTimeoutSec {
		r.addError("server.max_timeout_sec (%d) must be >= server.default_timeout_sec (%d)",
			cfg.Server.MaxTimeoutSec, cfg.Server.DefaultTimeoutSec)
	}

	// Validate cache configuration
	if cfg.Server.Cache != nil && cfg.Server.Cache.Enabled {
		if cfg.Server.Cache.MaxSizeMB < 0 {
			r.addError("server.cache.max_size_mb cannot be negative")
		}
		if cfg.Server.Cache.DefaultTTLSec < 0 {
			r.addError("server.cache.default_ttl_sec cannot be negative")
		}
	}
}

func validateDatabase(cfg *config.Config, r *Result) {
	if len(cfg.Databases) == 0 {
		r.addError("At least one database connection is required in 'databases'")
		return
	}

	names := make(map[string]bool)
	for i, dbCfg := range cfg.Databases {
		prefix := fmt.Sprintf("databases[%d] (%s)", i, dbCfg.Name)

		if dbCfg.Name == "" {
			r.addError("databases[%d]: name is required", i)
			continue
		}

		if names[dbCfg.Name] {
			r.addError("%s: duplicate database name", prefix)
		}
		names[dbCfg.Name] = true

		// Validate database type
		if dbCfg.Type == "" {
			r.addError("%s: type is required (must be sqlserver or sqlite)", prefix)
			continue
		}
		if !config.ValidDatabaseTypes[dbCfg.Type] {
			r.addError("%s: invalid type '%s' (must be sqlserver or sqlite)", prefix, dbCfg.Type)
			continue
		}

		// Type-specific validation
		switch dbCfg.Type {
		case "sqlserver":
			if dbCfg.Host == "" {
				r.addError("%s: host is required for sqlserver", prefix)
			}
			if dbCfg.Port == 0 {
				r.addError("%s: port is required for sqlserver", prefix)
			}
			if dbCfg.User == "" {
				r.addError("%s: user is required for sqlserver", prefix)
			}
			if dbCfg.Password == "" {
				r.addError("%s: password is required for sqlserver", prefix)
			}
			if dbCfg.Database == "" {
				r.addError("%s: database is required for sqlserver", prefix)
			}

			// Check for unresolved env vars
			if strings.HasPrefix(dbCfg.Host, "${") {
				r.addWarning("%s: host appears to be an unresolved env var: %s", prefix, dbCfg.Host)
			}
			if strings.HasPrefix(dbCfg.Password, "${") {
				r.addWarning("%s: password appears to be an unresolved env var", prefix)
			}

			// Validate SQL Server session settings
			if dbCfg.Isolation != "" && !config.ValidIsolationLevels[dbCfg.Isolation] {
				r.addError("%s: invalid isolation level '%s' (must be read_uncommitted, read_committed, repeatable_read, serializable, or snapshot)", prefix, dbCfg.Isolation)
			}
			if dbCfg.DeadlockPriority != "" && !config.ValidDeadlockPriorities[dbCfg.DeadlockPriority] {
				r.addError("%s: invalid deadlock_priority '%s' (must be low, normal, or high)", prefix, dbCfg.DeadlockPriority)
			}
			if dbCfg.LockTimeoutMs != nil && *dbCfg.LockTimeoutMs < 0 {
				r.addError("%s: lock_timeout_ms cannot be negative", prefix)
			}

		case "sqlite":
			if dbCfg.Path == "" {
				r.addError("%s: path is required for sqlite", prefix)
			}

			// Validate SQLite session settings
			if dbCfg.JournalMode != "" && !config.ValidJournalModes[dbCfg.JournalMode] {
				r.addError("%s: invalid journal_mode '%s' (must be wal, delete, truncate, memory, or off)", prefix, dbCfg.JournalMode)
			}
			if dbCfg.BusyTimeoutMs != nil && *dbCfg.BusyTimeoutMs < 0 {
				r.addError("%s: busy_timeout_ms cannot be negative", prefix)
			}
		}
	}
}

func validateLogging(cfg *config.Config, r *Result) {
	// Level validation
	if cfg.Logging.Level == "" {
		r.addError("logging.level is required")
	} else {
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[strings.ToLower(cfg.Logging.Level)] {
			r.addError("logging.level must be debug, info, warn, or error, got: %s", cfg.Logging.Level)
		}
	}

	// Log rotation settings (required for service mode)
	if cfg.Logging.MaxSizeMB == 0 {
		r.addError("logging.max_size_mb is required")
	} else if cfg.Logging.MaxSizeMB < 0 {
		r.addError("logging.max_size_mb cannot be negative")
	}
	if cfg.Logging.MaxBackups == 0 {
		r.addError("logging.max_backups is required")
	} else if cfg.Logging.MaxBackups < 0 {
		r.addError("logging.max_backups cannot be negative")
	}
	if cfg.Logging.MaxAgeDays == 0 {
		r.addError("logging.max_age_days is required")
	} else if cfg.Logging.MaxAgeDays < 0 {
		r.addError("logging.max_age_days cannot be negative")
	}
}

func validateDebug(cfg *config.Config, r *Result) {
	if !cfg.Debug.Enabled {
		return // Skip validation if debug is disabled
	}

	// Port validation
	if cfg.Debug.Port < 0 || cfg.Debug.Port > 65535 {
		r.addError("debug.port must be 0-65535, got: %d", cfg.Debug.Port)
	}

	// If debug.host is set but port is shared with main server, it's an error
	// because the host setting will be silently ignored
	sharesMainPort := cfg.Debug.Port == 0 || cfg.Debug.Port == cfg.Server.Port
	if cfg.Debug.Host != "" && sharesMainPort {
		r.addError("debug.host cannot be set when debug endpoints share the main server port (debug.port is 0 or same as server.port); debug.host only applies when using a separate debug port")
	}
}

func validateRateLimits(cfg *config.Config, r *Result) {
	if len(cfg.RateLimits) == 0 {
		return // Rate limits are optional
	}

	// Create template engine for validation
	tmplEngine := tmpl.New()

	names := make(map[string]bool)
	for i, pool := range cfg.RateLimits {
		prefix := fmt.Sprintf("rate_limits[%d]", i)

		// Name is required and must be unique
		if pool.Name == "" {
			r.addError("%s: name is required", prefix)
			continue
		}
		prefix = fmt.Sprintf("rate_limits[%d] (%s)", i, pool.Name)

		if names[pool.Name] {
			r.addError("%s: duplicate pool name", prefix)
		}
		names[pool.Name] = true

		// Prevent conflicts with internal inline pool naming convention
		if strings.HasPrefix(pool.Name, "_inline:") {
			r.addError("%s: pool name cannot start with '_inline:' (reserved for internal use)", prefix)
		}

		// RequestsPerSecond and Burst are required
		if pool.RequestsPerSecond <= 0 {
			r.addError("%s: requests_per_second must be positive", prefix)
		}
		if pool.Burst <= 0 {
			r.addError("%s: burst must be positive", prefix)
		}

		// Key template is required
		if pool.Key == "" {
			r.addError("%s: key template is required", prefix)
		} else {
			// Validate key template syntax
			if err := tmplEngine.Validate(pool.Key, tmpl.UsagePreQuery); err != nil {
				r.addError("%s: invalid key template: %v", prefix, err)
			}
		}
	}
}

func testDBConnections(cfg *config.Config, r *Result) {
	for _, dbCfg := range cfg.Databases {
		// Determine database type (default to sqlserver)
		dbType := dbCfg.Type
		if dbType == "" {
			dbType = "sqlserver"
		}

		// Skip if config incomplete (unresolved env vars) - only for sqlserver
		if dbType == "sqlserver" {
			if strings.HasPrefix(dbCfg.Host, "${") {
				continue
			}
			if strings.HasPrefix(dbCfg.Password, "${") {
				continue
			}
		}

		driver, err := db.NewDriver(dbCfg)
		if err != nil {
			r.addError("databases[%s]: connection failed: %v", dbCfg.Name, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = driver.Ping(ctx)
		cancel()
		driver.Close()

		if err != nil {
			r.addError("databases[%s]: ping failed: %v", dbCfg.Name, err)
		}
	}
}

func validateWorkflows(cfg *config.Config, r *Result) {
	// Build validation context for workflows
	databases := make(map[string]bool)
	for _, dbCfg := range cfg.Databases {
		databases[dbCfg.Name] = dbCfg.IsReadOnly()
	}
	rateLimitPools := make(map[string]bool)
	for _, rl := range cfg.RateLimits {
		rateLimitPools[rl.Name] = true
	}
	validationCtx := &workflow.ValidationContext{
		Databases:      databases,
		RateLimitPools: rateLimitPools,
	}

	// Validate each workflow
	for i, wfCfg := range cfg.Workflows {
		wfCfgCopy := wfCfg // Copy to avoid closure issues
		result := workflow.Validate(&wfCfgCopy, validationCtx)

		// Add workflow validation errors to our result
		for _, err := range result.Errors {
			r.addError("workflows[%d]: %s", i, err)
		}
		for _, warning := range result.Warnings {
			r.addWarning("workflows[%d]: %s", i, warning)
		}
	}
}
