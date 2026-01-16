package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

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
}

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
	Key               string `yaml:"key"`                 // Template for bucket key (e.g., "{{.ClientIP}}")
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
