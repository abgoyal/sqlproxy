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
