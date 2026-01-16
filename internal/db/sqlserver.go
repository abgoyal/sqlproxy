package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"sql-proxy/internal/config"
)

const (
	// sqlserverMaxOpenConns is the max open connections
	sqlserverMaxOpenConns = 5

	// sqlserverMaxIdleConns is the max idle connections to keep
	sqlserverMaxIdleConns = 2

	// sqlserverConnMaxLifetime is how long connections can be reused
	sqlserverConnMaxLifetime = 5 * time.Minute

	// sqlserverConnMaxIdleTime is how long idle connections are kept
	sqlserverConnMaxIdleTime = 2 * time.Minute

	// sqlserverConnectionTimeout is the timeout for connection establishment (seconds)
	sqlserverConnectionTimeout = 10

	// sqlserverPingTimeout is the timeout for ping operations
	sqlserverPingTimeout = 10 * time.Second
)

// SQLServerDriver implements Driver for Microsoft SQL Server
type SQLServerDriver struct {
	conn     *sql.DB
	connStr  string
	cfg      config.DatabaseConfig
	readOnly bool
}

// NewSQLServerDriver creates a new SQL Server driver
func NewSQLServerDriver(cfg config.DatabaseConfig) (*SQLServerDriver, error) {
	readOnly := cfg.IsReadOnly()

	// Resolve encrypt setting (default: disable for internal VPC connections)
	encrypt := cfg.Encrypt
	if encrypt == "" {
		encrypt = "disable"
	}

	// Build connection string
	var connStr string
	if readOnly {
		// Read-only: Add ApplicationIntent=ReadOnly for AG routing and read-only mode
		connStr = fmt.Sprintf(
			"server=%s;port=%d;user id=%s;password=%s;database=%s;encrypt=%s;connection timeout=%d;ApplicationIntent=ReadOnly",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, encrypt, sqlserverConnectionTimeout,
		)
	} else {
		// Write-enabled: No ApplicationIntent restriction
		connStr = fmt.Sprintf(
			"server=%s;port=%d;user id=%s;password=%s;database=%s;encrypt=%s;connection timeout=%d",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, encrypt, sqlserverConnectionTimeout,
		)
	}

	conn, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Conservative connection pool to minimize footprint on SQL Server
	configureSQLServerPool(conn, cfg)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), sqlserverPingTimeout)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &SQLServerDriver{conn: conn, connStr: connStr, cfg: cfg, readOnly: readOnly}, nil
}

func configureSQLServerPool(conn *sql.DB, cfg config.DatabaseConfig) {
	maxOpen := sqlserverMaxOpenConns
	if cfg.MaxOpenConns != nil {
		maxOpen = *cfg.MaxOpenConns
	}
	maxIdle := sqlserverMaxIdleConns
	if cfg.MaxIdleConns != nil {
		maxIdle = *cfg.MaxIdleConns
	}
	maxLifetime := sqlserverConnMaxLifetime
	if cfg.ConnMaxLifetime != nil {
		maxLifetime = time.Duration(*cfg.ConnMaxLifetime) * time.Second
	}
	maxIdleTime := sqlserverConnMaxIdleTime
	if cfg.ConnMaxIdleTime != nil {
		maxIdleTime = time.Duration(*cfg.ConnMaxIdleTime) * time.Second
	}

	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(maxIdle)
	conn.SetConnMaxLifetime(maxLifetime)
	conn.SetConnMaxIdleTime(maxIdleTime)
}

// Name returns the connection name
func (d *SQLServerDriver) Name() string {
	return d.cfg.Name
}

// Type returns the database type
func (d *SQLServerDriver) Type() string {
	return "sqlserver"
}

// IsReadOnly returns whether this connection is read-only
func (d *SQLServerDriver) IsReadOnly() bool {
	return d.readOnly
}

// Config returns the database configuration
func (d *SQLServerDriver) Config() config.DatabaseConfig {
	return d.cfg
}

// Reconnect attempts to re-establish the database connection
func (d *SQLServerDriver) Reconnect() error {
	// Close existing connection (ignore errors)
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}

	conn, err := sql.Open("sqlserver", d.connStr)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}

	configureSQLServerPool(conn, d.cfg)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), sqlserverPingTimeout)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	d.conn = conn
	return nil
}

func (d *SQLServerDriver) Close() error {
	return d.conn.Close()
}

// configureSession sets SQL Server session options based on the provided session config.
func (d *SQLServerDriver) configureSession(ctx context.Context, conn *sql.Conn, sessCfg config.SessionConfig) error {
	isolationSQL := isolationToSQL(sessCfg.Isolation)
	deadlockSQL := deadlockPriorityToSQL(sessCfg.DeadlockPriority)

	sessionSQL := fmt.Sprintf(`
		SET TRANSACTION ISOLATION LEVEL %s;
		SET LOCK_TIMEOUT %d;
		SET DEADLOCK_PRIORITY %s;
		SET NOCOUNT ON;
		SET IMPLICIT_TRANSACTIONS OFF;
		SET ARITHABORT ON;
	`, isolationSQL, sessCfg.LockTimeoutMs, deadlockSQL)

	_, err := conn.ExecContext(ctx, sessionSQL)
	return err
}

// isolationToSQL converts config isolation level to SQL Server syntax
func isolationToSQL(isolation string) string {
	switch isolation {
	case "read_uncommitted":
		return "READ UNCOMMITTED"
	case "read_committed":
		return "READ COMMITTED"
	case "repeatable_read":
		return "REPEATABLE READ"
	case "serializable":
		return "SERIALIZABLE"
	case "snapshot":
		return "SNAPSHOT"
	default:
		return "READ COMMITTED"
	}
}

// deadlockPriorityToSQL converts config deadlock priority to SQL Server syntax
func deadlockPriorityToSQL(priority string) string {
	switch priority {
	case "low":
		return "LOW"
	case "normal":
		return "NORMAL"
	case "high":
		return "HIGH"
	default:
		return "LOW"
	}
}

// Query executes a SQL query and returns results.
// SQL uses @param syntax which is native to SQL Server.
// params is a map of parameter name -> value.
// For SELECT queries, returns rows in QueryResult.Rows.
// For INSERT/UPDATE/DELETE, returns affected count in QueryResult.RowsAffected.
func (d *SQLServerDriver) Query(ctx context.Context, sessCfg config.SessionConfig, query string, params map[string]any) (*QueryResult, error) {
	// Get a dedicated connection from the pool
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() // Returns to pool

	// Configure session with the provided settings
	if err := d.configureSession(ctx, conn, sessCfg); err != nil {
		return nil, fmt.Errorf("failed to configure session: %w", err)
	}

	// Build args from params map using sql.Named
	// Find @params in SQL to maintain order
	args := d.buildArgs(query, params)

	// Use ExecContext for write operations, QueryContext for reads
	if IsWriteQuery(query) {
		result, err := conn.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("exec failed: %w", err)
		}
		rowsAffected, _ := result.RowsAffected()
		return &QueryResult{RowsAffected: rowsAffected}, nil
	}

	// Execute the query
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	scannedRows, err := ScanRows(rows)
	if err != nil {
		return nil, err
	}
	return &QueryResult{Rows: scannedRows}, nil
}

// buildArgs builds sql.Named arguments from the params map.
// SQL Server uses @param syntax natively, so we just need to convert
// the map to sql.Named arguments.
func (d *SQLServerDriver) buildArgs(query string, params map[string]any) []any {
	matches := ParamRegex.FindAllStringSubmatch(query, -1)

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

	return args
}

// Ping checks database connectivity
func (d *SQLServerDriver) Ping(ctx context.Context) error {
	return d.conn.PingContext(ctx)
}
