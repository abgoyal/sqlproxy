package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"sql-proxy/internal/config"
)

const (
	// mysqlMaxOpenConns is the max open connections
	mysqlMaxOpenConns = 5

	// mysqlMaxIdleConns is the max idle connections to keep
	mysqlMaxIdleConns = 2

	// mysqlConnMaxLifetime is how long connections can be reused
	mysqlConnMaxLifetime = 5 * time.Minute

	// mysqlConnMaxIdleTime is how long idle connections are kept
	mysqlConnMaxIdleTime = 2 * time.Minute

	// mysqlConnectionTimeout is the timeout for connection establishment (seconds)
	mysqlConnectionTimeout = 10

	// mysqlPingTimeout is the timeout for ping operations
	mysqlPingTimeout = 10 * time.Second

	// mysqlDefaultLockTimeout is the default lock wait timeout in seconds
	mysqlDefaultLockTimeout = 5
)

// MySQLDriver implements Driver for MySQL
type MySQLDriver struct {
	conn     *sql.DB
	dsn      string
	cfg      config.DatabaseConfig
	readOnly bool
}

// buildMySQLDSN constructs the DSN string for go-sql-driver/mysql.
func buildMySQLDSN(cfg config.DatabaseConfig) string {
	port := cfg.Port
	if port == 0 {
		port = 3306
	}

	// TLS configuration
	tlsMode := "false"
	if cfg.Encrypt != "" {
		switch cfg.Encrypt {
		case "false", "disable":
			tlsMode = "false"
		default:
			tlsMode = cfg.Encrypt
		}
	}

	timeout := time.Duration(mysqlConnectionTimeout) * time.Second

	mysqlCfg := mysql.Config{
		User:                 cfg.User,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%d", cfg.Host, port),
		DBName:               cfg.Database,
		ParseTime:            true,
		Timeout:              timeout,
		ReadTimeout:          timeout,
		WriteTimeout:         timeout,
		TLSConfig:            tlsMode,
		InterpolateParams:    false,
		Collation:            "utf8mb4_general_ci",
		AllowNativePasswords: true,
	}

	return mysqlCfg.FormatDSN()
}

// NewMySQLDriver creates a new MySQL driver
func NewMySQLDriver(cfg config.DatabaseConfig) (*MySQLDriver, error) {
	readOnly := cfg.IsReadOnly()

	dsn := buildMySQLDSN(cfg)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql database: %w", err)
	}

	// Configure connection pool
	configureMySQLPool(conn, cfg)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), mysqlPingTimeout)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to ping mysql database: %w", err)
	}

	return &MySQLDriver{conn: conn, dsn: dsn, cfg: cfg, readOnly: readOnly}, nil
}

func configureMySQLPool(conn *sql.DB, cfg config.DatabaseConfig) {
	maxOpen := mysqlMaxOpenConns
	if cfg.MaxOpenConns != nil {
		maxOpen = *cfg.MaxOpenConns
	}
	maxIdle := mysqlMaxIdleConns
	if cfg.MaxIdleConns != nil {
		maxIdle = *cfg.MaxIdleConns
	}
	maxLifetime := mysqlConnMaxLifetime
	if cfg.ConnMaxLifetime != nil {
		maxLifetime = time.Duration(*cfg.ConnMaxLifetime) * time.Second
	}
	maxIdleTime := mysqlConnMaxIdleTime
	if cfg.ConnMaxIdleTime != nil {
		maxIdleTime = time.Duration(*cfg.ConnMaxIdleTime) * time.Second
	}

	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(maxIdle)
	conn.SetConnMaxLifetime(maxLifetime)
	conn.SetConnMaxIdleTime(maxIdleTime)
}

// Name returns the connection name
func (d *MySQLDriver) Name() string {
	return d.cfg.Name
}

// Type returns the database type
func (d *MySQLDriver) Type() string {
	return "mysql"
}

// IsReadOnly returns whether this connection is read-only
func (d *MySQLDriver) IsReadOnly() bool {
	return d.readOnly
}

// Config returns the database configuration
func (d *MySQLDriver) Config() config.DatabaseConfig {
	return d.cfg
}

// Reconnect attempts to re-establish the database connection
func (d *MySQLDriver) Reconnect() error {
	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
	}

	conn, err := sql.Open("mysql", d.dsn)
	if err != nil {
		return fmt.Errorf("failed to open mysql database: %w", err)
	}

	configureMySQLPool(conn, d.cfg)

	ctx, cancel := context.WithTimeout(context.Background(), mysqlPingTimeout)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to ping mysql database: %w", err)
	}

	d.conn = conn
	return nil
}

func (d *MySQLDriver) Close() error {
	return d.conn.Close()
}

// configureSession sets MySQL session options based on the provided session config.
func (d *MySQLDriver) configureSession(ctx context.Context, conn *sql.Conn, sessCfg config.SessionConfig) error {
	isolationSQL := mysqlIsolationToSQL(sessCfg.Isolation)

	// Lock wait timeout: ceiling division from ms to seconds (minimum 1s)
	lockTimeoutSec := (sessCfg.LockTimeoutMs + 999) / 1000
	if lockTimeoutSec < 1 {
		lockTimeoutSec = mysqlDefaultLockTimeout
	}

	_, err := conn.ExecContext(ctx, fmt.Sprintf("SET SESSION TRANSACTION ISOLATION LEVEL %s", isolationSQL))
	if err != nil {
		return fmt.Errorf("failed to set isolation level: %w", err)
	}

	_, err = conn.ExecContext(ctx, fmt.Sprintf("SET SESSION innodb_lock_wait_timeout = %d", lockTimeoutSec))
	if err != nil {
		return fmt.Errorf("failed to set lock timeout: %w", err)
	}

	if d.readOnly {
		_, err = conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY")
		if err != nil {
			return fmt.Errorf("failed to set read only mode: %w", err)
		}
	}

	return nil
}

// mysqlIsolationToSQL converts config isolation level to MySQL syntax
func mysqlIsolationToSQL(isolation string) string {
	switch isolation {
	case "read_uncommitted":
		return "READ UNCOMMITTED"
	case "read_committed":
		return "READ COMMITTED"
	case "repeatable_read":
		return "REPEATABLE READ"
	case "serializable":
		return "SERIALIZABLE"
	default:
		return "READ COMMITTED"
	}
}

// Query executes a SQL query and returns results.
// SQL uses @param syntax which is translated to ? positional placeholders for MySQL.
// params is a map of parameter name -> value.
// hints carries precomputed SQL classification; falls back to parsing if nil.
// For SELECT queries, returns rows in QueryResult.Rows.
// For INSERT/UPDATE/DELETE, returns affected count in QueryResult.RowsAffected.
func (d *MySQLDriver) Query(ctx context.Context, sessCfg config.SessionConfig, query string, params map[string]any, hints *QueryHints) (*QueryResult, error) {
	// Get a dedicated connection from the pool
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Configure session with the provided settings
	if err := d.configureSession(ctx, conn, sessCfg); err != nil {
		return nil, fmt.Errorf("failed to configure session: %w", err)
	}

	// Translate @param to ? and build positional args
	translatedQuery, args := d.translateQuery(query, params)

	// Resolve SQL classification from hints or by parsing
	isWrite := resolveIsWrite(hints, query)
	hasReturning := resolveHasReturning(hints, query)

	// Route: writes without RETURNING use ExecContext (for RowsAffected),
	// everything else uses QueryContext (SELECTs and writes with RETURNING).
	if isWrite && !hasReturning {
		result, err := conn.ExecContext(ctx, translatedQuery, args...)
		if err != nil {
			return nil, fmt.Errorf("exec failed: %w", err)
		}
		rowsAffected, _ := result.RowsAffected()
		return &QueryResult{RowsAffected: rowsAffected}, nil
	}

	rows, err := conn.QueryContext(ctx, translatedQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	scannedRows, err := ScanRows(rows)
	if err != nil {
		return nil, err
	}
	qr := &QueryResult{Rows: scannedRows}
	if isWrite {
		qr.RowsAffected = int64(len(scannedRows))
	}
	return qr, nil
}

// translateQuery converts @param syntax to ? positional placeholders for MySQL.
// Each occurrence of @paramName is replaced with ? and the corresponding value
// is appended to the args slice in order of appearance.
func (d *MySQLDriver) translateQuery(query string, params map[string]any) (string, []any) {
	matches := ParamRegex.FindAllStringSubmatchIndex(query, -1)
	if len(matches) == 0 {
		return query, nil
	}

	var args []any
	var translated strings.Builder
	lastIndex := 0

	for _, match := range matches {
		// match[0]:match[1] is the full @param match
		// match[2]:match[3] is the param name (capture group)
		paramName := query[match[2]:match[3]]

		// Write everything before this match
		translated.WriteString(query[lastIndex:match[0]])
		// Replace @param with ?
		translated.WriteString("?")

		// Append the value (nil if not present in params)
		value := params[paramName]
		args = append(args, value)

		lastIndex = match[1]
	}

	// Write any remaining query after the last match
	translated.WriteString(query[lastIndex:])

	return translated.String(), args
}

// Ping checks database connectivity
func (d *MySQLDriver) Ping(ctx context.Context) error {
	return d.conn.PingContext(ctx)
}

func (d *MySQLDriver) PoolStats() PoolStats {
	if d.conn == nil {
		return PoolStats{}
	}
	s := d.conn.Stats()
	return PoolStats{OpenConnections: s.OpenConnections, IdleConnections: s.Idle}
}
