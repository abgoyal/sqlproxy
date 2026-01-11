package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
)

// createTestManager creates a db.Manager with an in-memory SQLite database and test schema
func createTestManager(t *testing.T) *db.Manager {
	t.Helper()

	readOnly := false
	cfg := []config.DatabaseConfig{
		{
			Name:     "test",
			Type:     "sqlite",
			Path:     ":memory:",
			ReadOnly: &readOnly,
		},
	}

	manager, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Create test tables
	driver, _ := manager.Get("test")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	// Create users table
	_, err = driver.Query(ctx, sessCfg, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			status TEXT DEFAULT 'active'
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	users := []struct {
		name, email, status string
	}{
		{"Alice", "alice@test.com", "active"},
		{"Bob", "bob@test.com", "active"},
		{"Charlie", "charlie@test.com", "inactive"},
	}

	for _, u := range users {
		_, err = driver.Query(ctx, sessCfg,
			"INSERT INTO users (name, email, status) VALUES (@name, @email, @status)",
			map[string]any{"name": u.name, "email": u.email, "status": u.status},
		)
		if err != nil {
			t.Fatalf("failed to insert user: %v", err)
		}
	}

	return manager
}

// TestHandler_ServeHTTP_SimpleQuery validates basic GET query returns JSON with success and data
func TestHandler_ServeHTTP_SimpleQuery(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "list_users",
		Database: "test",
		Path:     "/api/users",
		Method:   "GET",
		SQL:      "SELECT * FROM users ORDER BY id",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true, got false: %s", resp.Error)
	}

	if resp.Count != 3 {
		t.Errorf("expected count=3, got %d", resp.Count)
	}

	if resp.RequestID == "" {
		t.Error("expected request_id to be set")
	}

	// Check response header
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header")
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}
}

// TestHandler_ServeHTTP_WithParameters tests query string parameters are bound to SQL
func TestHandler_ServeHTTP_WithParameters(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_user",
		Database: "test",
		Path:     "/api/user",
		Method:   "GET",
		SQL:      "SELECT * FROM users WHERE status = @status",
		Parameters: []config.ParamConfig{
			{Name: "status", Type: "string", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/user?status=active", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 2 {
		t.Errorf("expected 2 active users, got %d", resp.Count)
	}
}

// TestHandler_ServeHTTP_MissingRequiredParam returns 400 when required parameter missing
func TestHandler_ServeHTTP_MissingRequiredParam(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_user",
		Database: "test",
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

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/user", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Success {
		t.Error("expected success=false")
	}

	if !strings.Contains(resp.Error, "missing required parameter") {
		t.Errorf("expected error about missing parameter, got: %s", resp.Error)
	}
}

// TestHandler_ServeHTTP_DefaultParameter uses default value when optional param omitted
func TestHandler_ServeHTTP_DefaultParameter(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "list_users",
		Database: "test",
		Path:     "/api/users",
		Method:   "GET",
		SQL:      "SELECT * FROM users WHERE status = @status",
		Parameters: []config.ParamConfig{
			{Name: "status", Type: "string", Required: false, Default: "active"},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	// Default status=active should return 2 users
	if resp.Count != 2 {
		t.Errorf("expected 2 users with default status=active, got %d", resp.Count)
	}
}

// TestHandler_ServeHTTP_WrongMethod returns 405 when HTTP method doesn't match config
func TestHandler_ServeHTTP_WrongMethod(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "list_users",
		Database: "test",
		Path:     "/api/users",
		Method:   "GET",
		SQL:      "SELECT * FROM users",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("POST", "/api/users", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestHandler_ServeHTTP_InvalidParamType returns 400 when int param gets non-numeric value
func TestHandler_ServeHTTP_InvalidParamType(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_user",
		Database: "test",
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

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/user?id=not_a_number", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestHandler_ServeHTTP_POSTMethod tests form-encoded POST parameters are parsed
func TestHandler_ServeHTTP_POSTMethod(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	readOnly := false
	queryCfg := config.QueryConfig{
		Name:     "insert_user",
		Database: "test",
		Path:     "/api/users",
		Method:   "POST",
		SQL:      "INSERT INTO users (name, email, status) VALUES (@name, @email, @status)",
		Parameters: []config.ParamConfig{
			{Name: "name", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
			{Name: "status", Type: "string", Required: false, Default: "active"},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	_ = readOnly // Not used in this test
	handler := New(manager, nil, queryCfg, serverCfg)

	form := url.Values{}
	form.Set("name", "Dave")
	form.Set("email", "dave@test.com")

	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// TestHandler_ServeHTTP_CustomRequestID echoes X-Request-ID or X-Correlation-ID headers
func TestHandler_ServeHTTP_CustomRequestID(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "list_users",
		Database: "test",
		Path:     "/api/users",
		Method:   "GET",
		SQL:      "SELECT * FROM users LIMIT 1",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	tests := []struct {
		name       string
		headerName string
		headerVal  string
	}{
		{"X-Request-ID", "X-Request-ID", "custom-request-123"},
		{"X-Correlation-ID", "X-Correlation-ID", "corr-456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/users", nil)
			req.Header.Set(tt.headerName, tt.headerVal)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Header().Get("X-Request-ID") != tt.headerVal {
				t.Errorf("expected X-Request-ID %s, got %s", tt.headerVal, w.Header().Get("X-Request-ID"))
			}

			var resp Response
			json.NewDecoder(w.Body).Decode(&resp)

			if resp.RequestID != tt.headerVal {
				t.Errorf("expected request_id %s, got %s", tt.headerVal, resp.RequestID)
			}
		})
	}
}

// TestHandler_ResolveTimeout validates timeout priority: _timeout param > query > default, capped by max
func TestHandler_ResolveTimeout(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	tests := []struct {
		name            string
		queryTimeout    int
		requestTimeout  string
		defaultTimeout  int
		maxTimeout      int
		expectedTimeout int
	}{
		{
			name:            "uses default",
			queryTimeout:    0,
			requestTimeout:  "",
			defaultTimeout:  30,
			maxTimeout:      300,
			expectedTimeout: 30,
		},
		{
			name:            "uses query config",
			queryTimeout:    60,
			requestTimeout:  "",
			defaultTimeout:  30,
			maxTimeout:      300,
			expectedTimeout: 60,
		},
		{
			name:            "uses request param",
			queryTimeout:    60,
			requestTimeout:  "120",
			defaultTimeout:  30,
			maxTimeout:      300,
			expectedTimeout: 120,
		},
		{
			name:            "caps at max",
			queryTimeout:    0,
			requestTimeout:  "600",
			defaultTimeout:  30,
			maxTimeout:      300,
			expectedTimeout: 300,
		},
		{
			name:            "zero request param ignored",
			queryTimeout:    0,
			requestTimeout:  "0",
			defaultTimeout:  30,
			maxTimeout:      300,
			expectedTimeout: 30, // 0 is not > 0, so request param ignored, use default
		},
		{
			name:            "invalid request param ignored",
			queryTimeout:    0,
			requestTimeout:  "invalid",
			defaultTimeout:  30,
			maxTimeout:      300,
			expectedTimeout: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queryCfg := config.QueryConfig{
				Name:       "test",
				Database:   "test",
				Path:       "/test",
				Method:     "GET",
				SQL:        "SELECT 1",
				TimeoutSec: tt.queryTimeout,
			}

			serverCfg := config.ServerConfig{
				DefaultTimeoutSec: tt.defaultTimeout,
				MaxTimeoutSec:     tt.maxTimeout,
			}

			handler := New(manager, nil, queryCfg, serverCfg)

			url := "/test"
			if tt.requestTimeout != "" {
				url += "?_timeout=" + tt.requestTimeout
			}

			req := httptest.NewRequest("GET", url, nil)
			result := handler.resolveTimeout(req)

			if result != tt.expectedTimeout {
				t.Errorf("expected timeout %d, got %d", tt.expectedTimeout, result)
			}
		})
	}
}

// TestConvertValue tests type conversion for string/int/bool/float/datetime parameters
func TestConvertValue(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		typeName  string
		wantValue any
		wantErr   bool
	}{
		{"string", "hello", "string", "hello", false},
		{"int", "42", "int", 42, false},
		{"integer", "42", "integer", 42, false},
		{"int invalid", "notanumber", "int", nil, true},
		{"bool true", "true", "bool", true, false},
		{"bool false", "false", "boolean", false, false},
		{"bool 1", "1", "bool", true, false},
		{"bool invalid", "maybe", "bool", nil, true},
		{"float", "3.14", "float", 3.14, false},
		{"double", "3.14159", "double", 3.14159, false},
		{"float invalid", "notafloat", "float", nil, true},
		{"datetime RFC3339", "2024-01-15T10:30:00Z", "datetime", mustParseTime("2006-01-02T15:04:05Z07:00", "2024-01-15T10:30:00Z"), false},
		{"datetime simple", "2024-01-15T10:30:00", "datetime", mustParseTime("2006-01-02T15:04:05", "2024-01-15T10:30:00"), false},
		{"date only", "2024-01-15", "date", mustParseTime("2006-01-02", "2024-01-15"), false},
		{"datetime invalid", "not-a-date", "datetime", nil, true},
		{"unknown type as string", "anything", "custom", "anything", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertValue(tt.value, tt.typeName)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// For time comparison
			if wantTime, ok := tt.wantValue.(time.Time); ok {
				gotTime, ok := result.(time.Time)
				if !ok {
					t.Errorf("expected time.Time, got %T", result)
					return
				}
				if !gotTime.Equal(wantTime) {
					t.Errorf("expected %v, got %v", wantTime, gotTime)
				}
				return
			}

			if result != tt.wantValue {
				t.Errorf("expected %v (%T), got %v (%T)", tt.wantValue, tt.wantValue, result, result)
			}
		})
	}
}

func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}

// TestHandler_ParseParameters validates required/optional/default parameter handling
func TestHandler_ParseParameters(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "test",
		Database: "test",
		Path:     "/test",
		Method:   "GET",
		SQL:      "SELECT 1",
		Parameters: []config.ParamConfig{
			{Name: "required_str", Type: "string", Required: true},
			{Name: "optional_str", Type: "string", Required: false, Default: "default_val"},
			{Name: "required_int", Type: "int", Required: true},
			{Name: "optional_int", Type: "int", Required: false},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	tests := []struct {
		name      string
		url       string
		wantErr   bool
		wantCount int
	}{
		{
			name:      "all required provided",
			url:       "/test?required_str=hello&required_int=42",
			wantErr:   false,
			wantCount: 3, // required_str, required_int, optional_str (default)
		},
		{
			name:      "missing required",
			url:       "/test?required_str=hello",
			wantErr:   true,
			wantCount: 0,
		},
		{
			name:      "all params provided",
			url:       "/test?required_str=hello&required_int=42&optional_str=custom&optional_int=99",
			wantErr:   false,
			wantCount: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			params, err := handler.parseParameters(req)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(params) != tt.wantCount {
				t.Errorf("expected %d params, got %d: %v", tt.wantCount, len(params), params)
			}
		})
	}
}

// TestHandler_ServeHTTP_EmptyResult returns success with count=0 for no matching rows
func TestHandler_ServeHTTP_EmptyResult(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_user",
		Database: "test",
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

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/user?id=999", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.Success {
		t.Error("expected success=true")
	}

	if resp.Count != 0 {
		t.Errorf("expected count=0, got %d", resp.Count)
	}
}

// TestHandler_ServeHTTP_SQLError returns 500 for queries against non-existent tables
func TestHandler_ServeHTTP_SQLError(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "bad_query",
		Database: "test",
		Path:     "/api/bad",
		Method:   "GET",
		SQL:      "SELECT * FROM nonexistent_table",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/bad", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Success {
		t.Error("expected success=false")
	}
}

// TestHandler_ServeHTTP_DateTimeParam tests datetime parameter parsing and SQL binding
func TestHandler_ServeHTTP_DateTimeParam(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	// Add datetime column
	driver, _ := manager.Get("test")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE events (
			id INTEGER PRIMARY KEY,
			name TEXT,
			event_time DATETIME
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create events table: %v", err)
	}

	_, err = driver.Query(ctx, sessCfg,
		"INSERT INTO events (name, event_time) VALUES (@name, @time)",
		map[string]any{"name": "test", "time": time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	queryCfg := config.QueryConfig{
		Name:     "get_events",
		Database: "test",
		Path:     "/api/events",
		Method:   "GET",
		SQL:      "SELECT * FROM events WHERE event_time >= @since",
		Parameters: []config.ParamConfig{
			{Name: "since", Type: "datetime", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/events?since=2024-01-01", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 1 {
		t.Errorf("expected 1 event, got %d", resp.Count)
	}
}

// TestGenerateRequestID validates unique 16-char hex IDs are generated
func TestGenerateRequestID(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateRequestID()
		if len(id) != 16 { // 8 bytes = 16 hex chars
			t.Errorf("expected 16 char ID, got %d: %s", len(id), id)
		}
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

// TestGetOrGenerateRequestID checks header extraction priority and fallback generation
func TestGetOrGenerateRequestID(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "test-123")
	id := getOrGenerateRequestID(req)
	if id != "test-123" {
		t.Errorf("expected test-123, got %s", id)
	}

	// Test X-Correlation-ID header
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Correlation-ID", "corr-456")
	id = getOrGenerateRequestID(req)
	if id != "corr-456" {
		t.Errorf("expected corr-456, got %s", id)
	}

	// Test X-Request-ID takes priority
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "req-id")
	req.Header.Set("X-Correlation-ID", "corr-id")
	id = getOrGenerateRequestID(req)
	if id != "req-id" {
		t.Errorf("expected req-id, got %s", id)
	}

	// Test generation when no header
	req = httptest.NewRequest("GET", "/", nil)
	id = getOrGenerateRequestID(req)
	if len(id) != 16 {
		t.Errorf("expected 16 char generated ID, got %d: %s", len(id), id)
	}
}

// TestSanitizeHeaderValue tests header value sanitization for security
func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal value",
			input:    "test-request-123",
			expected: "test-request-123",
		},
		{
			name:     "newline injection",
			input:    "test\nX-Injected: malicious",
			expected: "testX-Injected: malicious",
		},
		{
			name:     "carriage return injection",
			input:    "test\rX-Injected: malicious",
			expected: "testX-Injected: malicious",
		},
		{
			name:     "CRLF injection",
			input:    "test\r\nX-Injected: malicious",
			expected: "testX-Injected: malicious",
		},
		{
			name:     "null byte",
			input:    "test\x00value",
			expected: "testvalue",
		},
		{
			name:     "tab character",
			input:    "test\tvalue",
			expected: "testvalue",
		},
		{
			name:     "DEL character",
			input:    "test\x7Fvalue",
			expected: "testvalue",
		},
		{
			name:     "very long value truncated",
			input:    strings.Repeat("a", 200),
			expected: strings.Repeat("a", 128),
		},
		{
			name:     "empty value",
			input:    "",
			expected: "",
		},
		{
			name:     "unicode preserved",
			input:    "test-\u4e2d\u6587-value",
			expected: "test-\u4e2d\u6587-value",
		},
		{
			name:     "mixed control chars",
			input:    "a\x00b\nc\rd\te",
			expected: "abcde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeHeaderValue(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestGetOrGenerateRequestID_Sanitizes validates that request IDs from headers are sanitized
func TestGetOrGenerateRequestID_Sanitizes(t *testing.T) {
	// Test that malicious header values are sanitized
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "test\r\nX-Injected: evil")
	id := getOrGenerateRequestID(req)
	if strings.Contains(id, "\n") || strings.Contains(id, "\r") {
		t.Errorf("expected sanitized ID, got %q", id)
	}
	if id != "testX-Injected: evil" {
		t.Errorf("expected 'testX-Injected: evil', got %q", id)
	}
}

// TestHandler_ServeHTTP_JSONBody tests JSON body parsing for POST endpoints
func TestHandler_ServeHTTP_JSONBody(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_user",
		Database: "test",
		Path:     "/api/user",
		Method:   "POST",
		SQL:      "SELECT * FROM users WHERE status = @status",
		Parameters: []config.ParamConfig{
			{Name: "status", Type: "string", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	// Test JSON body parsing
	jsonBody := `{"status": "active"}`
	req := httptest.NewRequest("POST", "/api/user", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 2 {
		t.Errorf("expected 2 active users, got %d", resp.Count)
	}
}

// TestHandler_ServeHTTP_JSONBody_RejectsNestedForNonJSONType tests that nested JSON is rejected for non-json types
func TestHandler_ServeHTTP_JSONBody_RejectsNestedForNonJSONType(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "test",
		Database: "test",
		Path:     "/api/test",
		Method:   "POST",
		SQL:      "SELECT 1",
		Parameters: []config.ParamConfig{
			{Name: "data", Type: "string", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "nested object for string type",
			body:    `{"data": {"nested": "value"}}`,
			wantErr: "nested objects not supported",
		},
		{
			name:    "array for string type",
			body:    `{"data": [1, 2, 3]}`,
			wantErr: "arrays not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/test", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", w.Code)
			}

			var resp Response
			json.NewDecoder(w.Body).Decode(&resp)

			if !strings.Contains(resp.Error, tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, resp.Error)
			}
		})
	}
}

// TestHandler_ServeHTTP_JSONTypeParam tests json type parameter accepts nested objects
func TestHandler_ServeHTTP_JSONTypeParam(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	// Create a table with JSON column
	driver, _ := manager.Get("test")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE configs (
			id INTEGER PRIMARY KEY,
			name TEXT,
			data TEXT
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create configs table: %v", err)
	}

	queryCfg := config.QueryConfig{
		Name:     "save_config",
		Database: "test",
		Path:     "/api/config",
		Method:   "POST",
		SQL:      "INSERT INTO configs (name, data) VALUES (@name, @data)",
		Parameters: []config.ParamConfig{
			{Name: "name", Type: "string", Required: true},
			{Name: "data", Type: "json", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	// Test with nested JSON object
	jsonBody := `{"name": "test_config", "data": {"key": "value", "nested": {"a": 1}}}`
	req := httptest.NewRequest("POST", "/api/config", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
}

// TestHandler_ServeHTTP_ArrayTypeParam tests array type parameters (int[], string[], etc.)
func TestHandler_ServeHTTP_ArrayTypeParam(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	// SQLite with json_each for array parameter
	queryCfg := config.QueryConfig{
		Name:     "get_users_by_ids",
		Database: "test",
		Path:     "/api/users/batch",
		Method:   "POST",
		SQL:      "SELECT * FROM users WHERE id IN (SELECT value FROM json_each(@ids))",
		Parameters: []config.ParamConfig{
			{Name: "ids", Type: "int[]", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	// Test with JSON body containing int array
	jsonBody := `{"ids": [1, 2]}`
	req := httptest.NewRequest("POST", "/api/users/batch", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Should return users with id 1 and 2 (Alice and Bob)
	if resp.Count != 2 {
		t.Errorf("expected 2 users, got %d", resp.Count)
	}
}

// TestHandler_ServeHTTP_ArrayTypeParam_InvalidElement tests array type rejects wrong element types
func TestHandler_ServeHTTP_ArrayTypeParam_InvalidElement(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_users_by_ids",
		Database: "test",
		Path:     "/api/users/batch",
		Method:   "POST",
		SQL:      "SELECT * FROM users WHERE id IN (SELECT value FROM json_each(@ids))",
		Parameters: []config.ParamConfig{
			{Name: "ids", Type: "int[]", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	// Test with wrong element type (strings instead of ints)
	jsonBody := `{"ids": ["not", "integers"]}`
	req := httptest.NewRequest("POST", "/api/users/batch", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Success {
		t.Error("expected error for wrong element type")
	}

	if !strings.Contains(resp.Error, "expected integer") {
		t.Errorf("expected error about integer type, got: %s", resp.Error)
	}
}

// TestHandler_ServeHTTP_StringArrayParam tests string[] type parameter
func TestHandler_ServeHTTP_StringArrayParam(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	queryCfg := config.QueryConfig{
		Name:     "get_users_by_status",
		Database: "test",
		Path:     "/api/users/filter",
		Method:   "POST",
		SQL:      "SELECT * FROM users WHERE status IN (SELECT value FROM json_each(@statuses))",
		Parameters: []config.ParamConfig{
			{Name: "statuses", Type: "string[]", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	jsonBody := `{"statuses": ["active", "inactive"]}`
	req := httptest.NewRequest("POST", "/api/users/filter", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Should return all 3 users (2 active + 1 inactive)
	if resp.Count != 3 {
		t.Errorf("expected 3 users, got %d", resp.Count)
	}
}

// TestConvertJSONValue tests JSON value type conversion
func TestConvertJSONValue(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		typeName  string
		wantValue any
		wantErr   bool
	}{
		{"float64 to int", float64(42), "int", 42, false},
		{"string to int", "42", "int", 42, false},
		{"bool to int", true, "int", nil, true},
		{"float64 to float", 3.14, "float", 3.14, false},
		{"bool to bool", true, "bool", true, false},
		{"string to bool", "true", "bool", true, false},
		{"string to string", "hello", "string", "hello", false},
		{"float64 to string", float64(42), "string", "42", false},
		{"bool to string", true, "string", "true", false},
		{"nil to string", nil, "string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertJSONValue(tt.value, tt.typeName)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.wantValue {
				t.Errorf("expected %v (%T), got %v (%T)", tt.wantValue, tt.wantValue, result, result)
			}
		})
	}
}

// TestConvertJSONValue_JSONType tests json type serializes to JSON string
func TestConvertJSONValue_JSONType(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		wantJSON  string
	}{
		{"object", map[string]any{"key": "value"}, `{"key":"value"}`},
		{"array", []any{1.0, 2.0, 3.0}, `[1,2,3]`},
		{"nested", map[string]any{"a": map[string]any{"b": 1.0}}, `{"a":{"b":1}}`},
		{"string", "hello", `"hello"`},
		{"number", float64(42), `42`},
		{"bool", true, `true`},
		{"null", nil, `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertJSONValue(tt.value, "json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			strResult, ok := result.(string)
			if !ok {
				t.Fatalf("expected string result, got %T", result)
			}

			if strResult != tt.wantJSON {
				t.Errorf("expected %s, got %s", tt.wantJSON, strResult)
			}
		})
	}
}

// TestConvertJSONValue_ArrayTypes tests array types serialize to JSON array string
func TestConvertJSONValue_ArrayTypes(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		typeName string
		wantJSON string
		wantErr  bool
	}{
		{"int array", []any{float64(1), float64(2), float64(3)}, "int[]", `[1,2,3]`, false},
		{"string array", []any{"a", "b", "c"}, "string[]", `["a","b","c"]`, false},
		{"float array", []any{1.5, 2.5}, "float[]", `[1.5,2.5]`, false},
		{"bool array", []any{true, false}, "bool[]", `[true,false]`, false},
		{"not an array", "not array", "int[]", "", true},
		{"wrong element type", []any{"a", "b"}, "int[]", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertJSONValue(tt.value, tt.typeName)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			strResult, ok := result.(string)
			if !ok {
				t.Fatalf("expected string result, got %T", result)
			}

			if strResult != tt.wantJSON {
				t.Errorf("expected %s, got %s", tt.wantJSON, strResult)
			}
		})
	}
}

// TestConvertValue_JSONType tests json type from query string
func TestConvertValue_JSONType(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantJSON string
		wantErr  bool
	}{
		{"object", `{"key":"value"}`, `{"key":"value"}`, false},
		{"array", `[1,2,3]`, `[1,2,3]`, false},
		{"string", `"hello"`, `"hello"`, false},
		{"number", `42`, `42`, false},
		{"invalid json", `{not valid`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertValue(tt.value, "json")

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			strResult, ok := result.(string)
			if !ok {
				t.Fatalf("expected string result, got %T", result)
			}

			if strResult != tt.wantJSON {
				t.Errorf("expected %s, got %s", tt.wantJSON, strResult)
			}
		})
	}
}

// TestConvertValue_ArrayTypes tests array types from query string
func TestConvertValue_ArrayTypes(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		typeName string
		wantJSON string
		wantErr  bool
	}{
		{"int array", `[1,2,3]`, "int[]", `[1,2,3]`, false},
		{"string array", `["a","b","c"]`, "string[]", `["a","b","c"]`, false},
		{"float array", `[1.5,2.5]`, "float[]", `[1.5,2.5]`, false},
		{"bool array", `[true,false]`, "bool[]", `[true,false]`, false},
		{"not json array", `not an array`, "int[]", "", true},
		{"wrong element type", `["a","b"]`, "int[]", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertValue(tt.value, tt.typeName)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			strResult, ok := result.(string)
			if !ok {
				t.Fatalf("expected string result, got %T", result)
			}

			if strResult != tt.wantJSON {
				t.Errorf("expected %s, got %s", tt.wantJSON, strResult)
			}
		})
	}
}

// TestValidateArrayElements tests array element validation
func TestValidateArrayElements(t *testing.T) {
	tests := []struct {
		name     string
		arr      []any
		baseType string
		wantLen  int
		wantErr  bool
	}{
		{"int array", []any{float64(1), float64(2)}, "int", 2, false},
		{"string array", []any{"a", "b"}, "string", 2, false},
		{"float array", []any{1.5, 2.5}, "float", 2, false},
		{"bool array", []any{true, false}, "bool", 2, false},
		{"mixed int fails", []any{float64(1), "two"}, "int", 0, true},
		{"mixed string fails", []any{"a", float64(2)}, "string", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateArrayElements(tt.arr, tt.baseType)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != tt.wantLen {
				t.Errorf("expected %d elements, got %d", tt.wantLen, len(result))
			}
		})
	}
}

// TestParseJSONColumns tests parsing JSON string columns into objects
func TestParseJSONColumns(t *testing.T) {
	tests := []struct {
		name    string
		results []map[string]any
		columns []string
		wantErr bool
		check   func(t *testing.T, results []map[string]any)
	}{
		{
			name: "parses JSON object column",
			results: []map[string]any{
				{"id": 1, "data": `{"theme":"dark","count":42}`},
			},
			columns: []string{"data"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				data, ok := results[0]["data"].(map[string]any)
				if !ok {
					t.Fatalf("expected data to be map, got %T", results[0]["data"])
				}
				if data["theme"] != "dark" {
					t.Errorf("expected theme=dark, got %v", data["theme"])
				}
				if data["count"] != float64(42) {
					t.Errorf("expected count=42, got %v", data["count"])
				}
			},
		},
		{
			name: "parses JSON array column",
			results: []map[string]any{
				{"id": 1, "tags": `["a","b","c"]`},
			},
			columns: []string{"tags"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				tags, ok := results[0]["tags"].([]any)
				if !ok {
					t.Fatalf("expected tags to be array, got %T", results[0]["tags"])
				}
				if len(tags) != 3 {
					t.Errorf("expected 3 tags, got %d", len(tags))
				}
			},
		},
		{
			name: "parses nested JSON",
			results: []map[string]any{
				{"id": 1, "config": `{"db":{"host":"localhost","port":5432}}`},
			},
			columns: []string{"config"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				config, ok := results[0]["config"].(map[string]any)
				if !ok {
					t.Fatalf("expected config to be map, got %T", results[0]["config"])
				}
				db, ok := config["db"].(map[string]any)
				if !ok {
					t.Fatalf("expected db to be map, got %T", config["db"])
				}
				if db["host"] != "localhost" {
					t.Errorf("expected host=localhost, got %v", db["host"])
				}
			},
		},
		{
			name: "skips non-existent columns",
			results: []map[string]any{
				{"id": 1, "name": "test"},
			},
			columns: []string{"data"}, // doesn't exist
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				// Should not modify anything
				if results[0]["name"] != "test" {
					t.Errorf("name should remain test")
				}
			},
		},
		{
			name: "skips non-string values",
			results: []map[string]any{
				{"id": 1, "count": 42}, // int, not string
			},
			columns: []string{"count"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				// Should remain as int
				if results[0]["count"] != 42 {
					t.Errorf("count should remain 42")
				}
			},
		},
		{
			name: "skips empty strings",
			results: []map[string]any{
				{"id": 1, "data": ""},
			},
			columns: []string{"data"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				if results[0]["data"] != "" {
					t.Errorf("data should remain empty string")
				}
			},
		},
		{
			name: "fails on invalid JSON",
			results: []map[string]any{
				{"id": 1, "data": `{invalid json}`},
			},
			columns: []string{"data"},
			wantErr: true,
			check:   nil,
		},
		{
			name: "parses multiple columns",
			results: []map[string]any{
				{"id": 1, "meta": `{"a":1}`, "tags": `["x"]`},
			},
			columns: []string{"meta", "tags"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				meta, ok := results[0]["meta"].(map[string]any)
				if !ok {
					t.Fatalf("expected meta to be map")
				}
				if meta["a"] != float64(1) {
					t.Errorf("expected a=1")
				}
				tags, ok := results[0]["tags"].([]any)
				if !ok {
					t.Fatalf("expected tags to be array")
				}
				if len(tags) != 1 {
					t.Errorf("expected 1 tag")
				}
			},
		},
		{
			name: "parses multiple rows",
			results: []map[string]any{
				{"id": 1, "data": `{"x":1}`},
				{"id": 2, "data": `{"x":2}`},
			},
			columns: []string{"data"},
			wantErr: false,
			check: func(t *testing.T, results []map[string]any) {
				for i, row := range results {
					data, ok := row["data"].(map[string]any)
					if !ok {
						t.Fatalf("row %d: expected map", i)
					}
					if data["x"] != float64(i+1) {
						t.Errorf("row %d: expected x=%d", i, i+1)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseJSONColumns(tt.results, tt.columns)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, tt.results)
			}
		})
	}
}

// TestHandler_ServeHTTP_JSONColumns tests json_columns config parses JSON in response
func TestHandler_ServeHTTP_JSONColumns(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	// Create table with JSON column
	driver, _ := manager.Get("test")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE configs (
			id INTEGER PRIMARY KEY,
			name TEXT,
			data TEXT
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create configs table: %v", err)
	}

	// Insert JSON data
	_, err = driver.Query(ctx, sessCfg,
		"INSERT INTO configs (name, data) VALUES (@name, @data)",
		map[string]any{"name": "settings", "data": `{"theme":"dark","notifications":{"email":true}}`},
	)
	if err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	queryCfg := config.QueryConfig{
		Name:        "get_config",
		Database:    "test",
		Path:        "/api/config",
		Method:      "GET",
		SQL:         "SELECT id, name, data FROM configs WHERE name = @name",
		JSONColumns: []string{"data"}, // Parse this column
		Parameters: []config.ParamConfig{
			{Name: "name", Type: "string", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/config?name=settings", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Count != 1 {
		t.Fatalf("expected 1 row, got %d", resp.Count)
	}

	// Check that data was parsed as JSON object, not string
	rows, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected Data to be array, got %T", resp.Data)
	}

	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("expected row to be map, got %T", rows[0])
	}

	data, ok := row["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be parsed JSON object, got %T (value: %v)", row["data"], row["data"])
	}

	if data["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %v", data["theme"])
	}

	// Check nested object
	notifications, ok := data["notifications"].(map[string]any)
	if !ok {
		t.Fatalf("expected notifications to be map, got %T", data["notifications"])
	}

	if notifications["email"] != true {
		t.Errorf("expected notifications.email=true, got %v", notifications["email"])
	}
}

// TestHandler_ServeHTTP_JSONColumns_WithoutConfig tests default behavior (no json_columns)
func TestHandler_ServeHTTP_JSONColumns_WithoutConfig(t *testing.T) {
	manager := createTestManager(t)
	defer manager.Close()

	// Create table with JSON column
	driver, _ := manager.Get("test")
	ctx := context.Background()
	sessCfg := config.SessionConfig{}

	_, err := driver.Query(ctx, sessCfg, `
		CREATE TABLE configs2 (
			id INTEGER PRIMARY KEY,
			data TEXT
		)
	`, nil)
	if err != nil {
		t.Fatalf("failed to create configs2 table: %v", err)
	}

	// Insert JSON data
	_, err = driver.Query(ctx, sessCfg,
		"INSERT INTO configs2 (data) VALUES (@data)",
		map[string]any{"data": `{"key":"value"}`},
	)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// NO json_columns configured - should return as string
	queryCfg := config.QueryConfig{
		Name:     "get_config2",
		Database: "test",
		Path:     "/api/config2",
		Method:   "GET",
		SQL:      "SELECT data FROM configs2",
		// JSONColumns not set
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	handler := New(manager, nil, queryCfg, serverCfg)

	req := httptest.NewRequest("GET", "/api/config2", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	rows := resp.Data.([]any)
	row := rows[0].(map[string]any)

	// Without json_columns, data should be a string
	dataStr, ok := row["data"].(string)
	if !ok {
		t.Fatalf("expected data to be string (not parsed), got %T", row["data"])
	}

	if dataStr != `{"key":"value"}` {
		t.Errorf("expected JSON string, got %s", dataStr)
	}
}
