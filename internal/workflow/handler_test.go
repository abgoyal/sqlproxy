package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"
	"time"

	"sql-proxy/internal/workflow/step"
)

func TestNewHTTPHandler(t *testing.T) {
	exec := &Executor{}
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}

	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "1.0.0", "2024-01-15", nil)

	if handler.executor != exec {
		t.Error("executor not set")
	}
	if handler.workflow != wf {
		t.Error("workflow not set")
	}
	if handler.trigger != trigger {
		t.Error("trigger not set")
	}
	if handler.version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", handler.version)
	}
	if handler.buildTime != "2024-01-15" {
		t.Errorf("buildTime = %q, want 2024-01-15", handler.buildTime)
	}
}

func TestHTTPHandler_ServeHTTP_MethodNotAllowed(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "POST"}}

	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHTTPHandler_ServeHTTP_Success(t *testing.T) {
	logger := &testLogger{}
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{{"id": 1}}}, nil
		},
	}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{
				Name:     "query1",
				Type:     "query",
				Database: "testdb",
				SQL:      "SELECT 1",
			},
			{
				Name:       "respond",
				Type:       "response",
				StatusCode: 200,
				Template:   `{"success": true}`,
			},
		},
	})
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}

	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "1.0.0", "", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Check headers
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID should be set")
	}
	if rec.Header().Get("X-Server-Version") != "1.0.0" {
		t.Errorf("X-Server-Version = %q, want 1.0.0", rec.Header().Get("X-Server-Version"))
	}
}

func TestHTTPHandler_ServeHTTP_VersionWithBuildTime(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{
				Name:       "respond",
				Type:       "response",
				StatusCode: 200,
				Template:   `{}`,
			},
		},
	})
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}

	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "1.0.0", "2024-01-15", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	expected := "1.0.0 (built 2024-01-15)"
	if rec.Header().Get("X-Server-Version") != expected {
		t.Errorf("X-Server-Version = %q, want %q", rec.Header().Get("X-Server-Version"), expected)
	}
}

func TestHTTPHandler_ServeHTTP_RequestID_FromHeader(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-request-id")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "my-request-id" {
		t.Errorf("X-Request-ID = %q, want my-request-id", rec.Header().Get("X-Request-ID"))
	}
}

func TestHTTPHandler_ServeHTTP_CorrelationID(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Correlation-ID", "correlation-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "correlation-123" {
		t.Errorf("X-Request-ID = %q, want correlation-123", rec.Header().Get("X-Request-ID"))
	}
}

func TestHTTPHandler_ParseParameters_QueryString(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "GET",
			Parameters: []ParamConfig{
				{Name: "status", Type: "string", Required: true},
				{Name: "limit", Type: "int", Default: "10"},
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test?status=active", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHTTPHandler_ParseParameters_MissingRequired(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "GET",
			Parameters: []ParamConfig{
				{Name: "required_param", Type: "string", Required: true},
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test", nil) // No parameters
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "missing required parameter") {
		t.Errorf("body should contain error message, got: %s", body)
	}
}

func TestHTTPHandler_ParseParameters_JSONBody(t *testing.T) {
	logger := &testLogger{}
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "POST",
			Parameters: []ParamConfig{
				{Name: "name", Type: "string", Required: true},
				{Name: "count", Type: "int", Required: true},
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	body := `{"name": "test", "count": 42}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHTTPHandler_ParseParameters_InvalidJSON(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name:  "test",
		Steps: []StepConfig{},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{Method: "POST"},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	body := `{invalid json`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHTTPHandler_ParseParameters_TypeConversion(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "GET",
			Parameters: []ParamConfig{
				{Name: "count", Type: "int"},
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test?count=notanumber", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHTTPHandler_WorkflowError_DefaultResponse(t *testing.T) {
	logger := &testLogger{}
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return nil, testError{msg: "database error"}
		},
	}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "query1", Type: "query", Database: "db", SQL: "SELECT 1"},
			// No response step
		},
	})
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHTTPHandler_NoResponse_EmptySuccess(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name:  "test",
		Steps: []StepConfig{}, // No steps at all
	})
	trigger := &CompiledTrigger{Config: &TriggerConfig{Method: "GET"}}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should return 200 with success response
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetOrGenerateRequestID(t *testing.T) {
	t.Run("from X-Request-ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", "test-id-123")

		id := getOrGenerateRequestID(req)
		if id != "test-id-123" {
			t.Errorf("id = %q, want test-id-123", id)
		}
	})

	t.Run("from X-Correlation-ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Correlation-ID", "corr-id-456")

		id := getOrGenerateRequestID(req)
		if id != "corr-id-456" {
			t.Errorf("id = %q, want corr-id-456", id)
		}
	})

	t.Run("X-Request-ID takes precedence", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", "request-id")
		req.Header.Set("X-Correlation-ID", "correlation-id")

		id := getOrGenerateRequestID(req)
		if id != "request-id" {
			t.Errorf("id = %q, want request-id", id)
		}
	})

	t.Run("generate if not present", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		id := getOrGenerateRequestID(req)
		if id == "" {
			t.Error("id should be generated")
		}
	})
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	if id1 == "" {
		t.Error("id should not be empty")
	}
	if id1 == id2 {
		t.Error("ids should be unique")
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal value", "test-value-123", "test-value-123"},
		{"with spaces", "test value", "test value"},
		{"strips control chars", "test\x00value", "testvalue"},
		{"strips newlines", "test\nvalue", "testvalue"},
		{"truncates long values", strings.Repeat("a", 200), strings.Repeat("a", 128)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveClientIP(t *testing.T) {
	t.Run("trust proxy headers enabled", func(t *testing.T) {
		tests := []struct {
			name    string
			headers map[string]string
			remote  string
			want    string
		}{
			{
				name:    "from X-Forwarded-For",
				headers: map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
				remote:  "127.0.0.1:8080",
				want:    "1.2.3.4",
			},
			{
				name:    "from X-Real-IP",
				headers: map[string]string{"X-Real-IP": "9.8.7.6"},
				remote:  "127.0.0.1:8080",
				want:    "9.8.7.6",
			},
			{
				name:    "fallback to RemoteAddr",
				headers: map[string]string{},
				remote:  "192.168.1.1:12345",
				want:    "192.168.1.1",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/", nil)
				for k, v := range tt.headers {
					req.Header.Set(k, v)
				}
				req.RemoteAddr = tt.remote

				got := resolveClientIP(req, true)
				if got != tt.want {
					t.Errorf("resolveClientIP(trustProxy=true) = %q, want %q", got, tt.want)
				}
			})
		}
	})

	t.Run("trust proxy headers disabled", func(t *testing.T) {
		tests := []struct {
			name    string
			headers map[string]string
			remote  string
			want    string
		}{
			{
				name:    "ignores X-Forwarded-For",
				headers: map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
				remote:  "127.0.0.1:8080",
				want:    "127.0.0.1",
			},
			{
				name:    "ignores X-Real-IP",
				headers: map[string]string{"X-Real-IP": "9.8.7.6"},
				remote:  "127.0.0.1:8080",
				want:    "127.0.0.1",
			},
			{
				name:    "uses RemoteAddr",
				headers: map[string]string{},
				remote:  "192.168.1.1:12345",
				want:    "192.168.1.1",
			},
			{
				name:    "RemoteAddr without port",
				headers: map[string]string{},
				remote:  "192.168.1.1",
				want:    "192.168.1.1",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/", nil)
				for k, v := range tt.headers {
					req.Header.Set(k, v)
				}
				req.RemoteAddr = tt.remote

				got := resolveClientIP(req, false)
				if got != tt.want {
					t.Errorf("resolveClientIP(trustProxy=false) = %q, want %q", got, tt.want)
				}
			})
		}
	})
}

func TestDBManagerAdapter(t *testing.T) {
	called := false
	queryFunc := func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
		called = true
		if database != "testdb" {
			t.Errorf("database = %q, want testdb", database)
		}
		if sql != "SELECT 1" {
			t.Errorf("sql = %q, want 'SELECT 1'", sql)
		}
		return &step.QueryResult{Rows: []map[string]any{{"result": 1}}}, nil
	}

	adapter := NewDBManagerAdapter(queryFunc)
	results, err := adapter.ExecuteQuery(context.Background(), "testdb", "SELECT 1", nil, step.QueryOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("queryFunc was not called")
	}
	if len(results.Rows) != 1 {
		t.Errorf("len(results.Rows) = %d, want 1", len(results.Rows))
	}
}

func TestLoggerAdapter(t *testing.T) {
	var lastMsg string
	var lastFields map[string]any

	mockLogger := &mockLoggerInterface{
		debugFunc: func(msg string, fields map[string]any) {
			lastMsg = msg
			lastFields = fields
		},
		infoFunc: func(msg string, fields map[string]any) {
			lastMsg = msg
			lastFields = fields
		},
		warnFunc: func(msg string, fields map[string]any) {
			lastMsg = msg
			lastFields = fields
		},
		errorFunc: func(msg string, fields map[string]any) {
			lastMsg = msg
			lastFields = fields
		},
	}

	adapter := NewLoggerAdapter(mockLogger)

	adapter.Debug("debug_msg", map[string]any{"key": "debug"})
	if lastMsg != "debug_msg" || lastFields["key"] != "debug" {
		t.Errorf("Debug not forwarded correctly")
	}

	adapter.Info("info_msg", map[string]any{"key": "info"})
	if lastMsg != "info_msg" || lastFields["key"] != "info" {
		t.Errorf("Info not forwarded correctly")
	}

	adapter.Warn("warn_msg", map[string]any{"key": "warn"})
	if lastMsg != "warn_msg" || lastFields["key"] != "warn" {
		t.Errorf("Warn not forwarded correctly")
	}

	adapter.Error("error_msg", map[string]any{"key": "error"})
	if lastMsg != "error_msg" || lastFields["key"] != "error" {
		t.Errorf("Error not forwarded correctly")
	}
}

// mockLoggerInterface implements LoggerInterface for testing
type mockLoggerInterface struct {
	debugFunc func(msg string, fields map[string]any)
	infoFunc  func(msg string, fields map[string]any)
	warnFunc  func(msg string, fields map[string]any)
	errorFunc func(msg string, fields map[string]any)
}

func (m *mockLoggerInterface) Debug(msg string, fields map[string]any) {
	if m.debugFunc != nil {
		m.debugFunc(msg, fields)
	}
}

func (m *mockLoggerInterface) Info(msg string, fields map[string]any) {
	if m.infoFunc != nil {
		m.infoFunc(msg, fields)
	}
}

func (m *mockLoggerInterface) Warn(msg string, fields map[string]any) {
	if m.warnFunc != nil {
		m.warnFunc(msg, fields)
	}
}

func (m *mockLoggerInterface) Error(msg string, fields map[string]any) {
	if m.errorFunc != nil {
		m.errorFunc(msg, fields)
	}
}

func TestHTTPHandler_ParseParameters_NestedObject(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name:  "test",
		Steps: []StepConfig{},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "POST",
			Parameters: []ParamConfig{
				{Name: "data", Type: "string"}, // Not json type
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	body := `{"data": {"nested": "object"}}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should fail because nested objects require type: json
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHTTPHandler_ParseParameters_JSONType(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "POST",
			Parameters: []ParamConfig{
				{Name: "data", Type: "json"},
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	body := `{"data": {"nested": "object"}}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHTTPHandler_ParseParameters_ArrayType(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "POST",
			Parameters: []ParamConfig{
				{Name: "ids", Type: "int[]"},
			},
		},
	}
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	body := `{"ids": [1, 2, 3]}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// mockTriggerCache implements TriggerCache for testing.
type mockTriggerCache struct {
	data map[string]struct {
		body       []byte
		statusCode int
	}
}

func newMockTriggerCache() *mockTriggerCache {
	return &mockTriggerCache{
		data: make(map[string]struct {
			body       []byte
			statusCode int
		}),
	}
}

func (m *mockTriggerCache) Get(workflow, key string) ([]byte, int, bool) {
	fullKey := workflow + ":" + key
	entry, ok := m.data[fullKey]
	if !ok {
		return nil, 0, false
	}
	return entry.body, entry.statusCode, true
}

func (m *mockTriggerCache) Set(workflow, key string, body []byte, statusCode int, ttl time.Duration) bool {
	fullKey := workflow + ":" + key
	m.data[fullKey] = struct {
		body       []byte
		statusCode int
	}{body: body, statusCode: statusCode}
	return true
}

func TestHTTPHandler_TriggerCache_Hit(t *testing.T) {
	// Pre-populate cache
	cache := newMockTriggerCache()
	cachedBody := []byte(`{"success":true,"data":{"cached":true}}`)
	cache.Set("test_workflow", "response:user:42", cachedBody, 200, 0)

	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create trigger with cache key template
	cacheKeyTmpl := template.Must(template.New("cache_key").Parse("response:user:{{.trigger.params.id}}"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
	}
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "GET",
			Parameters: []ParamConfig{
				{Name: "id", Type: "int", Required: true},
			},
			Cache: &CacheConfig{Enabled: true, Key: "response:user:{{.trigger.params.id}}", TTLSec: 300},
		},
		CacheKey: cacheKeyTmpl,
	}

	handler := NewHTTPHandler(exec, wf, trigger, nil, cache, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test?id=42", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should return cached response
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("X-Cache = %q, want HIT", rec.Header().Get("X-Cache"))
	}
	if rec.Body.String() != string(cachedBody) {
		t.Errorf("body = %q, want %q", rec.Body.String(), string(cachedBody))
	}
}

func TestHTTPHandler_TriggerCache_Miss(t *testing.T) {
	cache := newMockTriggerCache()
	logger := &testLogger{}

	// Mock DB that returns data
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{{"id": 42, "name": "Alice"}}}, nil
		},
	}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Create trigger with cache key template
	cacheKeyTmpl := template.Must(template.New("cache_key").Parse("response:user:{{.trigger.params.id}}"))
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users"))
	responseTmpl := template.Must(template.New("response").Funcs(TemplateFuncs).Parse(`{"success":true,"data":{{json .steps.query.data}}}`))

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "query", Type: "query", Database: "testdb"},
				SQLTmpl: sqlTmpl,
			},
			{
				Config:       &StepConfig{Name: "response", Type: "response", StatusCode: 200},
				TemplateTmpl: responseTmpl,
			},
		},
	}
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "GET",
			Parameters: []ParamConfig{
				{Name: "id", Type: "int", Required: true},
			},
			Cache: &CacheConfig{Enabled: true, Key: "response:user:{{.trigger.params.id}}", TTLSec: 300},
		},
		CacheKey: cacheKeyTmpl,
	}

	handler := NewHTTPHandler(exec, wf, trigger, nil, cache, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test?id=42", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should execute workflow and return response
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", rec.Header().Get("X-Cache"))
	}

	// Should have cached the response
	cachedBody, cachedStatus, hit := cache.Get("test_workflow", "response:user:42")
	if !hit {
		t.Error("Expected response to be cached")
	}
	if cachedStatus != 200 {
		t.Errorf("cached status = %d, want 200", cachedStatus)
	}
	if len(cachedBody) == 0 {
		t.Error("Expected non-empty cached body")
	}
}

func TestHTTPHandler_TriggerCache_NilCache(t *testing.T) {
	logger := &testLogger{}
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{{"id": 1}}}, nil
		},
	}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Trigger with cache key but no cache provided
	cacheKeyTmpl := template.Must(template.New("cache_key").Parse("response:user:{{.trigger.params.id}}"))
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users"))
	responseTmpl := template.Must(template.New("response").Parse(`{"success":true}`))

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "query", Type: "query", Database: "testdb"},
				SQLTmpl: sqlTmpl,
			},
			{
				Config:       &StepConfig{Name: "response", Type: "response", StatusCode: 200},
				TemplateTmpl: responseTmpl,
			},
		},
	}
	trigger := &CompiledTrigger{
		Config: &TriggerConfig{
			Method: "GET",
			Parameters: []ParamConfig{
				{Name: "id", Type: "int", Required: true},
			},
			Cache: &CacheConfig{Enabled: true, Key: "response:user:{{.trigger.params.id}}"},
		},
		CacheKey: cacheKeyTmpl,
	}

	// Pass nil for cache
	handler := NewHTTPHandler(exec, wf, trigger, nil, nil, false, "", "", nil)

	req := httptest.NewRequest("GET", "/test?id=42", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should still work without cache
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// X-Cache header should not be set when cache is nil
	if rec.Header().Get("X-Cache") != "" {
		t.Errorf("X-Cache = %q, want empty", rec.Header().Get("X-Cache"))
	}
}

func TestFlattenHeaders(t *testing.T) {
	h := http.Header{
		"Content-Type":  []string{"application/json"},
		"Accept":        []string{"text/html", "application/json"},
		"Authorization": []string{"Bearer token"},
	}

	flat := flattenHeaders(h)

	if flat["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", flat["Content-Type"])
	}
	// Multi-value headers return first value
	if flat["Accept"] != "text/html" {
		t.Errorf("Accept = %q, want text/html", flat["Accept"])
	}
	if flat["Authorization"] != "Bearer token" {
		t.Errorf("Authorization = %q, want Bearer token", flat["Authorization"])
	}
}

func TestFlattenQuery(t *testing.T) {
	q := map[string][]string{
		"id":     []string{"123"},
		"filter": []string{"active", "archived"},
	}

	flat := flattenQuery(q)

	if flat["id"] != "123" {
		t.Errorf("id = %q, want 123", flat["id"])
	}
	// Multi-value query params return first value
	if flat["filter"] != "active" {
		t.Errorf("filter = %q, want active", flat["filter"])
	}
}

func TestEvaluateCacheKey_ExpandedContext(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := mustCompile(t, &WorkflowConfig{
		Name: "test",
		Steps: []StepConfig{
			{Name: "respond", Type: "response", Template: `{}`},
		},
	})

	// Create cache key template that uses multiple context fields
	cacheKeyTmpl, err := template.New("cache").Parse("{{.trigger.method}}:{{.trigger.path}}:{{.trigger.client_ip}}:{{.trigger.params.id}}:{{.trigger.headers.Authorization}}:{{.trigger.cookies.session}}")
	if err != nil {
		t.Fatalf("Failed to parse cache key template: %v", err)
	}

	trigger := &CompiledTrigger{
		Config:   &TriggerConfig{Method: "GET"},
		CacheKey: cacheKeyTmpl,
	}

	handler := NewHTTPHandler(exec, wf, trigger, nil, newMockTriggerCache(), false, "", "", nil)

	// Create request with various context values
	req := httptest.NewRequest("GET", "/api/users?id=123", nil)
	req.Header.Set("Authorization", "Bearer abc")
	req.AddCookie(&http.Cookie{Name: "session", Value: "xyz789"})

	params := map[string]any{"id": "123"}
	clientIP := "192.168.1.1"
	cookies := parseCookies(req)
	requestID := "req-test"

	key, err := handler.evaluateCacheKey(cacheKeyTmpl, req, params, clientIP, cookies, requestID)
	if err != nil {
		t.Fatalf("evaluateCacheKey failed: %v", err)
	}

	expected := "GET:/api/users:192.168.1.1:123:Bearer abc:xyz789"
	if key != expected {
		t.Errorf("cache key = %q, want %q", key, expected)
	}
}

func TestParseCookies(t *testing.T) {
	t.Run("single cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})

		cookies := parseCookies(req)
		if cookies["session"] != "abc123" {
			t.Errorf("session = %q, want abc123", cookies["session"])
		}
	})

	t.Run("multiple cookies", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
		req.AddCookie(&http.Cookie{Name: "user", Value: "john"})
		req.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})

		cookies := parseCookies(req)
		if cookies["session"] != "abc123" {
			t.Errorf("session = %q, want abc123", cookies["session"])
		}
		if cookies["user"] != "john" {
			t.Errorf("user = %q, want john", cookies["user"])
		}
		if cookies["theme"] != "dark" {
			t.Errorf("theme = %q, want dark", cookies["theme"])
		}
	})

	t.Run("no cookies", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		cookies := parseCookies(req)
		if len(cookies) != 0 {
			t.Errorf("len(cookies) = %d, want 0", len(cookies))
		}
	})

	t.Run("duplicate cookie names - last wins", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "first"})
		req.AddCookie(&http.Cookie{Name: "session", Value: "second"})

		cookies := parseCookies(req)
		// Go's http.Request.Cookies() returns cookies in order; our loop takes the last value for duplicates
		if cookies["session"] != "second" {
			t.Errorf("duplicate cookie: session = %q, want 'second' (last wins)", cookies["session"])
		}
	})
}

// mustCompile compiles a workflow config and fails the test if compilation fails.
func mustCompile(t *testing.T, cfg *WorkflowConfig) *CompiledWorkflow {
	t.Helper()
	wf, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	return wf
}
