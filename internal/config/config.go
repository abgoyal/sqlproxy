package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Databases []DatabaseConfig `yaml:"databases"`
	Logging   LoggingConfig    `yaml:"logging"`
	Metrics   MetricsConfig    `yaml:"metrics"`
	Queries   []QueryConfig    `yaml:"queries"`
}

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

type ServerConfig struct {
	Port              int          `yaml:"port"`
	Host              string       `yaml:"host"`
	DefaultTimeoutSec int          `yaml:"default_timeout_sec"` // Default query timeout (can be overridden per-query or per-request)
	MaxTimeoutSec     int          `yaml:"max_timeout_sec"`     // Maximum allowed timeout (caps request overrides)
	Cache             *CacheConfig `yaml:"cache"`               // Optional cache configuration
	Version           string       `yaml:"-"`                   // Set at runtime, not from config file
	BuildTime         string       `yaml:"-"`                   // Set at runtime, not from config file
}

// CacheConfig is server-level cache configuration
type CacheConfig struct {
	Enabled       bool `yaml:"enabled"`
	MaxSizeMB     int  `yaml:"max_size_mb"`      // Total cache limit in MB (default: 256)
	DefaultTTLSec int  `yaml:"default_ttl_sec"`  // Default TTL in seconds (default: 300)
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
	Isolation        string `yaml:"isolation"`          // read_uncommitted, read_committed, repeatable_read, serializable, snapshot
	LockTimeoutMs    *int   `yaml:"lock_timeout_ms"`    // Lock wait timeout in ms (default: 5000)
	DeadlockPriority string `yaml:"deadlock_priority"`  // low, normal, high (default: low)

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

type QueryConfig struct {
	Name        string            `yaml:"name"`
	Database    string            `yaml:"database"` // Connection name (required)
	Path        string            `yaml:"path"`     // HTTP path (required unless schedule-only)
	Method      string            `yaml:"method"`   // GET or POST (required when path is set)
	Description string            `yaml:"description"`
	SQL         string            `yaml:"sql"`
	Parameters  []ParamConfig     `yaml:"parameters"`
	TimeoutSec  int               `yaml:"timeout_sec"` // Query-specific timeout (0 = use server default)
	Schedule    *ScheduleConfig   `yaml:"schedule"`    // Optional scheduled execution
	Cache       *QueryCacheConfig `yaml:"cache"`       // Optional cache configuration

	// Session overrides (empty = use connection default)
	Isolation        string `yaml:"isolation"`         // Override isolation level for this query
	LockTimeoutMs    *int   `yaml:"lock_timeout_ms"`   // Override lock timeout for this query
	DeadlockPriority string `yaml:"deadlock_priority"` // Override deadlock priority for this query

	// Response transformation
	JSONColumns []string `yaml:"json_columns"` // Column names containing JSON to parse (e.g., ["data", "metadata"])
}

// QueryCacheConfig is per-query cache configuration
type QueryCacheConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Key       string `yaml:"key"`          // Template for cache key (e.g., "machines:{{.status}}")
	TTLSec    int    `yaml:"ttl_sec"`      // TTL in seconds (0 = use server default)
	MaxSizeMB int    `yaml:"max_size_mb"`  // Per-endpoint cache limit in MB (0 = no limit)
	EvictCron string `yaml:"evict_cron"`   // Optional cron expression for scheduled eviction
}

type ScheduleConfig struct {
	Cron       string            `yaml:"cron"`        // Cron expression (e.g., "0 8 * * *" for 8 AM daily)
	Params     map[string]string `yaml:"params"`      // Parameter values for scheduled runs
	LogResults bool              `yaml:"log_results"` // Log first 10 result rows (default: false, just log count)
	Webhook    *WebhookConfig    `yaml:"webhook"`     // Optional webhook to call after query execution
}

// WebhookConfig defines an outgoing webhook to call after query execution
type WebhookConfig struct {
	URL     string              `yaml:"url"`     // Target URL (supports templates: {{.query}}, {{.count}})
	Method  string              `yaml:"method"`  // HTTP method (default: POST)
	Headers map[string]string   `yaml:"headers"` // HTTP headers (supports env vars: ${TOKEN})
	Body    *WebhookBodyConfig  `yaml:"body"`    // Body template config (if nil, sends raw query results)
	Retry   *WebhookRetryConfig `yaml:"retry"`   // Retry configuration (default: enabled with 3 retries)
}

// WebhookRetryConfig defines retry behavior for failed webhooks
type WebhookRetryConfig struct {
	Enabled           *bool `yaml:"enabled"`             // Whether retries are enabled (default: true)
	MaxAttempts       int   `yaml:"max_attempts"`        // Max retry attempts, 0 = no retries (default: 3)
	InitialBackoffSec int   `yaml:"initial_backoff_sec"` // Initial backoff delay in seconds (default: 1)
	MaxBackoffSec     int   `yaml:"max_backoff_sec"`     // Maximum backoff delay in seconds (default: 30)
}

// WebhookBodyConfig defines the body template structure
type WebhookBodyConfig struct {
	Header    string `yaml:"header"`    // Template for body prefix (access: .count, .query, .success, .duration_ms, .params, .data)
	Item      string `yaml:"item"`      // Template for each result row (access: row fields, ._index, ._count)
	Footer    string `yaml:"footer"`    // Template for body suffix (same access as header)
	Separator string `yaml:"separator"` // Separator between items (default: ",")
	OnEmpty   string `yaml:"on_empty"`  // Behavior when no results: "send" (default) or "skip"
	Empty     string `yaml:"empty"`     // Alternate body template when count=0 (overrides on_empty)
}

type ParamConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // string, int, datetime, bool
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

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

// Valid parameter types
var ValidParameterTypes = map[string]bool{
	// Scalar types
	"string":   true,
	"int":      true,
	"integer":  true,
	"float":    true,
	"double":   true,
	"bool":     true,
	"boolean":  true,
	"datetime": true,
	"date":     true,
	// JSON type - accepts any JSON value (object, array, primitive), serializes to string
	"json": true,
	// Array types - accept JSON arrays, serialize to JSON array string for use with json_each/OPENJSON
	"int[]":    true,
	"string[]": true,
	"float[]":  true,
	"bool[]":   true,
}

// IsArrayType returns true if the type is an array type
func IsArrayType(typeName string) bool {
	return len(typeName) > 2 && typeName[len(typeName)-2:] == "[]"
}

// ArrayBaseType returns the base type of an array type (e.g., "int[]" -> "int")
func ArrayBaseType(typeName string) string {
	if IsArrayType(typeName) {
		return typeName[:len(typeName)-2]
	}
	return typeName
}

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

// ResolveSessionConfig returns the effective session config for a query
// Priority: query settings > database settings > implicit defaults
func ResolveSessionConfig(dbCfg DatabaseConfig, queryCfg QueryConfig) SessionConfig {
	// Start with implicit defaults based on readonly
	cfg := dbCfg.DefaultSessionConfig()

	// Apply database-level overrides
	if dbCfg.Isolation != "" {
		cfg.Isolation = dbCfg.Isolation
	}
	if dbCfg.LockTimeoutMs != nil {
		cfg.LockTimeoutMs = *dbCfg.LockTimeoutMs
	}
	if dbCfg.DeadlockPriority != "" {
		cfg.DeadlockPriority = dbCfg.DeadlockPriority
	}

	// Apply query-level overrides
	if queryCfg.Isolation != "" {
		cfg.Isolation = queryCfg.Isolation
	}
	if queryCfg.LockTimeoutMs != nil {
		cfg.LockTimeoutMs = *queryCfg.LockTimeoutMs
	}
	if queryCfg.DeadlockPriority != "" {
		cfg.DeadlockPriority = queryCfg.DeadlockPriority
	}

	return cfg
}

// Load parses a YAML config file, expanding environment variables.
// Use validate.Run() for comprehensive validation after loading.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in the config
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}
