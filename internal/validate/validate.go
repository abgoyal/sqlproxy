package validate

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/robfig/cron/v3"

	"sql-proxy/internal/cache"
	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/tmpl"
	"sql-proxy/internal/webhook"
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
	validateRateLimits(cfg, r)
	validateQueries(cfg, r)

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

		// Validate database type (default to sqlserver)
		dbType := dbCfg.Type
		if dbType == "" {
			dbType = "sqlserver"
		}
		if !config.ValidDatabaseTypes[dbType] {
			r.addError("%s: invalid type '%s' (must be sqlserver or sqlite)", prefix, dbCfg.Type)
			continue
		}

		// Type-specific validation
		switch dbType {
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

func validateQueries(cfg *config.Config, r *Result) {
	if len(cfg.Queries) == 0 {
		r.addWarning("No queries configured - service will have no endpoints")
		return
	}

	// Build map of database names and their read-only status
	dbNames := make(map[string]bool) // name -> isReadOnly
	for _, dbCfg := range cfg.Databases {
		dbNames[dbCfg.Name] = dbCfg.IsReadOnly()
	}

	// Build map of rate limit pool names for validation
	rateLimitPools := make(map[string]bool)
	for _, pool := range cfg.RateLimits {
		if pool.Name != "" {
			rateLimitPools[pool.Name] = true
		}
	}

	// Track which databases are used
	usedDatabases := make(map[string]bool)

	paths := make(map[string]string)
	names := make(map[string]bool)

	for i, q := range cfg.Queries {
		prefix := fmt.Sprintf("queries[%d] (%s)", i, q.Name)

		if q.Name == "" {
			r.addError("queries[%d]: name is required", i)
			continue
		}

		if names[q.Name] {
			r.addError("%s: duplicate query name", prefix)
		}
		names[q.Name] = true

		// Validate database connection reference
		if q.Database == "" {
			r.addError("%s: database is required", prefix)
			continue
		}
		isReadOnly, dbExists := dbNames[q.Database]
		if !dbExists {
			r.addError("%s: references unknown database '%s'", prefix, q.Database)
		} else {
			usedDatabases[q.Database] = true
		}

		// Warn if query has neither HTTP endpoint nor schedule
		if q.Path == "" && q.Schedule == nil {
			r.addWarning("%s: has neither path nor schedule - query is unreachable", prefix)
		}

		// Validate HTTP endpoint settings (only if path is set)
		if q.Path != "" {
			if existing, ok := paths[q.Path]; ok {
				r.addError("%s: path '%s' already used by '%s'", prefix, q.Path, existing)
			}
			paths[q.Path] = q.Name

			if !strings.HasPrefix(q.Path, "/") {
				r.addError("%s: path must start with '/'", prefix)
			}

			// /_/ prefix is reserved for internal endpoints
			if strings.HasPrefix(q.Path, "/_/") {
				r.addError("%s: path cannot start with '/_/' (reserved for internal endpoints)", prefix)
			}

			if q.Method != "GET" && q.Method != "POST" {
				r.addError("%s: method must be GET or POST", prefix)
			}
		}

		if strings.TrimSpace(q.SQL) == "" {
			r.addError("%s: SQL is empty", prefix)
		}

		// Check for write operations
		sqlUpper := strings.ToUpper(q.SQL)
		writeKeywords := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "EXEC "}
		for _, kw := range writeKeywords {
			if strings.Contains(sqlUpper, kw) {
				if dbExists && isReadOnly {
					// Error: write query on read-only connection
					r.addError("%s: SQL contains %s but database '%s' is read-only", prefix, strings.TrimSpace(kw), q.Database)
				} else if dbExists && !isReadOnly {
					// Info: write operation on write-enabled connection (just note it)
					// No warning - this is intentional
				} else {
					// Database doesn't exist, already errored above - just warn about write
					r.addWarning("%s: SQL contains %s", prefix, strings.TrimSpace(kw))
				}
				break // Only report first write keyword found
			}
		}

		// Check SQL params match config params
		validateParams(q, prefix, r)

		// Validate timeout
		if q.TimeoutSec < 0 {
			r.addError("%s: timeout_sec cannot be negative", prefix)
		}

		// Validate session settings if specified
		if q.Isolation != "" && !config.ValidIsolationLevels[q.Isolation] {
			r.addError("%s: invalid isolation level '%s'", prefix, q.Isolation)
		}
		if q.DeadlockPriority != "" && !config.ValidDeadlockPriorities[q.DeadlockPriority] {
			r.addError("%s: invalid deadlock_priority '%s'", prefix, q.DeadlockPriority)
		}
		if q.LockTimeoutMs != nil && *q.LockTimeoutMs < 0 {
			r.addError("%s: lock_timeout_ms cannot be negative", prefix)
		}

		// Validate schedule if present
		if q.Schedule != nil {
			validateSchedule(q, prefix, r)
		}

		// Validate cache if present
		if q.Cache != nil {
			validateCache(q.Cache, q.Parameters, prefix, r)
		}

		// Validate json_columns if present
		if len(q.JSONColumns) > 0 {
			validateJSONColumns(q.JSONColumns, prefix, r)
		}

		// Validate rate limits if present
		if len(q.RateLimit) > 0 {
			validateQueryRateLimits(q.RateLimit, rateLimitPools, prefix, r)
		}
	}

	// Warn about unused database connections
	for name := range dbNames {
		if !usedDatabases[name] {
			r.addWarning("Database '%s' is configured but not used by any query", name)
		}
	}
}

func validateParams(q config.QueryConfig, prefix string, r *Result) {
	// Find @params in SQL using shared regex from db package
	matches := db.ParamRegex.FindAllStringSubmatch(q.SQL, -1)

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
		if p.Name == "_nocache" {
			r.addError("%s: '_nocache' is a reserved parameter name", prefix)
		}

		// Validate parameter type
		paramType := strings.ToLower(p.Type)
		if paramType == "" {
			paramType = "string" // Default type
		}
		if !config.ValidParameterTypes[paramType] {
			r.addError("%s: parameter '%s' has invalid type '%s'", prefix, p.Name, p.Type)
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

func validateSchedule(q config.QueryConfig, prefix string, r *Result) {
	sched := q.Schedule

	// Validate cron expression
	if sched.Cron == "" {
		r.addError("%s: schedule.cron is required", prefix)
	} else {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(sched.Cron); err != nil {
			r.addError("%s: invalid cron expression '%s': %v", prefix, sched.Cron, err)
		}
	}

	// Check that required params have values in schedule.params
	for _, p := range q.Parameters {
		if p.Required {
			if _, ok := sched.Params[p.Name]; !ok {
				// Check if there's a default value
				if p.Default == "" {
					r.addError("%s: required parameter '%s' must have a value in schedule.params", prefix, p.Name)
				}
			}
		}
	}

	// Validate dynamic date values
	validDynamicDates := map[string]bool{
		"now": true, "today": true, "yesterday": true, "tomorrow": true,
	}
	for name, value := range sched.Params {
		// Check if it looks like a dynamic date but is misspelled
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "to") || strings.HasPrefix(lower, "yes") || lower == "now" {
			if !validDynamicDates[lower] && validDynamicDates[strings.ToLower(value)] == false {
				// It might be a typo - only warn if it's close to a valid value
				for valid := range validDynamicDates {
					if strings.HasPrefix(lower, valid[:2]) && lower != valid {
						r.addWarning("%s: schedule.params.%s value '%s' looks like a typo for '%s'", prefix, name, value, valid)
						break
					}
				}
			}
		}
	}

	// Validate webhook if present
	if sched.Webhook != nil {
		validateWebhook(sched.Webhook, prefix, r)
	}
}

func validateWebhook(w *config.WebhookConfig, prefix string, r *Result) {
	webhookPrefix := prefix + ".webhook"

	// URL is required
	if w.URL == "" {
		r.addError("%s: url is required", webhookPrefix)
	}

	// Validate method if specified
	if w.Method != "" {
		validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true}
		if !validMethods[strings.ToUpper(w.Method)] {
			r.addError("%s: method must be GET, POST, PUT, or PATCH", webhookPrefix)
		}
	}

	// Validate body config if present
	if w.Body != nil {
		validateWebhookBody(w.Body, webhookPrefix, r)
	}
}

func validateWebhookBody(b *config.WebhookBodyConfig, prefix string, r *Result) {
	bodyPrefix := prefix + ".body"

	// Validate on_empty if specified
	if b.OnEmpty != "" && b.OnEmpty != "send" && b.OnEmpty != "skip" {
		r.addError("%s: on_empty must be 'send' or 'skip'", bodyPrefix)
	}

	// If empty template is provided, it implies on_empty: send
	if b.Empty != "" && b.OnEmpty == "skip" {
		r.addWarning("%s: 'empty' template is ignored when on_empty is 'skip'", bodyPrefix)
	}

	// Validate templates parse correctly (basic syntax check)
	templates := map[string]string{
		"header": b.Header,
		"item":   b.Item,
		"footer": b.Footer,
		"empty":  b.Empty,
	}
	for name, tmpl := range templates {
		if tmpl != "" {
			if err := validateTemplate(tmpl); err != nil {
				r.addError("%s.%s: invalid template: %v", bodyPrefix, name, err)
			}
		}
	}
}

func validateTemplate(tmpl string) error {
	_, err := template.New("").Funcs(webhook.TemplateFuncMap).Parse(tmpl)
	return err
}

func validateJSONColumns(columns []string, prefix string, r *Result) {
	seen := make(map[string]bool)
	for _, col := range columns {
		if col == "" {
			r.addError("%s: json_columns contains empty column name", prefix)
			continue
		}
		if seen[col] {
			r.addWarning("%s: json_columns contains duplicate column '%s'", prefix, col)
		}
		seen[col] = true
	}
}

func validateQueryRateLimits(limits []config.QueryRateLimitConfig, pools map[string]bool, prefix string, r *Result) {
	tmplEngine := tmpl.New()

	for i, limit := range limits {
		limitPrefix := fmt.Sprintf("%s.rate_limit[%d]", prefix, i)

		// Check for mutually exclusive settings
		if limit.IsPoolReference() && limit.IsInline() {
			r.addError("%s: cannot specify both 'pool' and inline rate limit settings (requests_per_second/burst/key)", limitPrefix)
			continue
		}

		if !limit.IsPoolReference() && !limit.IsInline() {
			r.addError("%s: must specify either 'pool' or inline rate limit settings", limitPrefix)
			continue
		}

		if limit.IsPoolReference() {
			// Validate pool reference
			if !pools[limit.Pool] {
				r.addError("%s: references unknown rate limit pool '%s'", limitPrefix, limit.Pool)
			}
		} else {
			// Validate inline settings
			if limit.RequestsPerSecond <= 0 {
				r.addError("%s: requests_per_second must be positive", limitPrefix)
			}
			if limit.Burst <= 0 {
				r.addError("%s: burst must be positive", limitPrefix)
			}
			if limit.Key == "" {
				r.addError("%s: key template is required for inline rate limit", limitPrefix)
			} else {
				// Validate key template syntax
				if err := tmplEngine.Validate(limit.Key, tmpl.UsagePreQuery); err != nil {
					r.addError("%s: invalid key template: %v", limitPrefix, err)
				}
			}
		}
	}
}

// cacheKeyFieldRegex matches template field references like {{.fieldName}} or {{.fieldName | ...}}
var cacheKeyFieldRegex = regexp.MustCompile(`\{\{\s*\.([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:\||\}\})`)

func validateCache(c *config.QueryCacheConfig, params []config.ParamConfig, prefix string, r *Result) {
	cachePrefix := prefix + ".cache"

	if !c.Enabled {
		return // Skip validation for disabled cache
	}

	// Key template is required when cache is enabled
	if c.Key == "" {
		r.addError("%s: key template is required when cache is enabled", cachePrefix)
	} else {
		// Validate key template syntax using same FuncMap as cache package
		if _, err := template.New("").Funcs(cache.KeyFuncMap).Parse(c.Key); err != nil {
			r.addError("%s: invalid key template: %v", cachePrefix, err)
		} else {
			// Check that template field references match defined parameters
			validateCacheKeyParams(c.Key, params, cachePrefix, r)
		}
	}

	// Validate TTL
	if c.TTLSec < 0 {
		r.addError("%s: ttl_sec cannot be negative", cachePrefix)
	}

	// Validate max size
	if c.MaxSizeMB < 0 {
		r.addError("%s: max_size_mb cannot be negative", cachePrefix)
	}

	// Validate evict_cron if specified
	if c.EvictCron != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(c.EvictCron); err != nil {
			r.addError("%s: invalid evict_cron expression '%s': %v", cachePrefix, c.EvictCron, err)
		}
	}
}

// validateCacheKeyParams checks that all field references in a cache key template
// correspond to defined query parameters.
func validateCacheKeyParams(keyTmpl string, params []config.ParamConfig, prefix string, r *Result) {
	// Build set of defined parameter names
	paramNames := make(map[string]bool)
	for _, p := range params {
		paramNames[p.Name] = true
	}

	// Find all field references in the template
	matches := cacheKeyFieldRegex.FindAllStringSubmatch(keyTmpl, -1)
	for _, m := range matches {
		fieldName := m[1]
		if !paramNames[fieldName] {
			r.addWarning("%s: key template references '{{.%s}}' but no parameter '%s' is defined",
				prefix, fieldName, fieldName)
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
