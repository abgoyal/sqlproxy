// Package testutil provides testing utilities for sql-proxy tests.
package testutil

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

// TestDB wraps a SQLite in-memory database for testing
type TestDB struct {
	DB   *sql.DB
	Name string
}

// NewTestDB creates a new in-memory SQLite database with test schema
func NewTestDB(t *testing.T) *TestDB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Apply pragmas for testing
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatalf("failed to apply pragma %s: %v", pragma, err)
		}
	}

	tdb := &TestDB{DB: db, Name: "test_db"}
	tdb.createSchema(t)
	return tdb
}

// createSchema creates the test schema
func (tdb *TestDB) createSchema(t *testing.T) {
	t.Helper()

	schema := `
		-- Users table
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			email TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Machines table (mimics biometric machines)
		CREATE TABLE machines (
			machine_id INTEGER PRIMARY KEY AUTOINCREMENT,
			machine_name TEXT NOT NULL,
			machine_ip TEXT,
			location TEXT,
			serial_number TEXT,
			is_online INTEGER DEFAULT 1,
			last_ping_time DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Attendance logs table
		CREATE TABLE attendance_log (
			log_id INTEGER PRIMARY KEY AUTOINCREMENT,
			employee_id TEXT NOT NULL,
			machine_id INTEGER NOT NULL,
			punch_time DATETIME NOT NULL,
			punch_type TEXT NOT NULL,
			FOREIGN KEY (machine_id) REFERENCES machines(machine_id)
		);

		-- Products table (for general testing)
		CREATE TABLE products (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT,
			price REAL NOT NULL,
			quantity INTEGER DEFAULT 0,
			category TEXT,
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Orders table (for join testing)
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			product_id INTEGER NOT NULL,
			quantity INTEGER NOT NULL,
			total_price REAL NOT NULL,
			status TEXT DEFAULT 'pending',
			order_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (product_id) REFERENCES products(id)
		);

		-- Settings table (key-value store)
		CREATE TABLE settings (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Large data table (for performance testing)
		CREATE TABLE large_data (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			data TEXT,
			number_value INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Create indexes
		CREATE INDEX idx_attendance_employee ON attendance_log(employee_id);
		CREATE INDEX idx_attendance_time ON attendance_log(punch_time);
		CREATE INDEX idx_machines_online ON machines(is_online);
		CREATE INDEX idx_products_category ON products(category);
		CREATE INDEX idx_orders_user ON orders(user_id);
		CREATE INDEX idx_orders_date ON orders(order_date);
	`

	if _, err := tdb.DB.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
}

// SeedTestData populates the database with test data
func (tdb *TestDB) SeedTestData(t *testing.T) {
	t.Helper()

	// Seed users
	users := []struct {
		username, email, status string
	}{
		{"alice", "alice@example.com", "active"},
		{"bob", "bob@example.com", "active"},
		{"charlie", "charlie@example.com", "inactive"},
		{"diana", "diana@example.com", "active"},
		{"eve", "eve@example.com", "suspended"},
	}
	for _, u := range users {
		_, err := tdb.DB.Exec(
			"INSERT INTO users (username, email, status) VALUES (?, ?, ?)",
			u.username, u.email, u.status,
		)
		if err != nil {
			t.Fatalf("failed to insert user %s: %v", u.username, err)
		}
	}

	// Seed machines
	machines := []struct {
		name, ip, location, serial string
		isOnline                   int
	}{
		{"Entrance-A", "192.168.1.10", "Main Entrance", "SN001", 1},
		{"Entrance-B", "192.168.1.11", "Side Entrance", "SN002", 1},
		{"Floor-1", "192.168.1.20", "First Floor", "SN003", 1},
		{"Floor-2", "192.168.1.21", "Second Floor", "SN004", 0},
		{"Cafeteria", "192.168.1.30", "Cafeteria", "SN005", 1},
	}
	for _, m := range machines {
		_, err := tdb.DB.Exec(
			"INSERT INTO machines (machine_name, machine_ip, location, serial_number, is_online) VALUES (?, ?, ?, ?, ?)",
			m.name, m.ip, m.location, m.serial, m.isOnline,
		)
		if err != nil {
			t.Fatalf("failed to insert machine %s: %v", m.name, err)
		}
	}

	// Seed attendance logs
	attendanceLogs := []struct {
		employeeID string
		machineID  int
		punchTime  string
		punchType  string
	}{
		{"EMP001", 1, "2024-01-15 08:00:00", "IN"},
		{"EMP001", 1, "2024-01-15 17:30:00", "OUT"},
		{"EMP002", 1, "2024-01-15 08:15:00", "IN"},
		{"EMP002", 2, "2024-01-15 17:00:00", "OUT"},
		{"EMP003", 3, "2024-01-15 09:00:00", "IN"},
		{"EMP003", 3, "2024-01-15 18:00:00", "OUT"},
		{"EMP001", 1, "2024-01-16 08:05:00", "IN"},
		{"EMP001", 1, "2024-01-16 17:45:00", "OUT"},
	}
	for _, a := range attendanceLogs {
		_, err := tdb.DB.Exec(
			"INSERT INTO attendance_log (employee_id, machine_id, punch_time, punch_type) VALUES (?, ?, ?, ?)",
			a.employeeID, a.machineID, a.punchTime, a.punchType,
		)
		if err != nil {
			t.Fatalf("failed to insert attendance log: %v", err)
		}
	}

	// Seed products
	products := []struct {
		name, desc, category string
		price                float64
		qty                  int
		active               int
	}{
		{"Widget A", "Basic widget", "widgets", 9.99, 100, 1},
		{"Widget B", "Premium widget", "widgets", 19.99, 50, 1},
		{"Gadget X", "Cool gadget", "gadgets", 49.99, 25, 1},
		{"Gadget Y", "Cooler gadget", "gadgets", 79.99, 10, 1},
		{"Discontinued Item", "Old product", "legacy", 5.99, 0, 0},
	}
	for _, p := range products {
		_, err := tdb.DB.Exec(
			"INSERT INTO products (name, description, category, price, quantity, is_active) VALUES (?, ?, ?, ?, ?, ?)",
			p.name, p.desc, p.category, p.price, p.qty, p.active,
		)
		if err != nil {
			t.Fatalf("failed to insert product %s: %v", p.name, err)
		}
	}

	// Seed orders
	orders := []struct {
		userID, productID, qty int
		total                  float64
		status                 string
	}{
		{1, 1, 2, 19.98, "completed"},
		{1, 3, 1, 49.99, "completed"},
		{2, 2, 1, 19.99, "pending"},
		{3, 1, 5, 49.95, "shipped"},
		{4, 4, 1, 79.99, "pending"},
	}
	for _, o := range orders {
		_, err := tdb.DB.Exec(
			"INSERT INTO orders (user_id, product_id, quantity, total_price, status) VALUES (?, ?, ?, ?, ?)",
			o.userID, o.productID, o.qty, o.total, o.status,
		)
		if err != nil {
			t.Fatalf("failed to insert order: %v", err)
		}
	}

	// Seed settings
	settings := []struct {
		key, value string
	}{
		{"app_name", "SQL Proxy"},
		{"version", "1.0.0"},
		{"max_connections", "100"},
		{"timeout_sec", "30"},
	}
	for _, s := range settings {
		_, err := tdb.DB.Exec(
			"INSERT INTO settings (key, value) VALUES (?, ?)",
			s.key, s.value,
		)
		if err != nil {
			t.Fatalf("failed to insert setting %s: %v", s.key, err)
		}
	}
}

// SeedLargeData populates the large_data table with n rows for performance testing
func (tdb *TestDB) SeedLargeData(t *testing.T, n int) {
	t.Helper()

	tx, err := tdb.DB.Begin()
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO large_data (data, number_value) VALUES (?, ?)")
	if err != nil {
		t.Fatalf("failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	for i := 0; i < n; i++ {
		data := fmt.Sprintf("test data row %d with some additional text to make it realistic", i)
		if _, err := stmt.Exec(data, i); err != nil {
			t.Fatalf("failed to insert row %d: %v", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}
}

// Close closes the test database
func (tdb *TestDB) Close() error {
	return tdb.DB.Close()
}

// Exec executes a query and returns the result
func (tdb *TestDB) Exec(query string, args ...any) (sql.Result, error) {
	return tdb.DB.Exec(query, args...)
}

// Query executes a query and returns rows
func (tdb *TestDB) Query(query string, args ...any) (*sql.Rows, error) {
	return tdb.DB.Query(query, args...)
}

// QueryRow executes a query that returns a single row
func (tdb *TestDB) QueryRow(query string, args ...any) *sql.Row {
	return tdb.DB.QueryRow(query, args...)
}

// Count returns the number of rows in a table
func (tdb *TestDB) Count(t *testing.T, table string) int {
	t.Helper()
	var count int
	err := tdb.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count rows in %s: %v", table, err)
	}
	return count
}

// Truncate clears all data from a table
func (tdb *TestDB) Truncate(t *testing.T, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if _, err := tdb.DB.Exec(fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			t.Fatalf("failed to truncate %s: %v", table, err)
		}
	}
}

// InsertUser inserts a user and returns the ID
func (tdb *TestDB) InsertUser(t *testing.T, username, email, status string) int64 {
	t.Helper()
	result, err := tdb.DB.Exec(
		"INSERT INTO users (username, email, status) VALUES (?, ?, ?)",
		username, email, status,
	)
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}

// InsertMachine inserts a machine and returns the ID
func (tdb *TestDB) InsertMachine(t *testing.T, name, ip, location string, online bool) int64 {
	t.Helper()
	isOnline := 0
	if online {
		isOnline = 1
	}
	result, err := tdb.DB.Exec(
		"INSERT INTO machines (machine_name, machine_ip, location, is_online) VALUES (?, ?, ?, ?)",
		name, ip, location, isOnline,
	)
	if err != nil {
		t.Fatalf("failed to insert machine: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}
