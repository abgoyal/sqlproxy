package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/metrics"
	"sql-proxy/internal/workflow"
)

func createTestConfig() *config.Config {
	readOnly := false
	return &config.Config{
		Server: config.ServerConfig{
			Host:              "127.0.0.1",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
			Version:           "test",
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
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "list_all",
				Triggers: []workflow.TriggerConfig{
					{
						Type:   "http",
						Path:   "/api/test",
						Method: "GET",
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT 1 as num, 'hello' as msg",
					},
					{
						Type:     "response",
						Template: `{"success": true, "count": {{len .steps.fetch.data}}, "data": {{json .steps.fetch.data}}}`,
					},
				},
			},
			{
				Name: "with_params",
				Triggers: []workflow.TriggerConfig{
					{
						Type:   "http",
						Path:   "/api/params",
						Method: "GET",
						Parameters: []workflow.ParamConfig{
							{Name: "name", Type: "string", Required: true},
							{Name: "value", Type: "int", Required: false, Default: "42"},
						},
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT @name as name, @value as value",
					},
					{
						Type:     "response",
						Template: `{"success": true, "count": {{len .steps.fetch.data}}, "data": {{json .steps.fetch.data}}}`,
					},
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

	req := httptest.NewRequest("GET", "/_/health", nil)
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

// TestServer_MetricsHandler_Disabled tests /_/metrics.json returns not-enabled message when disabled
func TestServer_MetricsHandler_Disabled(t *testing.T) {
	// Clear global metrics state from previous tests
	metrics.Clear()

	cfg := createTestConfig()
	cfg.Metrics.Enabled = false

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/_/metrics.json", nil)
	w := httptest.NewRecorder()

	srv.metricsJSONHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "not enabled") {
		t.Errorf("expected 'not enabled' in response, got: %s", body)
	}
}

// TestServer_MetricsJSONHandler_Enabled tests /_/metrics.json returns valid JSON metrics
func TestServer_MetricsJSONHandler_Enabled(t *testing.T) {
	// Clear global metrics state from previous tests
	metrics.Clear()

	cfg := createTestConfig()
	cfg.Metrics.Enabled = true

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/_/metrics.json", nil)
	w := httptest.NewRecorder()

	srv.metricsJSONHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	// Check required fields exist
	requiredFields := []string{"timestamp", "uptime_sec", "total_requests", "total_errors", "db_healthy", "runtime", "endpoints"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("expected field %q in metrics response", field)
		}
	}

	// Check runtime stats
	runtime, ok := result["runtime"].(map[string]any)
	if !ok {
		t.Fatal("expected runtime to be a map")
	}
	if runtime["go_version"] == nil {
		t.Error("expected go_version in runtime stats")
	}
	if runtime["goroutines"] == nil {
		t.Error("expected goroutines in runtime stats")
	}
}

// TestServer_MetricsPrometheusHandler_Enabled tests /_/metrics returns Prometheus format
func TestServer_MetricsPrometheusHandler_Enabled(t *testing.T) {
	// Clear global metrics state from previous tests
	metrics.Clear()

	cfg := createTestConfig()
	cfg.Metrics.Enabled = true

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Build test server with the server's HTTP handler
	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// Make requests to generate metrics data
	resp, err := http.Get(ts.URL + "/api/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Make another request with params (different endpoint)
	resp2, err := http.Get(ts.URL + "/api/params?name=test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp2.Body.Close()

	// Now get Prometheus metrics
	resp3, err := http.Get(ts.URL + "/_/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp3.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp3.Body)
	body := string(bodyBytes)

	// Check for expected Prometheus metrics
	expectedMetrics := []string{
		"sqlproxy_info",
		"sqlproxy_uptime_seconds",
		"go_goroutines",
		"process_cpu_seconds_total",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("expected metric %q in Prometheus output", metric)
		}
	}

	// Check it's valid Prometheus format (has HELP and TYPE lines)
	if !strings.Contains(body, "# HELP") {
		t.Error("expected # HELP lines in Prometheus output")
	}
	if !strings.Contains(body, "# TYPE") {
		t.Error("expected # TYPE lines in Prometheus output")
	}

	// Verify sqlproxy_info has correct labels with version
	if !strings.Contains(body, `sqlproxy_info{`) {
		t.Error("expected sqlproxy_info with labels")
	}
}

// TestServer_MetricsPrometheusHandler_Disabled tests /_/metrics returns error when disabled
func TestServer_MetricsPrometheusHandler_Disabled(t *testing.T) {
	// Clear global metrics state from previous tests
	metrics.Clear()

	cfg := createTestConfig()
	cfg.Metrics.Enabled = false

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/_/metrics", nil)
	w := httptest.NewRecorder()

	srv.metricsPrometheusHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
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
	req := httptest.NewRequest("GET", "/_/config/loglevel", nil)
	w := httptest.NewRecorder()
	srv.logLevelHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// POST to change level
	req = httptest.NewRequest("POST", "/_/config/loglevel?level=debug", nil)
	w = httptest.NewRecorder()
	srv.logLevelHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// POST without level parameter
	req = httptest.NewRequest("POST", "/_/config/loglevel", nil)
	w = httptest.NewRecorder()
	srv.logLevelHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestServer_ListEndpointsHandler tests root path returns service info and workflow listing
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

	workflows, ok := resp["workflows"].([]any)
	if !ok {
		t.Fatal("expected workflows array")
	}

	if len(workflows) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(workflows))
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

	req := httptest.NewRequest("GET", "/_/openapi.json", nil)
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

// TestServer_Integration_WorkflowEndpoint tests workflow execution via httptest server
func TestServer_Integration_WorkflowEndpoint(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Create a test server using the server's HTTP handler
	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// Test workflow endpoint
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

// TestServer_Integration_ParameterizedWorkflow tests parameterized workflow with required and optional params
func TestServer_Integration_ParameterizedWorkflow(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	ts := httptest.NewServer(srv.httpServer.Handler)
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

// TestServer_Integration_WithGzip tests HTTP request/response cycle with gzip encoding
func TestServer_Integration_WithGzip(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	ts := httptest.NewServer(srv.httpServer.Handler)
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

// TestServer_HealthHandler_Degraded tests /health returns degraded status when database is unreachable
func TestServer_HealthHandler_Degraded(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Close the database connection to simulate failure
	driver, err := srv.dbManager.Get("test")
	if err != nil {
		t.Fatalf("failed to get database: %v", err)
	}
	driver.Close()

	// Now health check should show degraded/unhealthy status
	req := httptest.NewRequest("GET", "/_/health", nil)
	w := httptest.NewRecorder()

	srv.healthHandler(w, req)

	// Always returns 200 - clients parse the status field
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// With single DB down, status is "unhealthy" (all DBs disconnected)
	if resp["status"] != "unhealthy" {
		t.Errorf("expected status 'unhealthy', got %v", resp["status"])
	}

	databases, ok := resp["databases"].(map[string]any)
	if !ok {
		t.Fatal("expected databases map")
	}

	if databases["test"] != "disconnected" {
		t.Errorf("expected test database to be 'disconnected', got %v", databases["test"])
	}
}

// TestServer_HealthHandler_DatabaseDown tests /_/health shows database as disconnected when ping fails
func TestServer_HealthHandler_DatabaseDown(t *testing.T) {
	cfg := createTestConfig()
	// Use an invalid SQLite path to simulate connection failure
	cfg.Databases[0].Path = "/nonexistent/path/that/does/not/exist.db"

	// This should fail to create server since DB can't connect
	_, err := New(cfg, true)
	if err == nil {
		t.Error("expected error when database path is invalid")
	}
}

// TestServer_HealthHandler_MultipleDatabases tests /_/health with multiple database connections
func TestServer_HealthHandler_MultipleDatabases(t *testing.T) {
	readOnly := false
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "127.0.0.1",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{
				Name:     "db1",
				Type:     "sqlite",
				Path:     ":memory:",
				ReadOnly: &readOnly,
			},
			{
				Name:     "db2",
				Type:     "sqlite",
				Path:     ":memory:",
				ReadOnly: &readOnly,
			},
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
		},
		Workflows: []workflow.WorkflowConfig{},
	}

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("GET", "/_/health", nil)
	w := httptest.NewRecorder()

	srv.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	databases := result["databases"].(map[string]any)
	if databases["db1"] != "connected" {
		t.Errorf("expected db1 connected, got %v", databases["db1"])
	}
	if databases["db2"] != "connected" {
		t.Errorf("expected db2 connected, got %v", databases["db2"])
	}
}

// TestServer_DBHealthHandler tests /_/health/{dbname} endpoint
func TestServer_DBHealthHandler(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		checkBody      func(t *testing.T, body map[string]any)
	}{
		{
			name:           "valid database",
			path:           "/_/health/test",
			expectedStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["database"] != "test" {
					t.Errorf("expected database='test', got %v", body["database"])
				}
				if body["status"] != "connected" {
					t.Errorf("expected status='connected', got %v", body["status"])
				}
				if body["type"] != "sqlite" {
					t.Errorf("expected type='sqlite', got %v", body["type"])
				}
				if _, ok := body["readonly"]; !ok {
					t.Error("expected readonly field in response")
				}
			},
		},
		{
			name:           "unknown database",
			path:           "/_/health/nonexistent",
			expectedStatus: http.StatusNotFound,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["error"] == nil {
					t.Error("expected error field for unknown database")
				}
			},
		},
		{
			name:           "missing database name",
			path:           "/_/health/",
			expectedStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["error"] == nil {
					t.Error("expected error field for missing database name")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			srv.dbHealthHandler(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			tt.checkBody(t, body)
		})
	}
}

// TestServer_DBHealthHandler_Disconnected tests /_/health/{dbname} when db is down
func TestServer_DBHealthHandler_Disconnected(t *testing.T) {
	cfg := createTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Close the database to simulate disconnection
	driver, err := srv.dbManager.Get("test")
	if err != nil {
		t.Fatalf("failed to get database: %v", err)
	}
	driver.Close()

	req := httptest.NewRequest("GET", "/_/health/test", nil)
	w := httptest.NewRecorder()

	srv.dbHealthHandler(w, req)

	// Should still return 200 - clients parse status field
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "disconnected" {
		t.Errorf("expected status='disconnected', got %v", body["status"])
	}
}

// TestServer_CacheClearHandler tests /_/cache/clear endpoint
func TestServer_CacheClearHandler(t *testing.T) {
	readOnly := false
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "127.0.0.1",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
			Cache: &config.CacheConfig{
				Enabled:       true,
				MaxSizeMB:     64,
				DefaultTTLSec: 300,
			},
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
			Level: "error",
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
		},
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "cached_workflow",
				Triggers: []workflow.TriggerConfig{
					{
						Type:   "http",
						Path:   "/api/cached",
						Method: "GET",
						Cache: &workflow.CacheConfig{
							Enabled: true,
							Key:     "test:static",
							TTLSec:  60,
						},
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT 1 as num",
					},
					{
						Type:     "response",
						Template: `{"success": true, "data": {{json .steps.fetch.data}}}`,
					},
				},
			},
		},
	}

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/_/cache/clear", nil)
		w := httptest.NewRecorder()

		srv.cacheClearHandler(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", w.Code)
		}
	})

	t.Run("clear all cache", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/_/cache/clear", nil)
		w := httptest.NewRecorder()

		srv.cacheClearHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var result map[string]any
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result["status"] != "ok" {
			t.Errorf("expected status='ok', got %v", result["status"])
		}
		if result["message"] != "all cache cleared" {
			t.Errorf("expected message='all cache cleared', got %v", result["message"])
		}
	})

	t.Run("clear specific endpoint", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/_/cache/clear?endpoint=/api/cached", nil)
		w := httptest.NewRecorder()

		srv.cacheClearHandler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var result map[string]any
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result["status"] != "ok" {
			t.Errorf("expected status='ok', got %v", result["status"])
		}
		if result["endpoint"] != "/api/cached" {
			t.Errorf("expected endpoint='/api/cached', got %v", result["endpoint"])
		}
	})
}

// TestServer_CacheClearHandler_NoCacheConfigured tests cache clear when cache disabled
func TestServer_CacheClearHandler_NoCacheConfigured(t *testing.T) {
	cfg := createTestConfig() // No cache configured

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	req := httptest.NewRequest("POST", "/_/cache/clear", nil)
	w := httptest.NewRecorder()

	srv.cacheClearHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 when cache not enabled, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["error"] == nil {
		t.Error("expected error field when cache not enabled")
	}
}

// TestServer_RateLimitsHandler tests the /_/ratelimits endpoint
func TestServer_RateLimitsHandler(t *testing.T) {
	readOnly := false
	cfg := &config.Config{
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
			Level: "error",
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
		},
		RateLimits: []config.RateLimitPoolConfig{
			{
				Name:              "global",
				RequestsPerSecond: 100,
				Burst:             200,
				Key:               "{{.trigger.client_ip}}",
			},
			{
				Name:              "per_user",
				RequestsPerSecond: 10,
				Burst:             20,
				Key:               `{{getOr .trigger.headers "Authorization" "anonymous"}}`,
			},
		},
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test",
				Triggers: []workflow.TriggerConfig{
					{
						Type:   "http",
						Path:   "/api/test",
						Method: "GET",
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT 1",
					},
					{
						Type:     "response",
						Template: `{"success": true}`,
					},
				},
			},
		},
	}

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Create httptest server
	mux := http.NewServeMux()
	mux.HandleFunc("/_/ratelimits", srv.rateLimitsHandler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test rate limits endpoint
	resp, err := http.Get(ts.URL + "/_/ratelimits")
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

	// Check response structure
	if result["enabled"] != true {
		t.Error("expected enabled=true")
	}
	if _, ok := result["total_allowed"]; !ok {
		t.Error("expected total_allowed field")
	}
	if _, ok := result["total_denied"]; !ok {
		t.Error("expected total_denied field")
	}

	pools, ok := result["pools"].([]any)
	if !ok {
		t.Fatal("expected pools array")
	}
	if len(pools) != 2 {
		t.Errorf("expected 2 pools, got %d", len(pools))
	}

	// Verify pool info
	foundGlobal := false
	for _, p := range pools {
		pool := p.(map[string]any)
		if pool["name"] == "global" {
			foundGlobal = true
			if pool["requests_per_second"].(float64) != 100 {
				t.Errorf("expected global rps=100, got %v", pool["requests_per_second"])
			}
			if pool["burst"].(float64) != 200 {
				t.Errorf("expected global burst=200, got %v", pool["burst"])
			}
		}
	}
	if !foundGlobal {
		t.Error("expected to find 'global' pool")
	}
}

// TestServer_RateLimitsHandler_NotConfigured tests the endpoint when rate limiting is disabled
func TestServer_RateLimitsHandler_NotConfigured(t *testing.T) {
	readOnly := false
	cfg := &config.Config{
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
			Level: "error",
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
		},
		// No rate limits configured
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "test",
				Triggers: []workflow.TriggerConfig{
					{
						Type:   "http",
						Path:   "/api/test",
						Method: "GET",
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT 1",
					},
					{
						Type:     "response",
						Template: `{"success": true}`,
					},
				},
			},
		},
	}

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Create httptest server
	mux := http.NewServeMux()
	mux.HandleFunc("/_/ratelimits", srv.rateLimitsHandler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test rate limits endpoint without rate limiting configured
	resp, err := http.Get(ts.URL + "/_/ratelimits")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return error when not configured
	if _, ok := result["error"]; !ok {
		t.Error("expected error field when rate limiting not configured")
	}
}

// TestServer_RateLimitResponse tests that 429 response includes retry_after_sec
func TestServer_RateLimitResponse(t *testing.T) {
	readOnly := false
	cfg := &config.Config{
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
			Level: "error",
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
		},
		RateLimits: []config.RateLimitPoolConfig{
			{
				Name:              "strict",
				RequestsPerSecond: 1,
				Burst:             1, // Only 1 request allowed
				Key:               "{{.trigger.client_ip}}",
			},
		},
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "rate_limited",
				Triggers: []workflow.TriggerConfig{
					{
						Type:   "http",
						Path:   "/api/limited",
						Method: "GET",
						RateLimit: []workflow.RateLimitRefConfig{
							{Pool: "strict"},
						},
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT 1",
					},
					{
						Type:     "response",
						Template: `{"success": true}`,
					},
				},
			},
		},
	}

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// First request should succeed
	resp1, err := http.Get(ts.URL + "/api/limited")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected first request to succeed with 200, got %d", resp1.StatusCode)
	}

	// Second request should be rate limited
	resp2, err := http.Get(ts.URL + "/api/limited")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", resp2.StatusCode)
	}

	// Check Retry-After header
	retryAfterHeader := resp2.Header.Get("Retry-After")
	if retryAfterHeader == "" {
		t.Error("expected Retry-After header")
	}

	// Check response body
	var result map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check that response includes retry_after_sec
	if result["success"] != false {
		t.Error("expected success=false")
	}
	if result["error"] != "rate limit exceeded" {
		t.Errorf("expected error='rate limit exceeded', got %v", result["error"])
	}
	retryAfterSec, ok := result["retry_after_sec"].(float64)
	if !ok {
		t.Error("expected retry_after_sec in response body")
	}
	if retryAfterSec <= 0 {
		t.Errorf("expected retry_after_sec > 0, got %v", retryAfterSec)
	}

	// Verify retry_after_sec matches Retry-After header
	headerVal, _ := strconv.Atoi(retryAfterHeader)
	if int(retryAfterSec) != headerVal {
		t.Errorf("retry_after_sec (%v) doesn't match Retry-After header (%s)", retryAfterSec, retryAfterHeader)
	}
}

// createCronTestConfig creates a test config with a cron-triggered workflow
func createCronTestConfig() *config.Config {
	readOnly := false
	return &config.Config{
		Server: config.ServerConfig{
			Host:              "127.0.0.1",
			Port:              0, // Random port
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
			Version:           "test",
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
			Level: "error",
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
		},
		Workflows: []workflow.WorkflowConfig{
			{
				Name: "cron_test",
				Triggers: []workflow.TriggerConfig{
					{
						Type:     "cron",
						Schedule: "*/5 * * * *", // Every 5 minutes
						Params: map[string]string{
							"source": "cron",
						},
					},
				},
				Steps: []workflow.StepConfig{
					{
						Name:     "fetch",
						Type:     "query",
						Database: "test",
						SQL:      "SELECT 1 as status",
					},
				},
			},
		},
	}
}

// TestServer_CronWorkflowSetup verifies cron workflow jobs are registered correctly
func TestServer_CronWorkflowSetup(t *testing.T) {
	cfg := createCronTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Verify cron scheduler was created
	if srv.cron == nil {
		t.Error("expected cron scheduler to be created for workflow with cron trigger")
	}
}

// TestServer_CronWorkflowExecution verifies cron workflow execution path works
func TestServer_CronWorkflowExecution(t *testing.T) {
	cfg := createCronTestConfig()

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Find the compiled workflow and trigger
	if len(srv.workflows) == 0 {
		t.Fatal("expected at least one workflow")
	}

	wf := srv.workflows[0]
	if len(wf.Triggers) == 0 {
		t.Fatal("expected at least one trigger")
	}

	trigger := wf.Triggers[0]
	if trigger.Config.Type != "cron" {
		t.Fatalf("expected cron trigger, got %s", trigger.Config.Type)
	}

	// Execute the cron workflow directly
	srv.executeWorkflowCron(wf, trigger)

	// If we get here without panic, the execution path works
	// The actual workflow result is logged, not returned
}

// TestServer_NoCronWorkflow verifies server works without cron triggers
func TestServer_NoCronWorkflow(t *testing.T) {
	cfg := createTestConfig() // Uses HTTP triggers only

	srv, err := New(cfg, true)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// Verify cron scheduler was NOT created for HTTP-only workflows
	if srv.cron != nil {
		t.Error("expected no cron scheduler for HTTP-only workflows")
	}
}
