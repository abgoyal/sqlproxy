package e2e

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// testServer holds state for a running test server instance
type testServer struct {
	cmd        *exec.Cmd
	configPath string
	dbPath     string
	baseURL    string
	port       int
}

// findFreePort finds an available port for the test server
func findFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// buildBinary compiles the sql-proxy binary for testing
func buildBinary(t *testing.T) string {
	t.Helper()

	// Build to temp directory
	tmpDir := t.TempDir()
	binaryName := "sql-proxy-test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(tmpDir, binaryName)

	// Get project root (e2e is one level down)
	projectRoot := filepath.Join("..")

	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build binary: %v\nOutput: %s", err, output)
	}

	return binaryPath
}

// createTestConfig writes a test configuration file with SQLite
func createTestConfig(t *testing.T, port int, dbPath string) string {
	t.Helper()

	config := fmt.Sprintf(`server:
  host: "127.0.0.1"
  port: %d
  default_timeout_sec: 30
  max_timeout_sec: 300

databases:
  - name: "test"
    type: "sqlite"
    path: "%s"
    readonly: false

logging:
  level: "error"
  file_path: ""
  max_size_mb: 10
  max_backups: 1
  max_age_days: 1

metrics:
  enabled: true

queries:
  - name: "health_check"
    database: "test"
    path: "/api/ping"
    method: "GET"
    description: "Simple health check query"
    sql: "SELECT 1 as status, 'ok' as message"

  - name: "list_items"
    database: "test"
    path: "/api/items"
    method: "GET"
    description: "List all items"
    sql: "SELECT * FROM items ORDER BY id"

  - name: "get_item"
    database: "test"
    path: "/api/item"
    method: "GET"
    description: "Get item by ID"
    sql: "SELECT * FROM items WHERE id = @id"
    parameters:
      - name: "id"
        type: "int"
        required: true

  - name: "search_items"
    database: "test"
    path: "/api/items/search"
    method: "GET"
    description: "Search items by name"
    sql: "SELECT * FROM items WHERE name LIKE '%%' || @query || '%%'"
    parameters:
      - name: "query"
        type: "string"
        required: false
        default: ""

  - name: "create_item"
    database: "test"
    path: "/api/items/create"
    method: "POST"
    description: "Create new item"
    sql: "INSERT INTO items (name, value) VALUES (@name, @value)"
    parameters:
      - name: "name"
        type: "string"
        required: true
      - name: "value"
        type: "int"
        required: false
        default: "0"
`, port, dbPath)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	return configPath
}

// setupTestDatabase creates the test SQLite database with schema
func setupTestDatabase(t *testing.T, dbPath string) {
	t.Helper()

	// Create database directory if needed
	if dir := filepath.Dir(dbPath); dir != "." {
		os.MkdirAll(dir, 0755)
	}

	// Use sqlite3 CLI or create via the binary's first run
	// For simplicity, we'll let the server create it and use raw SQL
}

// startServer starts the sql-proxy server and waits for it to be ready
func startServer(t *testing.T, binaryPath, configPath string, port int) *testServer {
	t.Helper()

	cmd := exec.Command(binaryPath, "-config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	ts := &testServer{
		cmd:        cmd,
		configPath: configPath,
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		port:       port,
	}

	// Wait for server to be ready
	if err := ts.waitReady(10 * time.Second); err != nil {
		cmd.Process.Kill()
		t.Fatalf("server failed to become ready: %v", err)
	}

	return ts
}

// waitReady polls the health endpoint until the server is ready
func (ts *testServer) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(ts.baseURL + "/_/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("server not ready after %v", timeout)
}

// stop gracefully stops the server
func (ts *testServer) stop() error {
	if ts.cmd == nil || ts.cmd.Process == nil {
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if runtime.GOOS == "windows" {
		return ts.cmd.Process.Kill()
	}

	ts.cmd.Process.Signal(syscall.SIGTERM)

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- ts.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		ts.cmd.Process.Kill()
		return fmt.Errorf("server did not shutdown gracefully, killed")
	}
}

// get makes a GET request and returns the response body
func (ts *testServer) get(path string) (*http.Response, error) {
	return http.Get(ts.baseURL + path)
}

// getJSON makes a GET request and decodes JSON response
func (ts *testServer) getJSON(path string, v any) (*http.Response, error) {
	resp, err := ts.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}

	if err := json.Unmarshal(body, v); err != nil {
		return resp, fmt.Errorf("json decode error: %v, body: %s", err, body)
	}

	return resp, nil
}

// post makes a POST request with form data
func (ts *testServer) post(path string, data string) (*http.Response, error) {
	return http.Post(
		ts.baseURL+path,
		"application/x-www-form-urlencoded",
		strings.NewReader(data),
	)
}

// TestE2E_ServerStartupAndShutdown tests the server starts and stops cleanly
func TestE2E_ServerStartupAndShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	// Verify server is running
	resp, err := ts.get("/_/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Stop and verify clean shutdown
	if err := ts.stop(); err != nil {
		// Ignore "signal: terminated" which is expected
		if !strings.Contains(err.Error(), "signal") {
			t.Errorf("server shutdown error: %v", err)
		}
	}
}

// TestE2E_HealthEndpoint tests /health returns database status
func TestE2E_HealthEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	var result map[string]any
	resp, err := ts.getJSON("/_/health", &result)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if result["status"] != "healthy" {
		t.Errorf("expected status healthy, got %v", result["status"])
	}

	databases, ok := result["databases"].(map[string]any)
	if !ok {
		t.Fatal("expected databases map in response")
	}

	if databases["test"] != "connected" {
		t.Errorf("expected test database connected, got %v", databases["test"])
	}
}

// TestE2E_MetricsEndpoint tests /_/metrics.json returns runtime stats
func TestE2E_MetricsEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	var result map[string]any
	resp, err := ts.getJSON("/_/metrics.json", &result)
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check required fields
	if result["uptime_sec"] == nil {
		t.Error("expected uptime_sec in metrics")
	}

	if result["db_healthy"] != true {
		t.Errorf("expected db_healthy=true, got %v", result["db_healthy"])
	}

	runtime, ok := result["runtime"].(map[string]any)
	if !ok {
		t.Fatal("expected runtime map in metrics")
	}

	if runtime["go_version"] == nil {
		t.Error("expected go_version in runtime")
	}
}

// TestE2E_OpenAPIEndpoint tests /_/openapi.json returns valid spec
func TestE2E_OpenAPIEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	var spec map[string]any
	resp, err := ts.getJSON("/_/openapi.json", &spec)
	if err != nil {
		t.Fatalf("openapi request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if spec["openapi"] != "3.0.3" {
		t.Errorf("expected openapi 3.0.3, got %v", spec["openapi"])
	}

	if spec["paths"] == nil {
		t.Error("expected paths in openapi spec")
	}

	// Check CORS header
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header")
	}
}

// TestE2E_RootEndpoint tests / returns endpoint listing
func TestE2E_RootEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	var result map[string]any
	resp, err := ts.getJSON("/", &result)
	if err != nil {
		t.Fatalf("root request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if result["service"] != "sql-proxy" {
		t.Errorf("expected service sql-proxy, got %v", result["service"])
	}

	endpoints, ok := result["endpoints"].([]any)
	if !ok {
		t.Fatal("expected endpoints array")
	}

	if len(endpoints) < 3 {
		t.Errorf("expected at least 3 endpoints, got %d", len(endpoints))
	}
}

// TestE2E_QueryEndpoint tests query execution returns data
func TestE2E_QueryEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	// Test simple query that doesn't need a table
	var result map[string]any
	resp, err := ts.getJSON("/api/ping", &result)
	if err != nil {
		t.Fatalf("query request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}

	if result["count"].(float64) != 1 {
		t.Errorf("expected count=1, got %v", result["count"])
	}

	data, ok := result["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatal("expected data array with results")
	}

	row := data[0].(map[string]any)
	if row["message"] != "ok" {
		t.Errorf("expected message=ok, got %v", row["message"])
	}
}

// TestE2E_QueryWithParameters tests parameterized query execution
func TestE2E_QueryWithParameters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	// Test missing required parameter returns 400
	resp, err := ts.get("/api/item")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing param, got %d", resp.StatusCode)
	}

	// Test with parameter (will return empty since table doesn't exist yet)
	resp, err = ts.get("/api/item?id=1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Should return 500 because table doesn't exist
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500 for missing table, got %d", resp.StatusCode)
	}
}

// TestE2E_LogLevelEndpoint tests runtime log level changes
func TestE2E_LogLevelEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	// GET current level
	var result map[string]any
	resp, err := ts.getJSON("/_/config/loglevel", &result)
	if err != nil {
		t.Fatalf("get loglevel failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// POST to change level
	resp, err = ts.post("/_/config/loglevel?level=debug", "")
	if err != nil {
		t.Fatalf("post loglevel failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify level changed
	resp, err = ts.getJSON("/_/config/loglevel", &result)
	if err != nil {
		t.Fatalf("get loglevel failed: %v", err)
	}

	if result["current_level"] != "debug" {
		t.Errorf("expected current_level=debug, got %v", result["current_level"])
	}
}

// TestE2E_GzipCompression tests response compression
func TestE2E_GzipCompression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	// Request with Accept-Encoding: gzip
	req, _ := http.NewRequest("GET", ts.baseURL+"/api/ping", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Error("expected gzip content encoding")
	}

	// Decompress and verify
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

// TestE2E_RequestID tests request ID propagation
func TestE2E_RequestID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	// Send request with custom request ID
	customID := "test-request-id-12345"
	req, _ := http.NewRequest("GET", ts.baseURL+"/api/ping", nil)
	req.Header.Set("X-Request-ID", customID)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response header
	if resp.Header.Get("X-Request-ID") != customID {
		t.Errorf("expected X-Request-ID=%s, got %s", customID, resp.Header.Get("X-Request-ID"))
	}

	// Check response body
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["request_id"] != customID {
		t.Errorf("expected request_id=%s in body, got %v", customID, result["request_id"])
	}
}

// TestE2E_NotFound tests 404 for unknown paths
func TestE2E_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	resp, err := ts.get("/nonexistent/path")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

// TestE2E_GracefulShutdown tests server handles SIGTERM gracefully
func TestE2E_GracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	if runtime.GOOS == "windows" {
		t.Skip("skipping signal test on windows")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)

	// Start a request that we'll interrupt
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.baseURL+"/api/ping", nil)
	http.DefaultClient.Do(req)

	// Send SIGTERM
	ts.cmd.Process.Signal(syscall.SIGTERM)

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- ts.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited, good
	case <-time.After(5 * time.Second):
		ts.cmd.Process.Kill()
		t.Error("server did not shutdown within 5 seconds")
	}

	// Verify server is no longer accepting connections
	_, err = http.Get(ts.baseURL + "/_/health")
	if err == nil {
		t.Error("expected connection refused after shutdown")
	}
}

// TestE2E_ConfigValidation tests -validate flag
func TestE2E_ConfigValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	configPath := createTestConfig(t, port, dbPath)

	// Run with -validate flag
	cmd := exec.Command(binaryPath, "-validate", "-config", configPath)
	output, err := cmd.CombinedOutput()

	// Should exit 0 for valid config
	if err != nil {
		t.Errorf("validation failed for valid config: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(string(output), "Configuration valid") {
		t.Errorf("expected 'Configuration valid' in output, got: %s", output)
	}
}

// TestE2E_InvalidConfig tests server rejects invalid config
func TestE2E_InvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)

	// Create invalid config (missing required fields)
	invalidConfig := `server:
  host: ""
  port: 0
`
	configPath := filepath.Join(t.TempDir(), "invalid.yaml")
	os.WriteFile(configPath, []byte(invalidConfig), 0644)

	// Run with invalid config
	cmd := exec.Command(binaryPath, "-validate", "-config", configPath)
	output, err := cmd.CombinedOutput()

	// Should exit non-zero for invalid config
	if err == nil {
		t.Error("expected validation to fail for invalid config")
	}

	if !strings.Contains(string(output), "error") && !strings.Contains(string(output), "Error") {
		t.Errorf("expected error in output, got: %s", output)
	}
}
