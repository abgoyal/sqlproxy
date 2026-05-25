package db

import (
	"context"
	"fmt"

	"sql-proxy/internal/config"
)

// QueryHints carries precomputed SQL classification so drivers can skip re-parsing.
// When nil pointers are provided, drivers fall back to runtime parsing.
type QueryHints struct {
	IsWrite      *bool // SQL is INSERT/UPDATE/DELETE/etc.
	HasReturning *bool // SQL has OUTPUT INSERTED/DELETED or RETURNING
}

// PoolStats contains connection pool statistics.
type PoolStats struct {
	OpenConnections int
	IdleConnections int
}

// Driver is the interface all database implementations must satisfy.
type Driver interface {
	Query(ctx context.Context, sessCfg config.SessionConfig, query string, params map[string]any, hints *QueryHints) (*QueryResult, error)
	Ping(ctx context.Context) error
	Close() error
	Reconnect() error
	Name() string
	Type() string
	IsReadOnly() bool
	Config() config.DatabaseConfig
	PoolStats() PoolStats
}

// NewDriver creates a database driver based on the config type.
// This is the factory function that returns the appropriate driver implementation.
func NewDriver(cfg config.DatabaseConfig) (Driver, error) {
	switch cfg.Type {
	case "sqlserver":
		return NewSQLServerDriver(cfg)
	case "sqlite":
		return NewSQLiteDriver(cfg)
	case "mysql":
		return NewMySQLDriver(cfg)
	case "postgres":
		return nil, fmt.Errorf("postgres support not yet implemented")
	default:
		return nil, fmt.Errorf("unknown database type: %s", cfg.Type)
	}
}
