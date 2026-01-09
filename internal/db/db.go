package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"sql-proxy/internal/config"
)

type DB struct {
	conn     *sql.DB
	connStr  string
	cfg      config.DatabaseConfig
	readOnly bool
}

// Name returns the connection name
func (d *DB) Name() string {
	return d.cfg.Name
}

// IsReadOnly returns whether this connection is read-only
func (d *DB) IsReadOnly() bool {
	return d.readOnly
}

func New(cfg config.DatabaseConfig) (*DB, error) {
	readOnly := cfg.IsReadOnly()

	// Build connection string
	// - connection timeout=10: Fail fast on connection issues
	// - encrypt=disable: For RDS internal VPC (change to 'true' if needed)
	var connStr string
	if readOnly {
		// Read-only: Add ApplicationIntent=ReadOnly for AG routing and read-only mode
		connStr = fmt.Sprintf(
			"server=%s;port=%d;user id=%s;password=%s;database=%s;encrypt=disable;connection timeout=10;ApplicationIntent=ReadOnly",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database,
		)
	} else {
		// Write-enabled: No ApplicationIntent restriction
		connStr = fmt.Sprintf(
			"server=%s;port=%d;user id=%s;password=%s;database=%s;encrypt=disable;connection timeout=10",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database,
		)
	}

	conn, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Conservative connection pool to minimize footprint on SQL Server
	configurePool(conn)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{conn: conn, connStr: connStr, cfg: cfg, readOnly: readOnly}, nil
}

func configurePool(conn *sql.DB) {
	conn.SetMaxOpenConns(5)                   // Low max connections - we're just a read proxy
	conn.SetMaxIdleConns(2)                   // Keep few idle connections
	conn.SetConnMaxLifetime(5 * time.Minute)  // Recycle connections regularly
	conn.SetConnMaxIdleTime(2 * time.Minute)  // Don't hold idle connections long
}

// Reconnect attempts to re-establish the database connection
func (d *DB) Reconnect() error {
	// Close existing connection (ignore errors)
	if d.conn != nil {
		d.conn.Close()
	}

	conn, err := sql.Open("sqlserver", d.connStr)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}

	configurePool(conn)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	d.conn = conn
	return nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

// configureSession sets SQL Server session options for safe database access.
// This is called at the start of each query to ensure the session is configured
// correctly (connection pooling may give us a recycled connection).
//
// For read-only connections:
//   - READ UNCOMMITTED isolation (no shared locks, won't block writers)
//   - All other safety measures
//
// For write-enabled connections:
//   - Default isolation level (READ COMMITTED)
//   - Still keeps timeouts and deadlock priority for safety
func (d *DB) configureSession(ctx context.Context, conn *sql.Conn) error {
	var sessionConfig string

	if d.readOnly {
		// Full safety for read-only connections:
		//
		// READ UNCOMMITTED (NOLOCK equivalent at session level):
		//   - No shared locks acquired on reads
		//   - Won't block writers, writers won't block us
		//   - May read uncommitted data (dirty reads) - acceptable for monitoring
		//
		// LOCK_TIMEOUT 5000 (5 seconds):
		//   - If we somehow need a lock, fail fast instead of waiting
		//   - Prevents us from contributing to blocking chains
		//
		// DEADLOCK_PRIORITY LOW:
		//   - If somehow in a deadlock, we volunteer to be the victim
		//   - Protects the production application's transactions
		//
		// NOCOUNT ON:
		//   - Suppresses "N rows affected" messages
		//   - Reduces network traffic
		//
		// IMPLICIT_TRANSACTIONS OFF:
		//   - Ensures no implicit transaction starts
		//   - Each query is auto-committed (for reads, this is a no-op)
		//
		// ARITHABORT ON:
		//   - Standard setting, required for indexed view access
		//
		sessionConfig = `
			SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED;
			SET LOCK_TIMEOUT 5000;
			SET DEADLOCK_PRIORITY LOW;
			SET NOCOUNT ON;
			SET IMPLICIT_TRANSACTIONS OFF;
			SET ARITHABORT ON;
		`
	} else {
		// Write-enabled: Use default isolation (READ COMMITTED) but keep safety measures
		//
		// LOCK_TIMEOUT 5000: Fail fast instead of blocking
		// DEADLOCK_PRIORITY LOW: Yield to production app in deadlocks
		// NOCOUNT ON: Reduce network traffic
		// IMPLICIT_TRANSACTIONS OFF: No accidental open transactions
		// ARITHABORT ON: Required for indexed views
		//
		sessionConfig = `
			SET LOCK_TIMEOUT 5000;
			SET DEADLOCK_PRIORITY LOW;
			SET NOCOUNT ON;
			SET IMPLICIT_TRANSACTIONS OFF;
			SET ARITHABORT ON;
		`
	}

	_, err := conn.ExecContext(ctx, sessionConfig)
	return err
}

// Query executes a SQL query and returns results as a slice of maps.
// Session is configured based on connection's read-only mode.
func (d *DB) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	// Get a dedicated connection from the pool
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() // Returns to pool

	// Configure session for safe read-only access
	if err := d.configureSession(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to configure session: %w", err)
	}

	// Execute the query
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any

	for rows.Next() {
		// Create a slice of interface{} to hold the values
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert to map
		row := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			// Handle specific types for JSON serialization
			switch v := val.(type) {
			case []byte:
				row[col] = string(v)
			case time.Time:
				row[col] = v.Format(time.RFC3339)
			default:
				row[col] = v
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// Ping checks database connectivity
func (d *DB) Ping(ctx context.Context) error {
	return d.conn.PingContext(ctx)
}
