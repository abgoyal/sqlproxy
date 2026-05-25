package db

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"sql-proxy/internal/config"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestNewDriver_SQLite verifies factory creates SQLite driver with :memory: path
func TestNewDriver_SQLite(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "sqlite",
		Path: ":memory:",
	}

	driver, err := NewDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer func() { _ = driver.Close() }()

	if driver.Type() != "sqlite" {
		t.Errorf("expected type sqlite, got %s", driver.Type())
	}
	if driver.Name() != "test" {
		t.Errorf("expected name test, got %s", driver.Name())
	}
}

// TestNewDriver_SQLiteExplicit confirms returned driver is *SQLiteDriver type
func TestNewDriver_SQLiteExplicit(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "sqlite",
		Path: ":memory:",
	}

	driver, err := NewDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer func() { _ = driver.Close() }()

	// Verify it's actually a SQLite driver
	_, ok := driver.(*SQLiteDriver)
	if !ok {
		t.Error("expected *SQLiteDriver")
	}
}

// TestNewDriver_EmptyTypeReturnsError ensures empty type is rejected
func TestNewDriver_EmptyTypeReturnsError(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "", // Empty type should be rejected
	}

	_, err := NewDriver(cfg)
	if err == nil {
		t.Error("expected error for empty type")
	}
	if !strings.Contains(err.Error(), "unknown database type") {
		t.Errorf("expected 'unknown database type' error, got: %v", err)
	}
}

// TestNewDriver_MySQL verifies factory creates MySQL driver (requires running MySQL)
func TestNewDriver_MySQL(t *testing.T) {
	host := os.Getenv("MYSQL_HOST")
	if host == "" {
		t.Skip("MYSQL_HOST not set, skipping MySQL integration test")
	}

	port := 3306
	if p := os.Getenv("MYSQL_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}

	cfg := config.DatabaseConfig{
		Name:     "test",
		Type:     "mysql",
		Host:     host,
		Port:     port,
		User:     envOrDefault("MYSQL_USER", "root"),
		Password: envOrDefault("MYSQL_PASSWORD", "testpass"),
		Database: envOrDefault("MYSQL_DATABASE", "testdb"),
	}

	driver, err := NewDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer func() { _ = driver.Close() }()

	if driver.Type() != "mysql" {
		t.Errorf("expected type mysql, got %s", driver.Type())
	}
	if driver.Name() != "test" {
		t.Errorf("expected name test, got %s", driver.Name())
	}

	// Verify it's actually a MySQL driver
	_, ok := driver.(*MySQLDriver)
	if !ok {
		t.Error("expected *MySQLDriver")
	}
}

// TestNewDriver_Postgres_NotImplemented confirms postgres type returns not-implemented error
func TestNewDriver_Postgres_NotImplemented(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "postgres",
	}

	_, err := NewDriver(cfg)
	if err == nil {
		t.Fatal("expected error for postgres")
	}
	if !strings.Contains(err.Error(), "postgres support not yet implemented") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewDriver_UnknownType rejects unrecognized database types like oracle
func TestNewDriver_UnknownType(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "oracle",
	}

	_, err := NewDriver(cfg)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown database type: oracle") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewDriver_SQLiteInvalidPath ensures SQLite driver requires non-empty path
func TestNewDriver_SQLiteInvalidPath(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "sqlite",
		Path: "", // Invalid - empty path
	}

	_, err := NewDriver(cfg)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDriverInterface_SQLite validates SQLiteDriver implements all Driver interface methods
func TestDriverInterface_SQLite(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "sqlite",
		Path: ":memory:",
	}

	var driver Driver
	var err error

	driver, err = NewDriver(cfg)
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	defer func() { _ = driver.Close() }()

	// All interface methods should be callable
	_ = driver.Name()
	_ = driver.Type()
	_ = driver.IsReadOnly()
	_ = driver.Config()

	// Test Ping
	ctx := t.Context()
	if err := driver.Ping(ctx); err != nil {
		t.Errorf("ping failed: %v", err)
	}

	// Test Query
	results, err := driver.Query(ctx, config.SessionConfig{}, "SELECT 1 as num", nil, nil)
	if err != nil {
		t.Errorf("query failed: %v", err)
	}
	if len(results.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(results.Rows))
	}
}

// TestDriverInterface_Polymorphism verifies multiple drivers work through interface
func TestDriverInterface_Polymorphism(t *testing.T) {
	drivers := make([]Driver, 0)

	cfg1 := config.DatabaseConfig{
		Name: "db1",
		Type: "sqlite",
		Path: ":memory:",
	}
	d1, err := NewDriver(cfg1)
	if err != nil {
		t.Fatalf("failed to create db1: %v", err)
	}
	drivers = append(drivers, d1)

	cfg2 := config.DatabaseConfig{
		Name: "db2",
		Type: "sqlite",
		Path: ":memory:",
	}
	d2, err := NewDriver(cfg2)
	if err != nil {
		t.Fatalf("failed to create db2: %v", err)
	}
	drivers = append(drivers, d2)

	// Close all drivers at end
	defer func() {
		for _, d := range drivers {
			_ = d.Close()
		}
	}()

	// All should respond to polymorphic calls
	ctx := t.Context()
	for _, driver := range drivers {
		if driver.Type() != "sqlite" {
			t.Errorf("expected sqlite type, got %s", driver.Type())
		}
		if err := driver.Ping(ctx); err != nil {
			t.Errorf("ping failed for %s: %v", driver.Name(), err)
		}
	}
}

// TestNewDriver_AllTypes table-tests factory behavior for all database type values
func TestNewDriver_AllTypes(t *testing.T) {
	tests := []struct {
		name        string
		dbType      string
		path        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "sqlite valid",
			dbType:      "sqlite",
			path:        ":memory:",
			expectError: false,
		},
		{
			name:        "sqlite invalid path",
			dbType:      "sqlite",
			path:        "",
			expectError: true,
			errorMsg:    "path is required",
		},
		{
			name:        "mysql requires connection",
			dbType:      "mysql",
			expectError: true,
			errorMsg:    "failed to ping mysql database",
		},
		{
			name:        "postgres not implemented",
			dbType:      "postgres",
			expectError: true,
			errorMsg:    "not yet implemented",
		},
		{
			name:        "unknown type",
			dbType:      "mongodb",
			expectError: true,
			errorMsg:    "unknown database type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DatabaseConfig{
				Name: "test",
				Type: tt.dbType,
				Path: tt.path,
			}

			driver, err := NewDriver(cfg)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
					if driver != nil {
						_ = driver.Close()
					}
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if driver != nil {
					_ = driver.Close()
				}
			}
		})
	}
}
