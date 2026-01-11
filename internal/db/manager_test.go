package db

import (
	"context"
	"sync"
	"testing"

	"sql-proxy/internal/config"
)

// TestNewManager_SingleDatabase verifies manager creation with one SQLite database
func TestNewManager_SingleDatabase(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "test",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	if manager.Count() != 1 {
		t.Errorf("expected 1 connection, got %d", manager.Count())
	}
}

// TestNewManager_MultipleDatabases tests manager with three databases, validates Get by name
func TestNewManager_MultipleDatabases(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := []config.DatabaseConfig{
		{
			Name: "db1",
			Type: "sqlite",
			Path: tmpDir + "/db1.db",
		},
		{
			Name: "db2",
			Type: "sqlite",
			Path: tmpDir + "/db2.db",
		},
		{
			Name: "db3",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	if manager.Count() != 3 {
		t.Errorf("expected 3 connections, got %d", manager.Count())
	}

	names := manager.Names()
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}

	// Verify all databases are accessible
	for _, name := range []string{"db1", "db2", "db3"} {
		driver, err := manager.Get(name)
		if err != nil {
			t.Errorf("failed to get %s: %v", name, err)
		}
		if driver.Name() != name {
			t.Errorf("expected name %s, got %s", name, driver.Name())
		}
	}
}

// TestNewManager_EmptyConfig confirms manager handles zero databases gracefully
func TestNewManager_EmptyConfig(t *testing.T) {
	cfg := []config.DatabaseConfig{}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer manager.Close()

	if manager.Count() != 0 {
		t.Errorf("expected 0 connections, got %d", manager.Count())
	}
}

// TestNewManager_InvalidConfig ensures manager rejects invalid database config
func TestNewManager_InvalidConfig(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "test",
			Type: "sqlite",
			Path: "", // Invalid - missing path
		},
	}

	_, err := NewManager(cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

// TestManager_Get tests retrieving connections by name and error for unknown names
func TestManager_Get(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "test",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	// Get existing connection
	driver, err := manager.Get("test")
	if err != nil {
		t.Errorf("failed to get test: %v", err)
	}
	if driver.Type() != "sqlite" {
		t.Errorf("expected type sqlite, got %s", driver.Type())
	}

	// Get non-existent connection
	_, err = manager.Get("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent connection")
	}
}

// TestManager_IsReadOnly validates readonly status lookup for each connection
func TestManager_IsReadOnly(t *testing.T) {
	readOnly := true
	readWrite := false

	cfg := []config.DatabaseConfig{
		{
			Name:     "readonly",
			Type:     "sqlite",
			Path:     ":memory:",
			ReadOnly: &readOnly,
		},
		{
			Name:     "readwrite",
			Type:     "sqlite",
			Path:     ":memory:",
			ReadOnly: &readWrite,
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	// Check readonly
	isRO, err := manager.IsReadOnly("readonly")
	if err != nil {
		t.Errorf("failed to check readonly: %v", err)
	}
	if !isRO {
		t.Error("expected readonly to be true")
	}

	// Check readwrite
	isRO, err = manager.IsReadOnly("readwrite")
	if err != nil {
		t.Errorf("failed to check readwrite: %v", err)
	}
	if isRO {
		t.Error("expected readwrite to be false")
	}

	// Check non-existent
	_, err = manager.IsReadOnly("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent connection")
	}
}

// TestManager_Ping checks connectivity to all managed databases individually
func TestManager_Ping(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "db1",
			Type: "sqlite",
			Path: ":memory:",
		},
		{
			Name: "db2",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	results := manager.Ping(ctx)

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	for name, err := range results {
		if err != nil {
			t.Errorf("ping failed for %s: %v", name, err)
		}
	}
}

// TestManager_PingAll verifies all connections are healthy in single call
func TestManager_PingAll(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "db1",
			Type: "sqlite",
			Path: ":memory:",
		},
		{
			Name: "db2",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	if !manager.PingAll(ctx) {
		t.Error("expected PingAll to return true")
	}
}

// TestManager_Reconnect tests single connection re-establishment by name
func TestManager_Reconnect(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	readOnly := false
	cfg := []config.DatabaseConfig{
		{
			Name:     "test",
			Type:     "sqlite",
			Path:     dbPath,
			ReadOnly: &readOnly,
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	// Test successful reconnect
	err = manager.Reconnect("test")
	if err != nil {
		t.Errorf("reconnect failed: %v", err)
	}

	// Verify connection works after reconnect
	ctx := context.Background()
	if !manager.PingAll(ctx) {
		t.Error("ping failed after reconnect")
	}

	// Test reconnect for non-existent
	err = manager.Reconnect("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent connection")
	}
}

// TestManager_ReconnectAll reconnects all databases and verifies connectivity
func TestManager_ReconnectAll(t *testing.T) {
	tmpDir := t.TempDir()

	readOnly := false
	cfg := []config.DatabaseConfig{
		{
			Name:     "db1",
			Type:     "sqlite",
			Path:     tmpDir + "/db1.db",
			ReadOnly: &readOnly,
		},
		{
			Name:     "db2",
			Type:     "sqlite",
			Path:     tmpDir + "/db2.db",
			ReadOnly: &readOnly,
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	results := manager.ReconnectAll()

	for name, err := range results {
		if err != nil {
			t.Errorf("reconnect failed for %s: %v", name, err)
		}
	}

	// Verify connections work after reconnect
	ctx := context.Background()
	if !manager.PingAll(ctx) {
		t.Error("ping failed after reconnect all")
	}
}

// TestManager_Close ensures all connections are released and count returns 0
func TestManager_Close(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "test",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	err = manager.Close()
	if err != nil {
		t.Errorf("close failed: %v", err)
	}

	if manager.Count() != 0 {
		t.Errorf("expected 0 connections after close, got %d", manager.Count())
	}
}

// TestManager_ConcurrentAccess runs 100 concurrent Get and Ping operations
func TestManager_ConcurrentAccess(t *testing.T) {
	cfg := []config.DatabaseConfig{
		{
			Name: "test",
			Type: "sqlite",
			Path: ":memory:",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.Get("test")
			if err != nil {
				errors <- err
			}
		}()
	}

	// Concurrent pings
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Ping(ctx)
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent access error: %v", err)
	}
}

// TestManager_ConcurrentReconnect tests concurrent Reconnect calls to prevent race conditions
func TestManager_ConcurrentReconnect(t *testing.T) {
	tmpDir := t.TempDir()

	readOnly := false
	cfg := []config.DatabaseConfig{
		{
			Name:     "test",
			Type:     "sqlite",
			Path:     tmpDir + "/test.db",
			ReadOnly: &readOnly,
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Launch concurrent reconnect attempts
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := manager.Reconnect("test"); err != nil {
				errors <- err
			}
		}()
	}

	// Interleave with Get and Ping operations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.Get("test"); err != nil {
				errors <- err
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Ping(ctx)
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent reconnect error: %v", err)
	}

	// Verify connection is still functional after all reconnects
	if !manager.PingAll(ctx) {
		t.Error("connection unhealthy after concurrent reconnects")
	}
}

// TestManager_ConcurrentReconnectAll tests concurrent ReconnectAll calls
func TestManager_ConcurrentReconnectAll(t *testing.T) {
	tmpDir := t.TempDir()

	readOnly := false
	cfg := []config.DatabaseConfig{
		{
			Name:     "db1",
			Type:     "sqlite",
			Path:     tmpDir + "/db1.db",
			ReadOnly: &readOnly,
		},
		{
			Name:     "db2",
			Type:     "sqlite",
			Path:     tmpDir + "/db2.db",
			ReadOnly: &readOnly,
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Launch concurrent ReconnectAll calls
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results := manager.ReconnectAll()
			for name, err := range results {
				if err != nil {
					t.Errorf("reconnect failed for %s: %v", name, err)
				}
			}
		}()
	}

	wg.Wait()

	// Verify all connections are healthy
	if !manager.PingAll(ctx) {
		t.Error("connections unhealthy after concurrent ReconnectAll")
	}
}

// TestManager_MixedDatabaseTypes manages SQLite connections with different readonly/settings
func TestManager_MixedDatabaseTypes(t *testing.T) {
	tmpDir := t.TempDir()

	readOnly := true
	readWrite := false
	busyTimeout := 10000

	cfg := []config.DatabaseConfig{
		{
			Name:     "readonly_memory",
			Type:     "sqlite",
			Path:     ":memory:",
			ReadOnly: &readOnly,
		},
		{
			Name:          "readwrite_file",
			Type:          "sqlite",
			Path:          tmpDir + "/test.db",
			ReadOnly:      &readWrite,
			BusyTimeoutMs: &busyTimeout,
			JournalMode:   "wal",
		},
	}

	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	// Verify readonly
	driver1, err := manager.Get("readonly_memory")
	if err != nil {
		t.Fatalf("failed to get readonly_memory: %v", err)
	}
	if !driver1.IsReadOnly() {
		t.Error("expected readonly_memory to be read-only")
	}

	// Verify readwrite
	driver2, err := manager.Get("readwrite_file")
	if err != nil {
		t.Fatalf("failed to get readwrite_file: %v", err)
	}
	if driver2.IsReadOnly() {
		t.Error("expected readwrite_file to be read-write")
	}

	// Verify configs are preserved
	cfg1 := driver1.Config()
	if cfg1.JournalMode != "" {
		t.Errorf("expected empty journal mode for readonly_memory, got %s", cfg1.JournalMode)
	}

	cfg2 := driver2.Config()
	if cfg2.JournalMode != "wal" {
		t.Errorf("expected journal mode 'wal' for readwrite_file, got %s", cfg2.JournalMode)
	}
	if cfg2.BusyTimeoutMs == nil || *cfg2.BusyTimeoutMs != 10000 {
		t.Errorf("expected busy timeout 10000 for readwrite_file")
	}
}
