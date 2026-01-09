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
	Port              int `yaml:"port"`
	Host              string `yaml:"host"`
	DefaultTimeoutSec int `yaml:"default_timeout_sec"` // Default query timeout (can be overridden per-query or per-request)
	MaxTimeoutSec     int `yaml:"max_timeout_sec"`     // Maximum allowed timeout (caps request overrides)
}

type DatabaseConfig struct {
	Name     string `yaml:"name"`     // Connection name (required)
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	ReadOnly *bool  `yaml:"readonly"` // nil defaults to true for safety
}

// IsReadOnly returns whether this connection is read-only (defaults to true)
func (d *DatabaseConfig) IsReadOnly() bool {
	if d.ReadOnly == nil {
		return true // Default to read-only for safety
	}
	return *d.ReadOnly
}

type QueryConfig struct {
	Name        string          `yaml:"name"`
	Database    string          `yaml:"database"` // Connection name (required)
	Path        string          `yaml:"path"`     // HTTP path (required unless schedule-only)
	Method      string          `yaml:"method"`   // GET or POST (required when path is set)
	Description string          `yaml:"description"`
	SQL         string          `yaml:"sql"`
	Parameters  []ParamConfig   `yaml:"parameters"`
	TimeoutSec  int             `yaml:"timeout_sec"` // Query-specific timeout (0 = use server default)
	Schedule    *ScheduleConfig `yaml:"schedule"`    // Optional scheduled execution
}

type ScheduleConfig struct {
	Cron       string            `yaml:"cron"`        // Cron expression (e.g., "0 8 * * *" for 8 AM daily)
	Params     map[string]string `yaml:"params"`      // Parameter values for scheduled runs
	LogResults bool              `yaml:"log_results"` // Log first 10 result rows (default: false, just log count)
}

type ParamConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // string, int, datetime, bool
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

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

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	// Validate server config
	if c.Server.Host == "" {
		return fmt.Errorf("server.host is required")
	}
	if c.Server.Port == 0 {
		return fmt.Errorf("server.port is required")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535")
	}
	if c.Server.DefaultTimeoutSec == 0 {
		return fmt.Errorf("server.default_timeout_sec is required")
	}
	if c.Server.MaxTimeoutSec == 0 {
		return fmt.Errorf("server.max_timeout_sec is required")
	}
	if c.Server.MaxTimeoutSec < c.Server.DefaultTimeoutSec {
		return fmt.Errorf("server.max_timeout_sec must be >= server.default_timeout_sec")
	}

	// Validate logging config
	if c.Logging.Level == "" {
		return fmt.Errorf("logging.level is required")
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("logging.level must be debug, info, warn, or error")
	}
	if c.Logging.MaxSizeMB == 0 {
		return fmt.Errorf("logging.max_size_mb is required")
	}
	if c.Logging.MaxBackups == 0 {
		return fmt.Errorf("logging.max_backups is required")
	}
	if c.Logging.MaxAgeDays == 0 {
		return fmt.Errorf("logging.max_age_days is required")
	}

	// Validate database configs
	if len(c.Databases) == 0 {
		return fmt.Errorf("at least one database connection is required in 'databases'")
	}

	dbNames := make(map[string]bool)
	for i, db := range c.Databases {
		if db.Name == "" {
			return fmt.Errorf("databases[%d].name is required", i)
		}
		if dbNames[db.Name] {
			return fmt.Errorf("duplicate database name: %s", db.Name)
		}
		dbNames[db.Name] = true

		if db.Host == "" {
			return fmt.Errorf("databases[%d] (%s): host is required", i, db.Name)
		}
		if db.Port == 0 {
			return fmt.Errorf("databases[%d] (%s): port is required", i, db.Name)
		}
		if db.User == "" {
			return fmt.Errorf("databases[%d] (%s): user is required", i, db.Name)
		}
		if db.Password == "" {
			return fmt.Errorf("databases[%d] (%s): password is required", i, db.Name)
		}
		if db.Database == "" {
			return fmt.Errorf("databases[%d] (%s): database is required", i, db.Name)
		}
	}

	for i, q := range c.Queries {
		if q.Name == "" {
			return fmt.Errorf("queries[%d]: name is required", i)
		}
		if q.Database == "" {
			return fmt.Errorf("queries[%d] (%s): database is required", i, q.Name)
		}
		if !dbNames[q.Database] {
			return fmt.Errorf("queries[%d] (%s): unknown database '%s'", i, q.Name, q.Database)
		}
		// Path is required only if not a scheduled-only query
		if q.Path == "" && q.Schedule == nil {
			return fmt.Errorf("queries[%d] (%s): path is required (unless schedule is set)", i, q.Name)
		}
		if q.SQL == "" {
			return fmt.Errorf("queries[%d] (%s): sql is required", i, q.Name)
		}
		if q.Path != "" && q.Method == "" {
			return fmt.Errorf("queries[%d] (%s): method is required when path is set", i, q.Name)
		}
		if q.Path != "" && q.Method != "GET" && q.Method != "POST" {
			return fmt.Errorf("queries[%d] (%s): method must be GET or POST", i, q.Name)
		}
	}

	return nil
}
