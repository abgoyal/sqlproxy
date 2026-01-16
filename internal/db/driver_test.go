package db

import (
	"strings"
	"testing"

	"sql-proxy/internal/config"
)

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
	defer driver.Close()

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
	defer driver.Close()

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

// TestNewDriver_MySQL_NotImplemented confirms mysql type returns not-implemented error
func TestNewDriver_MySQL_NotImplemented(t *testing.T) {
	cfg := config.DatabaseConfig{
		Name: "test",
		Type: "mysql",
	}

	_, err := NewDriver(cfg)
	if err == nil {
		t.Fatal("expected error for mysql")
	}
	if !strings.Contains(err.Error(), "mysql support not yet implemented") {
		t.Errorf("unexpected error: %v", err)
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
	defer driver.Close()

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
	results, err := driver.Query(ctx, config.SessionConfig{}, "SELECT 1 as num", nil)
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
			d.Close()
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
			name:        "mysql not implemented",
			dbType:      "mysql",
			expectError: true,
			errorMsg:    "not yet implemented",
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
						driver.Close()
					}
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if driver != nil {
					driver.Close()
				}
			}
		})
	}
}
