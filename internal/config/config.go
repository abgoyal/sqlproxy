package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"sql-proxy/internal/publicid"
	"sql-proxy/internal/types"
	"sql-proxy/internal/workflow"
)

type Config struct {
	Server     ServerConfig          `yaml:"server"`
	Databases  []DatabaseConfig      `yaml:"databases"`
	Logging    LoggingConfig         `yaml:"logging"`
	Metrics    MetricsConfig         `yaml:"metrics"`
	Debug      DebugConfig           `yaml:"debug"`       // Debug/pprof endpoints
	RateLimits []RateLimitPoolConfig `yaml:"rate_limits"` // Named rate limit pools
	Workflows  []WorkflowConfig      `yaml:"workflows"`   // Workflow definitions
	Variables  VariablesConfig       `yaml:"variables"`   // Template variables
	PublicIDs  *PublicIDsConfig      `yaml:"public_ids"`  // Encrypted public IDs
}

// VariablesConfig defines variables available in templates via {{.vars.name}}
type VariablesConfig struct {
	EnvFile string            `yaml:"env_file"` // Optional path to env file (shell format)
	Values  map[string]string `yaml:"values"`   // Variable values with ${VAR} or ${VAR:default} expansion
}

// PublicIDsConfig configures encrypted public ID generation to prevent PK enumeration
type PublicIDsConfig struct {
	SecretKey  string                     `yaml:"secret_key"` // Required: 32+ character secret key
	Namespaces []publicid.NamespaceConfig `yaml:"namespaces"` // Namespace definitions with optional prefixes
}

// NamespaceConfig is re-exported from publicid for convenience
type NamespaceConfig = publicid.NamespaceConfig

// WorkflowConfig is re-exported from internal/workflow for use in main config
type WorkflowConfig = workflow.WorkflowConfig

type LoggingConfig struct {
	Level      string `yaml:"level"`        // debug, info, warn, error
	FilePath   string `yaml:"file_path"`    // Log file path (used in service mode)
	MaxSizeMB  int    `yaml:"max_size_mb"`  // Rotate at this size (MB)
	MaxBackups int    `yaml:"max_backups"`  // Old files to keep
	MaxAgeDays int    `yaml:"max_age_days"` // Delete after days
}

type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DebugConfig configures debug endpoints (pprof)
type DebugConfig struct {
	Enabled bool   `yaml:"enabled"` // Enable pprof endpoints (default: false)
	Port    int    `yaml:"port"`    // Port for debug endpoints (0 = same as main server)
	Host    string `yaml:"host"`    // Host for debug endpoints (default: localhost for security)
}

type ServerConfig struct {
	Port              int          `yaml:"port"`
	Host              string       `yaml:"host"`
	DefaultTimeoutSec int          `yaml:"default_timeout_sec"` // Default query timeout (can be overridden per-query or per-request)
	MaxTimeoutSec     int          `yaml:"max_timeout_sec"`     // Maximum allowed timeout (caps request overrides)
	Cache             *CacheConfig `yaml:"cache"`               // Optional cache configuration
	TrustProxyHeaders bool         `yaml:"trust_proxy_headers"` // Trust X-Forwarded-For/X-Real-IP for client IP (default: false)
	APIVersion        string       `yaml:"api_version"`         // API version for OpenAPI spec (e.g., "1.0.0")
	Version           string       `yaml:"-"`                   // Server version, set at runtime, not from config file
	BuildTime         string       `yaml:"-"`                   // Set at runtime, not from config file
}

// CacheConfig is server-level cache configuration
type CacheConfig struct {
	Enabled       bool `yaml:"enabled"`
	MaxSizeMB     int  `yaml:"max_size_mb"`     // Total cache limit in MB (default: 256)
	DefaultTTLSec int  `yaml:"default_ttl_sec"` // Default TTL in seconds (default: 300)
}

// EndpointCacheConfig is per-endpoint cache configuration (used by workflows)
type EndpointCacheConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Key       string `yaml:"key"`         // Template for cache key
	TTLSec    int    `yaml:"ttl_sec"`     // TTL in seconds (0 = use server default)
	MaxSizeMB int    `yaml:"max_size_mb"` // Per-endpoint cache limit in MB (0 = no limit)
	EvictCron string `yaml:"evict_cron"`  // Optional cron expression for scheduled eviction
}

// RateLimitPoolConfig defines a named rate limit pool that can be referenced by queries
type RateLimitPoolConfig struct {
	Name              string `yaml:"name"`                // Pool name (required, must be unique)
	RequestsPerSecond int    `yaml:"requests_per_second"` // Token refill rate (required)
	Burst             int    `yaml:"burst"`               // Maximum burst size (required)
	Key               string `yaml:"key"`                 // Template for bucket key (e.g., "{{.trigger.client_ip}}")
}

// RateLimitConfig is a rate limit configuration that can reference a named pool
// or define an inline limit. Used by workflows and the rate limiter.
type RateLimitConfig struct {
	// Reference a named pool (mutually exclusive with inline settings)
	Pool string `yaml:"pool"`

	// Inline rate limit settings (mutually exclusive with pool reference)
	RequestsPerSecond int    `yaml:"requests_per_second"`
	Burst             int    `yaml:"burst"`
	Key               string `yaml:"key"`
}

// IsPoolReference returns true if this config references a named pool
func (r *RateLimitConfig) IsPoolReference() bool {
	return r.Pool != ""
}

// IsInline returns true if this config defines valid inline rate limit settings.
// An inline config requires both RequestsPerSecond and Burst to be positive.
// Key is optional (defaults to ClientIP).
func (r *RateLimitConfig) IsInline() bool {
	return r.RequestsPerSecond > 0 && r.Burst > 0
}

type DatabaseConfig struct {
	Name string `yaml:"name"` // Connection name (required)
	Type string `yaml:"type"` // Database type: sqlserver, sqlite (default: sqlserver)

	// Connection settings (SQL Server, MySQL, PostgreSQL)
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`

	// Connection settings (SQLite)
	Path string `yaml:"path"` // File path or :memory: for in-memory database

	// Common settings
	ReadOnly *bool `yaml:"readonly"` // Connection routing: ApplicationIntent=ReadOnly (nil defaults to true)

	// SQL Server connection options
	Encrypt string `yaml:"encrypt"` // disable, false, true (default: disable)

	// Session defaults for queries using this connection (override implicit defaults)
	// SQL Server: isolation, lock_timeout_ms, deadlock_priority
	// SQLite: busy_timeout_ms, journal_mode
	Isolation        string `yaml:"isolation"`         // read_uncommitted, read_committed, repeatable_read, serializable, snapshot
	LockTimeoutMs    *int   `yaml:"lock_timeout_ms"`   // Lock wait timeout in ms (default: 5000)
	DeadlockPriority string `yaml:"deadlock_priority"` // low, normal, high (default: low)

	// SQLite-specific settings
	BusyTimeoutMs *int   `yaml:"busy_timeout_ms"` // SQLite busy timeout in ms (default: 5000)
	JournalMode   string `yaml:"journal_mode"`    // wal, delete, truncate, memory, off (default: wal)

	// Connection pool settings (applies to all database types)
	MaxOpenConns    *int `yaml:"max_open_conns"`     // Maximum open connections (default: 5)
	MaxIdleConns    *int `yaml:"max_idle_conns"`     // Maximum idle connections (default: 2)
	ConnMaxLifetime *int `yaml:"conn_max_lifetime"`  // Max connection lifetime in seconds (default: 300)
	ConnMaxIdleTime *int `yaml:"conn_max_idle_time"` // Max idle time in seconds (default: 120)
}

// IsReadOnly returns whether this connection is read-only (defaults to true)
func (d *DatabaseConfig) IsReadOnly() bool {
	if d.ReadOnly == nil {
		return true // Default to read-only for safety
	}
	return *d.ReadOnly
}

// ParamConfig is re-exported from internal/types for use in workflow configs
type ParamConfig = types.ParamConfig

// SessionConfig holds resolved SQL Server session-level settings
type SessionConfig struct {
	Isolation        string // read_uncommitted, read_committed, repeatable_read, serializable, snapshot
	LockTimeoutMs    int    // Lock wait timeout in milliseconds
	DeadlockPriority string // low, normal, high
}

// Valid isolation levels for SQL Server
var ValidIsolationLevels = map[string]bool{
	"read_uncommitted": true,
	"read_committed":   true,
	"repeatable_read":  true,
	"serializable":     true,
	"snapshot":         true,
}

// Valid deadlock priorities
var ValidDeadlockPriorities = map[string]bool{
	"low":    true,
	"normal": true,
	"high":   true,
}

// Valid SQLite journal modes
var ValidJournalModes = map[string]bool{
	"wal":      true,
	"delete":   true,
	"truncate": true,
	"memory":   true,
	"off":      true,
}

// Valid database types
var ValidDatabaseTypes = map[string]bool{
	"sqlserver": true,
	"sqlite":    true,
	// Future: "mysql", "postgres"
}

// ValidParameterTypes is re-exported from internal/types
var ValidParameterTypes = types.ValidParamTypes

// IsArrayType is re-exported from internal/types
var IsArrayType = types.IsArrayType

// ArrayBaseType is re-exported from internal/types
var ArrayBaseType = types.ArrayBaseType

// DefaultSessionConfig returns implicit defaults based on readonly flag
func (d *DatabaseConfig) DefaultSessionConfig() SessionConfig {
	cfg := SessionConfig{
		LockTimeoutMs:    5000,
		DeadlockPriority: "low",
	}
	if d.IsReadOnly() {
		cfg.Isolation = "read_uncommitted"
	} else {
		cfg.Isolation = "read_committed"
	}
	return cfg
}

// Load parses a YAML config file, expanding environment variables.
// If variables.env_file is specified, those values are loaded first, then
// actual environment variables override them. Finally, variables.values are added
// and can be used throughout the config.
//
// Variable syntax:
// - ${VAR} and ${VAR:default}: ONLY valid in variables.values section (imports env vars)
// - {{.vars.X}}: Valid anywhere to reference imported variables
//
// Use validate.Run() for comprehensive validation after loading.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// First pass: parse to get env_file and variables.values (before expansion)
	var preConfig struct {
		Variables struct {
			EnvFile string            `yaml:"env_file"`
			Values  map[string]string `yaml:"values"`
		} `yaml:"variables"`
	}
	if err := yaml.Unmarshal(data, &preConfig); err != nil {
		return nil, fmt.Errorf("failed to pre-parse config: %w", err)
	}

	// Build the variable lookup: env file values first
	varLookup := make(map[string]string)

	if preConfig.Variables.EnvFile != "" {
		envFilePath := preConfig.Variables.EnvFile
		// Resolve relative paths based on config file location
		if !filepath.IsAbs(envFilePath) {
			envFilePath = filepath.Join(filepath.Dir(path), envFilePath)
		}
		fileVars, err := loadEnvFile(envFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load env file %q (resolved to %q): %w", preConfig.Variables.EnvFile, envFilePath, err)
		}
		for k, v := range fileVars {
			varLookup[k] = v
		}
	}

	// Selectively load only referenced environment variables (overrides file values)
	// This avoids loading ALL env vars into memory - only those needed for config
	if preConfig.Variables.Values != nil {
		for _, v := range preConfig.Variables.Values {
			// Find all ${VAR} or ${VAR:default} references
			matches := varPattern.FindAllStringSubmatch(v, -1)
			for _, match := range matches {
				if len(match) >= 2 {
					varName := match[1]
					if val, ok := os.LookupEnv(varName); ok {
						varLookup[varName] = val
					}
				}
			}
		}
	}

	// Expand ${VAR} syntax ONLY within variables.values entries
	// This allows importing environment variables into the config namespace
	if preConfig.Variables.Values != nil {
		for k, v := range preConfig.Variables.Values {
			preConfig.Variables.Values[k] = expandVars(v, varLookup)
		}
	}

	// Pre-render {{.vars.X}} templates in the raw YAML before parsing.
	// This allows template syntax to work in numeric fields (like server.port)
	// which would otherwise fail YAML parsing with unexpanded template strings.
	expandedYAML := preRenderVarsTemplates(string(data), preConfig.Variables.Values)

	var cfg Config
	if err := yaml.Unmarshal([]byte(expandedYAML), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Copy the expanded variables.values to the final config
	// The YAML parse only sees the original ${VAR} syntax, not the expanded values
	cfg.Variables.Values = preConfig.Variables.Values

	// Render static templates in must-be-static fields
	// These fields support {{.vars.X}} syntax and pure template functions
	// Most .vars references are already expanded by preRenderVarsTemplates,
	// but this handles more complex template expressions (function calls, etc.)
	if err := renderStaticFields(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// renderStaticFields renders {{}} templates in config fields that must be resolved at load time.
// Returns an error if any template references dynamic paths (like .trigger or .steps).
func renderStaticFields(cfg *Config) error {
	staticCtx := &StaticContext{
		Vars: cfg.Variables.Values,
	}
	if staticCtx.Vars == nil {
		staticCtx.Vars = make(map[string]string)
	}

	// Database connection fields
	for i := range cfg.Databases {
		db := &cfg.Databases[i]
		var err error

		if db.Host, err = RenderStaticTemplate(db.Host, staticCtx); err != nil {
			return fmt.Errorf("databases[%d].host: %w", i, err)
		}
		if db.User, err = RenderStaticTemplate(db.User, staticCtx); err != nil {
			return fmt.Errorf("databases[%d].user: %w", i, err)
		}
		if db.Password, err = RenderStaticTemplate(db.Password, staticCtx); err != nil {
			return fmt.Errorf("databases[%d].password: %w", i, err)
		}
		if db.Database, err = RenderStaticTemplate(db.Database, staticCtx); err != nil {
			return fmt.Errorf("databases[%d].database: %w", i, err)
		}
		if db.Path, err = RenderStaticTemplate(db.Path, staticCtx); err != nil {
			return fmt.Errorf("databases[%d].path: %w", i, err)
		}
	}

	// Public IDs secret key
	if cfg.PublicIDs != nil {
		var err error
		if cfg.PublicIDs.SecretKey, err = RenderStaticTemplate(cfg.PublicIDs.SecretKey, staticCtx); err != nil {
			return fmt.Errorf("public_ids.secret_key: %w", err)
		}
	}

	// Parameter defaults in workflows
	for i := range cfg.Workflows {
		wf := &cfg.Workflows[i]
		for j := range wf.Triggers {
			trigger := &wf.Triggers[j]
			for k := range trigger.Parameters {
				param := &trigger.Parameters[k]
				if param.Default != "" {
					var err error
					if param.Default, err = RenderStaticTemplate(param.Default, staticCtx); err != nil {
						return fmt.Errorf("workflows[%d].triggers[%d].parameters[%d].default: %w", i, j, k, err)
					}
				}
			}
		}
	}

	return nil
}

// loadEnvFile parses a .env file using godotenv.
// Supports: KEY=value, KEY="value", KEY='value', # comments, export prefix,
// escaped quotes within values, and multi-line values in quotes.
func loadEnvFile(path string) (map[string]string, error) {
	return godotenv.Read(path)
}

// varPattern matches ${VAR} and ${VAR:default} syntax
var varPattern = regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)

// expandVars expands ${VAR} and ${VAR:default} patterns using the lookup map.
func expandVars(s string, lookup map[string]string) string {
	return varPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := varPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		defaultVal := ""
		if len(parts) >= 3 {
			defaultVal = parts[2]
		}

		if val, ok := lookup[varName]; ok {
			return val
		}
		return defaultVal
	})
}

// varsTemplatePattern matches simple {{.vars.X}} references.
// Only matches direct .vars.NAME references, not function calls or complex expressions.
var varsTemplatePattern = regexp.MustCompile(`\{\{\s*\.vars\.([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// preRenderVarsTemplates expands simple {{.vars.X}} templates in the raw YAML string.
// This is done BEFORE YAML parsing to allow template syntax in numeric fields.
// Only simple variable references are expanded; complex expressions (function calls, etc.)
// are left for renderStaticFields to handle after YAML parsing.
func preRenderVarsTemplates(yamlStr string, vars map[string]string) string {
	if vars == nil {
		vars = make(map[string]string)
	}

	return varsTemplatePattern.ReplaceAllStringFunc(yamlStr, func(match string) string {
		parts := varsTemplatePattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		if val, ok := vars[varName]; ok {
			return val
		}
		// Variable not found - leave the template unexpanded
		// This will be caught as an error during renderStaticFields or at runtime
		return match
	})
}
