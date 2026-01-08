package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Logging  LoggingConfig  `yaml:"logging"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Queries  []QueryConfig  `yaml:"queries"`
}

type LoggingConfig struct {
	Level      string `yaml:"level"`        // debug, info, warn, error
	FilePath   string `yaml:"file_path"`    // Log file path (empty = stdout only)
	MaxSizeMB  int    `yaml:"max_size_mb"`  // Max size before rotation (default 100)
	MaxBackups int    `yaml:"max_backups"`  // Max old files to keep (default 5)
	MaxAgeDays int    `yaml:"max_age_days"` // Max days to retain (default 30)
	Compress   bool   `yaml:"compress"`     // Compress rotated files
}

type MetricsConfig struct {
	Enabled     bool   `yaml:"enabled"`
	FilePath    string `yaml:"file_path"`    // Metrics output file
	IntervalSec int    `yaml:"interval_sec"` // Export interval (default 300 = 5 min)
	RetainFiles int    `yaml:"retain_files"` // Number of metric files to keep (default 288)
}

type ServerConfig struct {
	Port              int `yaml:"port"`
	Host              string `yaml:"host"`
	DefaultTimeoutSec int `yaml:"default_timeout_sec"` // Default query timeout (can be overridden per-query or per-request)
	MaxTimeoutSec     int `yaml:"max_timeout_sec"`     // Maximum allowed timeout (caps request overrides)
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

type QueryConfig struct {
	Name        string        `yaml:"name"`
	Path        string        `yaml:"path"`
	Method      string        `yaml:"method"`
	Description string        `yaml:"description"`
	SQL         string        `yaml:"sql"`
	Parameters  []ParamConfig `yaml:"parameters"`
	TimeoutSec  int           `yaml:"timeout_sec"` // Query-specific default timeout (0 = use server default)
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

	// Set defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.DefaultTimeoutSec == 0 {
		cfg.Server.DefaultTimeoutSec = 30
	}
	if cfg.Server.MaxTimeoutSec == 0 {
		cfg.Server.MaxTimeoutSec = 300 // 5 minutes max
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 1433
	}

	// Logging defaults
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.MaxSizeMB == 0 {
		cfg.Logging.MaxSizeMB = 100
	}
	if cfg.Logging.MaxBackups == 0 {
		cfg.Logging.MaxBackups = 5
	}
	if cfg.Logging.MaxAgeDays == 0 {
		cfg.Logging.MaxAgeDays = 30
	}

	// Metrics defaults
	if cfg.Metrics.IntervalSec == 0 {
		cfg.Metrics.IntervalSec = 300 // 5 minutes
	}
	if cfg.Metrics.RetainFiles == 0 {
		cfg.Metrics.RetainFiles = 288 // 24 hours at 5-min intervals
	}

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database user is required")
	}
	if c.Database.Database == "" {
		return fmt.Errorf("database name is required")
	}

	for i, q := range c.Queries {
		if q.Name == "" {
			return fmt.Errorf("query %d: name is required", i)
		}
		if q.Path == "" {
			return fmt.Errorf("query %s: path is required", q.Name)
		}
		if q.SQL == "" {
			return fmt.Errorf("query %s: sql is required", q.Name)
		}
		if q.Method == "" {
			c.Queries[i].Method = "GET"
		}
	}

	return nil
}
