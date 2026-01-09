package db

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"sql-proxy/internal/config"
)

// Benchmark setup helper
func setupBenchmarkDB(b *testing.B) (*SQLiteDriver, func()) {
	b.Helper()

	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "bench",
		Type:     "sqlite",
		Path:     ":memory:",
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		b.Fatalf("failed to create driver: %v", err)
	}

	// Create test table
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			status TEXT DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`, nil)
	if err != nil {
		driver.Close()
		b.Fatalf("failed to create table: %v", err)
	}

	// Insert 1000 rows for query benchmarks
	for i := 0; i < 1000; i++ {
		_, err = driver.Query(ctx, sessCfg,
			"INSERT INTO users (name, email) VALUES (@name, @email)",
			map[string]any{"name": fmt.Sprintf("User%d", i), "email": fmt.Sprintf("user%d@test.com", i)},
		)
		if err != nil {
			driver.Close()
			b.Fatalf("failed to insert: %v", err)
		}
	}

	return driver, func() { driver.Close() }
}

// BenchmarkSQLiteDriver_SimpleQuery measures minimal "SELECT 1" query performance
func BenchmarkSQLiteDriver_SimpleQuery(b *testing.B) {
	driver, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := driver.Query(ctx, sessCfg, "SELECT 1", nil)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
	}
}

// BenchmarkSQLiteDriver_SelectAll measures full table scan of 1000 rows
func BenchmarkSQLiteDriver_SelectAll(b *testing.B) {
	driver, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := driver.Query(ctx, sessCfg, "SELECT * FROM users", nil)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		if len(results) != 1000 {
			b.Fatalf("expected 1000 rows, got %d", len(results))
		}
	}
}

// BenchmarkSQLiteDriver_SelectWithParam measures parameterized single-row lookup
func BenchmarkSQLiteDriver_SelectWithParam(b *testing.B) {
	driver, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := driver.Query(ctx, sessCfg,
			"SELECT * FROM users WHERE id = @id",
			map[string]any{"id": i%1000 + 1},
		)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
	}
}

// BenchmarkSQLiteDriver_SelectWithMultipleParams measures query with 3 WHERE parameters
func BenchmarkSQLiteDriver_SelectWithMultipleParams(b *testing.B) {
	driver, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := driver.Query(ctx, sessCfg,
			"SELECT * FROM users WHERE id >= @min AND id <= @max AND status = @status",
			map[string]any{"min": 1, "max": 100, "status": "active"},
		)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
	}
}

// BenchmarkSQLiteDriver_Insert measures single row insert performance
func BenchmarkSQLiteDriver_Insert(b *testing.B) {
	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "bench",
		Type:     "sqlite",
		Path:     ":memory:",
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		b.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE bench_insert (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)
	`, nil)
	if err != nil {
		b.Fatalf("failed to create table: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := driver.Query(ctx, sessCfg,
			"INSERT INTO bench_insert (name) VALUES (@name)",
			map[string]any{"name": fmt.Sprintf("BenchUser%d", i)},
		)
		if err != nil {
			b.Fatalf("insert failed: %v", err)
		}
	}
}

// BenchmarkSQLiteDriver_ConcurrentReads measures parallel read operations on file-based db
func BenchmarkSQLiteDriver_ConcurrentReads(b *testing.B) {
	// Use file-based SQLite for concurrent access (in-memory doesn't share state across pool connections)
	tmpFile := b.TempDir() + "/bench_concurrent_reads.db"

	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "bench",
		Type:     "sqlite",
		Path:     tmpFile,
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		b.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Create and populate table
	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL
		)
	`, nil)
	if err != nil {
		b.Fatalf("failed to create table: %v", err)
	}

	for i := 0; i < 1000; i++ {
		_, err = driver.Query(ctx, sessCfg,
			"INSERT INTO users (name, email) VALUES (@name, @email)",
			map[string]any{"name": fmt.Sprintf("User%d", i), "email": fmt.Sprintf("user%d@test.com", i)},
		)
		if err != nil {
			b.Fatalf("failed to insert: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, err := driver.Query(ctx, sessCfg,
				"SELECT * FROM users WHERE id = @id",
				map[string]any{"id": i%1000 + 1},
			)
			if err != nil {
				b.Errorf("query failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkSQLiteDriver_TranslateQuery measures @param to $param translation speed
func BenchmarkSQLiteDriver_TranslateQuery(b *testing.B) {
	driver := &SQLiteDriver{}
	query := "SELECT * FROM users WHERE name = @name AND status = @status AND id > @minId"
	params := map[string]any{
		"name":   "test",
		"status": "active",
		"minId":  100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = driver.translateQuery(query, params)
	}
}

// BenchmarkManager_Get measures driver lookup by name across 3 databases
func BenchmarkManager_Get(b *testing.B) {
	readOnly := false
	configs := []config.DatabaseConfig{
		{Name: "db1", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		{Name: "db2", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		{Name: "db3", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, err := NewManager(configs)
	if err != nil {
		b.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("db%d", i%3+1)
		_, err := manager.Get(name)
		if err != nil {
			b.Fatalf("database %s not found: %v", name, err)
		}
	}
}

// BenchmarkManager_Get_Concurrent measures parallel driver lookups across 3 databases
func BenchmarkManager_Get_Concurrent(b *testing.B) {
	readOnly := false
	configs := []config.DatabaseConfig{
		{Name: "db1", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		{Name: "db2", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		{Name: "db3", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, err := NewManager(configs)
	if err != nil {
		b.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := fmt.Sprintf("db%d", i%3+1)
			_, err := manager.Get(name)
			if err != nil {
				b.Errorf("database %s not found: %v", name, err)
			}
			i++
		}
	})
}

// BenchmarkManager_PingAll measures ping across all 3 databases sequentially
func BenchmarkManager_PingAll(b *testing.B) {
	readOnly := false
	configs := []config.DatabaseConfig{
		{Name: "db1", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		{Name: "db2", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
		{Name: "db3", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, err := NewManager(configs)
	if err != nil {
		b.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.PingAll(ctx)
	}
}

// BenchmarkSQLiteDriver_LargeResult_100 measures fetching 100 row result set
func BenchmarkSQLiteDriver_LargeResult_100(b *testing.B) {
	benchmarkLargeResult(b, 100)
}

// BenchmarkSQLiteDriver_LargeResult_1000 measures fetching 1000 row result set
func BenchmarkSQLiteDriver_LargeResult_1000(b *testing.B) {
	benchmarkLargeResult(b, 1000)
}

// BenchmarkSQLiteDriver_LargeResult_10000 measures fetching 10000 row result set
func BenchmarkSQLiteDriver_LargeResult_10000(b *testing.B) {
	benchmarkLargeResult(b, 10000)
}

func benchmarkLargeResult(b *testing.B, rowCount int) {
	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "bench",
		Type:     "sqlite",
		Path:     ":memory:",
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		b.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Create table with many rows
	_, err = driver.Query(ctx, sessCfg, "CREATE TABLE large (id INTEGER, data TEXT)", nil)
	if err != nil {
		b.Fatalf("failed to create table: %v", err)
	}

	for i := 0; i < rowCount; i++ {
		_, err = driver.Query(ctx, sessCfg,
			"INSERT INTO large (id, data) VALUES (@id, @data)",
			map[string]any{"id": i, "data": fmt.Sprintf("data%d", i)},
		)
		if err != nil {
			b.Fatalf("failed to insert: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := driver.Query(ctx, sessCfg, "SELECT * FROM large", nil)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		if len(results) != rowCount {
			b.Fatalf("expected %d rows, got %d", rowCount, len(results))
		}
	}
}

// BenchmarkSQLiteDriver_ConcurrentWrites measures serialized parallel writes with mutex
func BenchmarkSQLiteDriver_ConcurrentWrites(b *testing.B) {
	tmpFile := b.TempDir() + "/bench_concurrent.db"

	readOnly := false
	cfg := config.DatabaseConfig{
		Name:     "bench",
		Type:     "sqlite",
		Path:     tmpFile,
		ReadOnly: &readOnly,
	}

	driver, err := NewSQLiteDriver(cfg)
	if err != nil {
		b.Fatalf("failed to create driver: %v", err)
	}
	defer driver.Close()

	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE concurrent_write (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)
	`, nil)
	if err != nil {
		b.Fatalf("failed to create table: %v", err)
	}

	var mu sync.Mutex
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			mu.Lock()
			_, err := driver.Query(ctx, sessCfg,
				"INSERT INTO concurrent_write (name) VALUES (@name)",
				map[string]any{"name": fmt.Sprintf("User%d", i)},
			)
			mu.Unlock()
			if err != nil {
				b.Errorf("insert failed: %v", err)
			}
			i++
		}
	})
}
