package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/handler"
)

func createTestConfig() *config.Config {
	readOnly := false
	return &config.Config{
		Server: config.ServerConfig{
			Host:              "127.0.0.1",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{
				Name:     "test",
				Type:     "sqlite",
				Path:     ":memory:",
				ReadOnly: &readOnly,
			},
		},
		Logging: config.LoggingConfig{
			Level: "error", // Quiet for tests
		},
		Metrics: config.MetricsConfig{
			Enabled: false, // Disable for tests
		},
		Queries: []config.QueryConfig{
			{
				Name:        "list_all",
				Database:    "test",
				Path:        "/api/test",
				Method:      "GET",
				Description: "Test endpoint",
				SQL:         "SELECT 1 as num, 'hello' as msg",
			},
			{
				Name:     "with_params",
				Database: "test",
				Path:     "/api/params",
				Method:   "GET",
				SQL:      "SELECT @name as name, @value as value",
				Parameters: []config.ParamConfig{
					{Name: "name", Type: "string", Required: true},
					{Name: "value", Type: "int", Required: false, Default: "42"},
				},
			},
		},
	}
}

// TestServer_New verifies server initialization creates dbManager and httpServer
func TestServer_New(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true) // interactive mode
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	if srv.dbManager == nil {
		t.Error("expected dbManager to be set")
	}
	if srv.httpServer == nil {
		t.Error("expected httpServer to be set")
	}
}

// TestServer_HealthHandler tests /health returns status and database connections
func TestServer_HealthHandler(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("expected status healthy, got %v", resp["status"])
	}

	databases, ok := resp["databases"].(map[string]any)
	if !ok {
		t.Fatal("expected databases map")
	}

	if databases["test"] != "connected" {
		t.Errorf("expected test database to be connected, got %v", databases["test"])
	}
}

// TestServer_MetricsHandler_Disabled tests /metrics returns not-enabled message when disabled
func TestServer_MetricsHandler_Disabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Metrics.Enabled = false

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	srv.metricsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "not enabled") {
		t.Errorf("expected 'not enabled' in response, got: %s", body)
	}
}

// TestServer_LogLevelHandler tests log level GET retrieval and POST update operations
func TestServer_LogLevelHandler(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// GET current level
	req := httptest.NewRequest("GET", "/config/loglevel", nil)
	w := httptest.NewRecorder()
	srv.logLevelHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// POST to change level
	req = httptest.NewRequest("POST", "/config/loglevel?level=debug", nil)
	w = httptest.NewRecorder()
	srv.logLevelHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// POST without level parameter
	req = httptest.NewRequest("POST", "/config/loglevel", nil)
	w = httptest.NewRecorder()
	srv.logLevelHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestServer_ListEndpointsHandler tests root path returns service info and endpoint listing
func TestServer_ListEndpointsHandler(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.listEndpointsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["service"] != "sql-proxy" {
		t.Errorf("expected service sql-proxy, got %v", resp["service"])
	}

	endpoints, ok := resp["endpoints"].([]any)
	if !ok {
		t.Fatal("expected endpoints array")
	}

	if len(endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(endpoints))
	}
}

// TestServer_ListEndpointsHandler_NotFound tests unknown paths return 404
func TestServer_ListEndpointsHandler_NotFound(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.listEndpointsHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// TestServer_OpenAPIHandler tests /openapi.json returns valid spec with CORS headers
func TestServer_OpenAPIHandler(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	w := httptest.NewRecorder()
	srv.openAPIHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	// Check CORS header
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header")
	}

	var spec map[string]any
	if err := json.NewDecoder(w.Body).Decode(&spec); err != nil {
		t.Fatalf("failed to decode OpenAPI spec: %v", err)
	}

	if spec["openapi"] == nil {
		t.Error("expected openapi version in spec")
	}
}

// TestServer_RecoveryMiddleware tests panic recovery returns 500 without server crash
func TestServer_RecoveryMiddleware(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Create a handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Wrap with recovery middleware
	handler := srv.recoveryMiddleware(panicHandler)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["success"] != false {
		t.Error("expected success=false")
	}
}

// TestServer_GzipMiddleware tests gzip compression when Accept-Encoding header set
func TestServer_GzipMiddleware(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Create a handler that returns content
	contentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message": "hello world"}`))
	})

	handler := srv.gzipMiddleware(contentHandler)

	// Test with gzip accepted
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Error("expected gzip content encoding")
	}

	// Verify content is actually gzipped
	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to read gzipped content: %v", err)
	}

	if string(body) != `{"message": "hello world"}` {
		t.Errorf("unexpected content: %s", body)
	}
}

// TestServer_GzipMiddleware_NoGzip tests no compression without Accept-Encoding header
func TestServer_GzipMiddleware_NoGzip(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	contentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message": "hello"}`))
	})

	handler := srv.gzipMiddleware(contentHandler)

	// Test without gzip accepted
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("should not use gzip when not accepted")
	}

	if w.Body.String() != `{"message": "hello"}` {
		t.Errorf("unexpected content: %s", w.Body.String())
	}
}

// TestServer_StartShutdown tests server start and graceful shutdown sequence
func TestServer_StartShutdown(t *testing.T) {
	cfg := createTestConfig()
	cfg.Server.Port = 0 // Use random port

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("shutdown error: %v", err)
	}

	// Check that Start() returned
	select {
	case err := <-errCh:
		if err != http.ErrServerClosed {
			t.Errorf("expected ErrServerClosed, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Start() did not return after shutdown")
	}
}

// TestServer_E2E_QueryEndpoint tests end-to-end query execution via HTTP test server
func TestServer_E2E_QueryEndpoint(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Create a test server using the handler
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.healthHandler)
	mux.HandleFunc("/metrics", srv.metricsHandler)
	mux.HandleFunc("/openapi.json", srv.openAPIHandler)
	mux.HandleFunc("/config/loglevel", srv.logLevelHandler)
	mux.HandleFunc("/", srv.listEndpointsHandler)

	// Register query endpoints
	for _, q := range cfg.Queries {
		if q.Path != "" {
			h := createQueryHandler(srv, q)
			mux.Handle(q.Path, h)
		}
	}

	ts := httptest.NewServer(srv.recoveryMiddleware(srv.gzipMiddleware(mux)))
	defer ts.Close()

	// Test query endpoint
	resp, err := http.Get(ts.URL + "/api/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}

	if result["count"].(float64) != 1 {
		t.Errorf("expected count=1, got %v", result["count"])
	}
}

// TestServer_E2E_ParameterizedQuery tests parameterized query with required and optional params
func TestServer_E2E_ParameterizedQuery(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	mux := http.NewServeMux()
	for _, q := range cfg.Queries {
		if q.Path != "" {
			h := createQueryHandler(srv, q)
			mux.Handle(q.Path, h)
		}
	}

	ts := httptest.NewServer(srv.recoveryMiddleware(mux))
	defer ts.Close()

	// Test with required parameter
	resp, err := http.Get(ts.URL + "/api/params?name=test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Test without required parameter
	resp2, err := http.Get(ts.URL + "/api/params")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp2.StatusCode)
	}
}

// TestServer_E2E_WithGzip tests full HTTP request/response cycle with gzip encoding
func TestServer_E2E_WithGzip(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	mux := http.NewServeMux()
	for _, q := range cfg.Queries {
		if q.Path != "" {
			h := createQueryHandler(srv, q)
			mux.Handle(q.Path, h)
		}
	}

	ts := httptest.NewServer(srv.gzipMiddleware(mux))
	defer ts.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", ts.URL+"/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Error("expected gzip content encoding")
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	var result map[string]any
	if err := json.NewDecoder(gr).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

// Helper to create a query handler
func createQueryHandler(srv *Server, q config.QueryConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := handler.New(srv.dbManager, q, srv.config.Server)
		h.ServeHTTP(w, r)
	})
}
