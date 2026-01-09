package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
)

func setupBenchmarkHandler(b *testing.B) (*Handler, func()) {
	b.Helper()

	readOnly := false
	dbCfg := []config.DatabaseConfig{
		{
			Name:     "bench",
			Type:     "sqlite",
			Path:     ":memory:",
			ReadOnly: &readOnly,
		},
	}

	manager, err := db.NewManager(dbCfg)
	if err != nil {
		b.Fatalf("failed to create manager: %v", err)
	}

	// Create test table
	driver, _ := manager.Get("bench")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			status TEXT DEFAULT 'active'
		)
	`, nil)
	if err != nil {
		manager.Close()
		b.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	for i := 0; i < 100; i++ {
		_, err = driver.Query(ctx, sessCfg,
			"INSERT INTO users (name, email) VALUES (@name, @email)",
			map[string]any{"name": fmt.Sprintf("User%d", i), "email": fmt.Sprintf("user%d@test.com", i)},
		)
		if err != nil {
			manager.Close()
			b.Fatalf("failed to insert: %v", err)
		}
	}

	queryCfg := config.QueryConfig{
		Name:     "list_users",
		Database: "bench",
		Path:     "/api/users",
		Method:   "GET",
		SQL:      "SELECT * FROM users",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, queryCfg, serverCfg)

	return handler, func() { manager.Close() }
}

// BenchmarkHandler_ServeHTTP_SimpleQuery measures simple query handling throughput
func BenchmarkHandler_ServeHTTP_SimpleQuery(b *testing.B) {
	handler, cleanup := setupBenchmarkHandler(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/users", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d", w.Code)
		}
	}
}

// BenchmarkHandler_ServeHTTP_WithParams measures parameterized query performance
func BenchmarkHandler_ServeHTTP_WithParams(b *testing.B) {
	readOnly := false
	dbCfg := []config.DatabaseConfig{
		{Name: "bench", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, err := db.NewManager(dbCfg)
	if err != nil {
		b.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	driver, _ := manager.Get("bench")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, _ = driver.Query(ctx, sessCfg, `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, status TEXT)
	`, nil)

	for i := 0; i < 100; i++ {
		_, _ = driver.Query(ctx, sessCfg,
			"INSERT INTO users (id, name, status) VALUES (@id, @name, @status)",
			map[string]any{"id": i, "name": fmt.Sprintf("User%d", i), "status": "active"},
		)
	}

	queryCfg := config.QueryConfig{
		Name:     "get_user",
		Database: "bench",
		Path:     "/api/user",
		Method:   "GET",
		SQL:      "SELECT * FROM users WHERE id = @id",
		Parameters: []config.ParamConfig{
			{Name: "id", Type: "int", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, queryCfg, serverCfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/user?id=%d", i%100), nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d", w.Code)
		}
	}
}

// BenchmarkHandler_ServeHTTP_Concurrent measures parallel request handling
func BenchmarkHandler_ServeHTTP_Concurrent(b *testing.B) {
	handler, cleanup := setupBenchmarkHandler(b)
	defer cleanup()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/api/users", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				b.Errorf("expected 200, got %d", w.Code)
			}
		}
	})
}

// BenchmarkConvertValue_String measures string type conversion speed
func BenchmarkConvertValue_String(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = convertValue("test_string", "string")
	}
}

// BenchmarkConvertValue_Int measures integer type conversion speed
func BenchmarkConvertValue_Int(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = convertValue("12345", "int")
	}
}

// BenchmarkConvertValue_Float measures float type conversion speed
func BenchmarkConvertValue_Float(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = convertValue("3.14159", "float")
	}
}

// BenchmarkConvertValue_Bool measures boolean type conversion speed
func BenchmarkConvertValue_Bool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = convertValue("true", "bool")
	}
}

// BenchmarkConvertValue_DateTime measures datetime parsing performance
func BenchmarkConvertValue_DateTime(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = convertValue("2024-01-15T10:30:00Z", "datetime")
	}
}

// BenchmarkGenerateRequestID measures random ID generation throughput
func BenchmarkGenerateRequestID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = generateRequestID()
	}
}

// BenchmarkGetOrGenerateRequestID_WithHeader measures header extraction speed
func BenchmarkGetOrGenerateRequestID_WithHeader(b *testing.B) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "test-request-id-123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getOrGenerateRequestID(req)
	}
}

// BenchmarkGetOrGenerateRequestID_NoHeader measures ID generation when no header
func BenchmarkGetOrGenerateRequestID_NoHeader(b *testing.B) {
	req := httptest.NewRequest("GET", "/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getOrGenerateRequestID(req)
	}
}

// BenchmarkHandler_ParseParameters_NoParams measures parsing overhead with zero params
func BenchmarkHandler_ParseParameters_NoParams(b *testing.B) {
	readOnly := false
	dbCfg := []config.DatabaseConfig{
		{Name: "bench", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, _ := db.NewManager(dbCfg)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:       "test",
		Database:   "bench",
		Path:       "/test",
		Method:     "GET",
		SQL:        "SELECT 1",
		Parameters: []config.ParamConfig{},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, queryCfg, serverCfg)
	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handler.parseParameters(req)
	}
}

// BenchmarkHandler_ParseParameters_ManyParams measures parsing 5 params with type conversion
func BenchmarkHandler_ParseParameters_ManyParams(b *testing.B) {
	readOnly := false
	dbCfg := []config.DatabaseConfig{
		{Name: "bench", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, _ := db.NewManager(dbCfg)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "test",
		Database: "bench",
		Path:     "/test",
		Method:   "GET",
		SQL:      "SELECT @p1, @p2, @p3, @p4, @p5",
		Parameters: []config.ParamConfig{
			{Name: "p1", Type: "string", Required: true},
			{Name: "p2", Type: "int", Required: true},
			{Name: "p3", Type: "float", Required: false, Default: "1.5"},
			{Name: "p4", Type: "bool", Required: false, Default: "true"},
			{Name: "p5", Type: "datetime", Required: false, Default: "2024-01-01"},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, queryCfg, serverCfg)
	req := httptest.NewRequest("GET", "/test?p1=hello&p2=42&p3=3.14&p4=false&p5=2024-06-15", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handler.parseParameters(req)
	}
}

// BenchmarkHandler_ResolveTimeout measures timeout resolution with query params
func BenchmarkHandler_ResolveTimeout(b *testing.B) {
	readOnly := false
	dbCfg := []config.DatabaseConfig{
		{Name: "bench", Type: "sqlite", Path: ":memory:", ReadOnly: &readOnly},
	}

	manager, _ := db.NewManager(dbCfg)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:       "test",
		Database:   "bench",
		Path:       "/test",
		Method:     "GET",
		SQL:        "SELECT 1",
		TimeoutSec: 60,
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, queryCfg, serverCfg)
	req := httptest.NewRequest("GET", "/test?_timeout=120", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.resolveTimeout(req)
	}
}
