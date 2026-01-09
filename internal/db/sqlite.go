package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"sql-proxy/internal/config"
)

// SQLiteDriver implements Driver for SQLite
type SQLiteDriver struct {
	conn     *sql.DB
	path     string
	cfg      config.DatabaseConfig
	readOnly bool
}

// NewSQLiteDriver creates a new SQLite driver
func NewSQLiteDriver(cfg config.DatabaseConfig) (*SQLiteDriver, error) {
	readOnly := cfg.IsReadOnly()

	// Build connection string (DSN)
	// SQLite uses file path as connection string
	dsn := cfg.Path
	if dsn == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}

	// Add DSN parameters for better concurrency
	// - _txlock=immediate: Acquire write lock at transaction start, prevents deadlocks
	// - mode=ro: Read-only mode (for readonly connections, non-memory DBs)
	var params []string
	if !readOnly {
		// For write connections, use immediate locking to prevent deadlocks
		params = append(params, "_txlock=immediate")
	}
	if readOnly && dsn != ":memory:" {
		params = append(params, "mode=ro")
	}

	if len(params) > 0 {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn += separator + strings.Join(params, "&")
	}

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Configure connection pool (SQLite is single-writer, so keep it conservative)
	configureSQLitePool(conn)

	// Apply initial PRAGMA settings
	driver := &SQLiteDriver{
		conn:     conn,
		path:     cfg.Path,
		cfg:      cfg,
		readOnly: readOnly,
	}

	if err := driver.applyInitialPragmas(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to apply initial pragmas: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	return driver, nil
}

func configureSQLitePool(conn *sql.DB) {
	// SQLite is single-writer, so we don't need many connections
	// WAL mode allows concurrent reads with single writer
	conn.SetMaxOpenConns(5)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxLifetime(5 * time.Minute)
	conn.SetConnMaxIdleTime(2 * time.Minute)
}

// applyInitialPragmas applies database-level pragmas that should be set once.
// These settings are critical for good concurrent performance and avoiding "database is locked" errors.
func (d *SQLiteDriver) applyInitialPragmas() error {
	// Resolve busy timeout (default: 5 seconds)
	busyTimeout := 5000
	if d.cfg.BusyTimeoutMs != nil {
		busyTimeout = *d.cfg.BusyTimeoutMs
	}

	// Set journal mode (default: WAL for better concurrency)
	journalMode := d.cfg.JournalMode
	if journalMode == "" {
		journalMode = "wal"
	}

	// Build pragmas - order matters for some settings
	pragmas := []string{
		// busy_timeout: How long to wait when database is locked (milliseconds)
		// This is THE most important setting for preventing "database is locked" errors
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout),

		// journal_mode: WAL allows concurrent reads while writing
		fmt.Sprintf("PRAGMA journal_mode = %s", journalMode),
	}

	// WAL-specific optimizations
	if strings.ToLower(journalMode) == "wal" {
		pragmas = append(pragmas,
			// synchronous=NORMAL: Safe for WAL mode, much faster than FULL
			"PRAGMA synchronous = NORMAL",

			// wal_autocheckpoint: Checkpoint every 1000 pages (default)
			// Keeps WAL file from growing too large
			"PRAGMA wal_autocheckpoint = 1000",
		)
	}

	// Additional performance/concurrency pragmas
	pragmas = append(pragmas,
		// temp_store=MEMORY: Store temp tables in memory (faster)
		"PRAGMA temp_store = MEMORY",

		// cache_size: Negative value = KB, positive = pages
		// -64000 = 64MB cache (helps with concurrent reads)
		"PRAGMA cache_size = -64000",

		// mmap_size: Memory-map up to 256MB of database file
		// Improves read performance significantly
		"PRAGMA mmap_size = 268435456",

		// foreign_keys: Enable foreign key enforcement (good practice)
		"PRAGMA foreign_keys = ON",
	)

	// Execute all pragmas
	for _, pragma := range pragmas {
		if _, err := d.conn.Exec(pragma); err != nil {
			return fmt.Errorf("failed to execute %s: %w", pragma, err)
		}
	}

	return nil
}

// Name returns the connection name
func (d *SQLiteDriver) Name() string {
	return d.cfg.Name
}

// Type returns the database type
func (d *SQLiteDriver) Type() string {
	return "sqlite"
}

// IsReadOnly returns whether this connection is read-only
func (d *SQLiteDriver) IsReadOnly() bool {
	return d.readOnly
}

// Config returns the database configuration
func (d *SQLiteDriver) Config() config.DatabaseConfig {
	return d.cfg
}

// Reconnect attempts to re-establish the database connection
func (d *SQLiteDriver) Reconnect() error {
	// Close existing connection (ignore errors)
	if d.conn != nil {
		d.conn.Close()
	}

	// Build connection string with DSN parameters
	dsn := d.path
	var params []string
	if !d.readOnly {
		params = append(params, "_txlock=immediate")
	}
	if d.readOnly && dsn != ":memory:" {
		params = append(params, "mode=ro")
	}

	if len(params) > 0 {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn += separator + strings.Join(params, "&")
	}

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open sqlite database: %w", err)
	}

	configureSQLitePool(conn)
	d.conn = conn

	if err := d.applyInitialPragmas(); err != nil {
		conn.Close()
		return fmt.Errorf("failed to apply initial pragmas: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	return nil
}

func (d *SQLiteDriver) Close() error {
	return d.conn.Close()
}

// configureSession sets SQLite session options via PRAGMA on the specific connection.
// Since SQLite pragmas are per-connection and we use connection pooling, we must set
// critical pragmas on each connection we get from the pool.
// Isolation and deadlock_priority are ignored (not applicable to SQLite).
func (d *SQLiteDriver) configureSession(ctx context.Context, conn *sql.Conn, sessCfg config.SessionConfig) error {
	// Resolve busy timeout (default: 5 seconds)
	busyTimeout := 5000
	if d.cfg.BusyTimeoutMs != nil {
		busyTimeout = *d.cfg.BusyTimeoutMs
	}

	// Resolve journal mode (default: WAL)
	journalMode := d.cfg.JournalMode
	if journalMode == "" {
		journalMode = "wal"
	}

	// Set critical pragmas on this connection
	// Note: journal_mode is database-level (persists), others are connection-level
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout),
		fmt.Sprintf("PRAGMA journal_mode = %s", journalMode),
		"PRAGMA foreign_keys = ON",
	}

	// WAL-specific settings
	if strings.ToLower(journalMode) == "wal" {
		pragmas = append(pragmas, "PRAGMA synchronous = NORMAL")
	}

	for _, pragma := range pragmas {
		if _, err := conn.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("failed to execute %s: %w", pragma, err)
		}
	}

	return nil
}

// Query executes a SQL query and returns results as a slice of maps.
// SQL uses @param syntax which is translated to $param for SQLite.
// params is a map of parameter name -> value.
func (d *SQLiteDriver) Query(ctx context.Context, sessCfg config.SessionConfig, query string, params map[string]any) ([]map[string]any, error) {
	// Get a dedicated connection from the pool
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() // Returns to pool

	// Configure session with pragmas
	if err := d.configureSession(ctx, conn, sessCfg); err != nil {
		return nil, fmt.Errorf("failed to configure session: %w", err)
	}

	// Translate @param to $param and build args
	translatedQuery, args := d.translateQuery(query, params)

	// Execute the query
	rows, err := conn.QueryContext(ctx, translatedQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	return scanRows(rows)
}

// translateQuery keeps @param syntax for SQLite and builds args.
// modernc.org/sqlite supports named parameters with @name syntax using sql.Named().
func (d *SQLiteDriver) translateQuery(query string, params map[string]any) (string, []any) {
	re := regexp.MustCompile(`@(\w+)`)
	matches := re.FindAllStringSubmatch(query, -1)

	// Keep @param syntax - SQLite driver supports it directly with sql.Named()
	// No translation needed

	// Build args using sql.Named for each unique parameter
	addedParams := make(map[string]bool)
	var args []any

	for _, match := range matches {
		paramName := match[1]
		if addedParams[paramName] {
			continue
		}

		value := params[paramName] // nil if not present
		args = append(args, sql.Named(paramName, value))
		addedParams[paramName] = true
	}

	return query, args
}

// Ping checks database connectivity
func (d *SQLiteDriver) Ping(ctx context.Context) error {
	return d.conn.PingContext(ctx)
}
