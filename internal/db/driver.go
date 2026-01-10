package db

import (
	"context"
	"fmt"

	"sql-proxy/internal/config"
)

// Driver is the interface all database implementations must satisfy.
// Each driver handles its own parameter translation from @param syntax
// to the native syntax of the database.
type Driver interface {
	// Query executes a query with named parameters.
	// SQL uses @param syntax; driver translates to native syntax.
	// params is a map of parameter name -> value.
	Query(ctx context.Context, sessCfg config.SessionConfig, query string, params map[string]any) ([]map[string]any, error)

	// Ping checks database connectivity
	Ping(ctx context.Context) error

	// Close closes the database connection
	Close() error

	// Reconnect re-establishes the connection
	Reconnect() error

	// Name returns the connection name
	Name() string

	// Type returns the database type (sqlserver or sqlite)
	Type() string

	// IsReadOnly returns whether this is a read-only connection
	IsReadOnly() bool

	// Config returns the database configuration
	Config() config.DatabaseConfig
}

// NewDriver creates a database driver based on the config type.
// This is the factory function that returns the appropriate driver implementation.
func NewDriver(cfg config.DatabaseConfig) (Driver, error) {
	switch cfg.Type {
	case "sqlserver", "":
		// Default to sqlserver for backwards compatibility
		return NewSQLServerDriver(cfg)
	case "sqlite":
		return NewSQLiteDriver(cfg)
	case "mysql":
		return nil, fmt.Errorf("mysql support not yet implemented")
	case "postgres":
		return nil, fmt.Errorf("postgres support not yet implemented")
	default:
		return nil, fmt.Errorf("unknown database type: %s", cfg.Type)
	}
}
