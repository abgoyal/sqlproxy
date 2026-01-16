package step

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"
)

// mockDBManager implements DBManager for testing.
type mockDBManager struct {
	queryFunc func(ctx context.Context, database, sql string, params map[string]any, opts QueryOptions) (*QueryResult, error)
}

func (m *mockDBManager) ExecuteQuery(ctx context.Context, database, sql string, params map[string]any, opts QueryOptions) (*QueryResult, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, database, sql, params, opts)
	}
	return &QueryResult{}, nil
}

// mockHTTPClient implements HTTPClient for testing.
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.doFunc != nil {
		return m.doFunc(req)
	}
	return nil, nil
}

// mockLogger implements Logger for testing.
type mockLogger struct {
	debugFunc func(msg string, fields map[string]any)
	infoFunc  func(msg string, fields map[string]any)
	warnFunc  func(msg string, fields map[string]any)
	errorFunc func(msg string, fields map[string]any)
}

func (m *mockLogger) Debug(msg string, fields map[string]any) {
	if m.debugFunc != nil {
		m.debugFunc(msg, fields)
	}
}

func (m *mockLogger) Info(msg string, fields map[string]any) {
	if m.infoFunc != nil {
		m.infoFunc(msg, fields)
	}
}

func (m *mockLogger) Warn(msg string, fields map[string]any) {
	if m.warnFunc != nil {
		m.warnFunc(msg, fields)
	}
}

func (m *mockLogger) Error(msg string, fields map[string]any) {
	if m.errorFunc != nil {
		m.errorFunc(msg, fields)
	}
}

// TestQueryStep tests the QueryStep implementation.
func TestQueryStep(t *testing.T) {
	t.Run("Type returns query", func(t *testing.T) {
		step := &QueryStep{}
		if step.Type() != "query" {
			t.Errorf("Type() = %q, want %q", step.Type(), "query")
		}
	})

	t.Run("NewQueryStep creates step with fields", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse("SELECT * FROM users"))
		lockTimeout := 5000
		step := NewQueryStep("test", "mydb", tmpl, "read_committed", &lockTimeout, "low", []string{"data"})

		if step.Name != "test" {
			t.Errorf("Name = %q, want %q", step.Name, "test")
		}
		if step.Database != "mydb" {
			t.Errorf("Database = %q, want %q", step.Database, "mydb")
		}
		if step.Isolation != "read_committed" {
			t.Errorf("Isolation = %q, want %q", step.Isolation, "read_committed")
		}
		if step.LockTimeoutMs == nil || *step.LockTimeoutMs != 5000 {
			t.Errorf("LockTimeoutMs = %v, want 5000", step.LockTimeoutMs)
		}
		if step.DeadlockPriority != "low" {
			t.Errorf("DeadlockPriority = %q, want %q", step.DeadlockPriority, "low")
		}
		if len(step.JSONColumns) != 1 || step.JSONColumns[0] != "data" {
			t.Errorf("JSONColumns = %v, want [data]", step.JSONColumns)
		}
	})

	t.Run("Execute success", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse("SELECT * FROM users WHERE status = @status"))
		step := NewQueryStep("get_users", "testdb", tmpl, "", nil, "", nil)

		dbManager := &mockDBManager{
			queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts QueryOptions) (*QueryResult, error) {
				if database != "testdb" {
					t.Errorf("database = %q, want %q", database, "testdb")
				}
				return &QueryResult{
					Rows: []map[string]any{
						{"id": 1, "name": "Alice"},
						{"id": 2, "name": "Bob"},
					},
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{
				"trigger": map[string]any{
					"params": map[string]any{"status": "active"},
				},
			},
			DBManager: dbManager,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Errorf("Success = false, want true")
		}
		if result.Count != 2 {
			t.Errorf("Count = %d, want 2", result.Count)
		}
		if len(result.Data) != 2 {
			t.Errorf("len(Data) = %d, want 2", len(result.Data))
		}
	})

	t.Run("Execute with template error", func(t *testing.T) {
		// Use Option("missingkey=error") to make template error on missing keys
		tmpl := template.Must(template.New("test").Option("missingkey=error").Parse("SELECT * FROM {{.missing}}"))
		step := NewQueryStep("test", "testdb", tmpl, "", nil, "", nil)

		data := ExecutionData{
			TemplateData: map[string]any{},
			DBManager:    &mockDBManager{},
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error instead of result with Error field: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil {
			t.Errorf("Error = nil, want template error")
		}
	})

	t.Run("Execute with database error", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse("SELECT * FROM users"))
		step := NewQueryStep("test", "testdb", tmpl, "", nil, "", nil)

		dbManager := &mockDBManager{
			queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts QueryOptions) (*QueryResult, error) {
				return nil, errors.New("database connection failed")
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			DBManager:    dbManager,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error instead of result with Error field: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "database connection failed") {
			t.Errorf("Error = %v, want database error", result.Error)
		}
	})

	t.Run("Execute with logger", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse("SELECT * FROM users"))
		step := NewQueryStep("test_step", "testdb", tmpl, "", nil, "", nil)

		logged := false
		logger := &mockLogger{
			debugFunc: func(msg string, fields map[string]any) {
				if msg == "query_step_executed" {
					logged = true
					if fields["step"] != "test_step" {
						t.Errorf("logged step = %v, want test_step", fields["step"])
					}
				}
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			DBManager: &mockDBManager{
				queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts QueryOptions) (*QueryResult, error) {
					return &QueryResult{Rows: []map[string]any{}}, nil
				},
			},
			Logger: logger,
		}

		_, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !logged {
			t.Errorf("Logger was not called")
		}
	})
}

// TestExtractSQLParams tests the extractSQLParams function.
func TestExtractSQLParams(t *testing.T) {
	t.Run("extract from trigger.params", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE status = @status AND id = @id"
		data := map[string]any{
			"trigger": map[string]any{
				"params": map[string]any{
					"status": "active",
					"id":     42,
				},
			},
		}

		params := extractSQLParams(sql, data)

		if params["status"] != "active" {
			t.Errorf("status = %v, want active", params["status"])
		}
		if params["id"] != 42 {
			t.Errorf("id = %v, want 42", params["id"])
		}
	})

	t.Run("extract from direct data", func(t *testing.T) {
		sql := "SELECT * FROM items WHERE item_id = @item_id"
		data := map[string]any{
			"item_id": 123,
		}

		params := extractSQLParams(sql, data)

		if params["item_id"] != 123 {
			t.Errorf("item_id = %v, want 123", params["item_id"])
		}
	})

	t.Run("trigger.params takes precedence", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE id = @id"
		data := map[string]any{
			"id": 999, // Direct data
			"trigger": map[string]any{
				"params": map[string]any{
					"id": 42, // Should take precedence
				},
			},
		}

		params := extractSQLParams(sql, data)

		if params["id"] != 42 {
			t.Errorf("id = %v, want 42 (from trigger.params)", params["id"])
		}
	})

	t.Run("no params found", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE status = @status"
		data := map[string]any{} // No matching params

		params := extractSQLParams(sql, data)

		if _, ok := params["status"]; ok {
			t.Errorf("status should not be in params")
		}
	})
}

// TestHTTPCallStep tests the HTTPCallStep implementation.
func TestHTTPCallStep(t *testing.T) {
	t.Run("Type returns httpcall", func(t *testing.T) {
		step := &HTTPCallStep{}
		if step.Type() != "httpcall" {
			t.Errorf("Type() = %q, want %q", step.Type(), "httpcall")
		}
	})

	t.Run("NewHTTPCallStep with defaults", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		step := NewHTTPCallStep("test", urlTmpl, "", nil, nil, "", 0, nil)

		if step.Method != "GET" {
			t.Errorf("Method = %q, want %q (default)", step.Method, "GET")
		}
		if step.Parse != "json" {
			t.Errorf("Parse = %q, want %q (default)", step.Parse, "json")
		}
	})

	t.Run("NewHTTPCallStep with custom values", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		retry := &RetryConfig{Enabled: true, MaxAttempts: 5}
		step := NewHTTPCallStep("test", urlTmpl, "POST", nil, nil, "text", 30, retry)

		if step.Method != "POST" {
			t.Errorf("Method = %q, want %q", step.Method, "POST")
		}
		if step.Parse != "text" {
			t.Errorf("Parse = %q, want %q", step.Parse, "text")
		}
		if step.TimeoutSec != 30 {
			t.Errorf("TimeoutSec = %d, want 30", step.TimeoutSec)
		}
		if step.Retry == nil || !step.Retry.Enabled {
			t.Errorf("Retry = %v, want enabled", step.Retry)
		}
	})

	t.Run("Execute success with JSON response", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com/users"))
		step := NewHTTPCallStep("fetch_users", urlTmpl, "GET", nil, nil, "json", 0, nil)

		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				body := `[{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]`
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Errorf("Success = false, want true")
		}
		if result.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want 200", result.StatusCode)
		}
		if result.Count != 2 {
			t.Errorf("Count = %d, want 2", result.Count)
		}
	})

	t.Run("Execute with text parse mode", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com/text"))
		step := NewHTTPCallStep("fetch_text", urlTmpl, "GET", nil, nil, "text", 0, nil)

		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("Hello, World!")),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Errorf("Success = false, want true")
		}
		if len(result.Data) != 1 || result.Data[0]["body"] != "Hello, World!" {
			t.Errorf("Data = %v, want body='Hello, World!'", result.Data)
		}
	})

	t.Run("Execute with form parse mode", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com/form"))
		step := NewHTTPCallStep("fetch_form", urlTmpl, "GET", nil, nil, "form", 0, nil)

		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("name=Alice&age=30")),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Errorf("Success = false, want true")
		}
		if len(result.Data) != 1 {
			t.Errorf("len(Data) = %d, want 1", len(result.Data))
		}
		if result.Data[0]["name"] != "Alice" {
			t.Errorf("name = %v, want Alice", result.Data[0]["name"])
		}
	})

	t.Run("Execute with URL template error", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Option("missingkey=error").Parse("https://api.example.com/{{.missing}}"))
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, nil)

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   &mockHTTPClient{},
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "url template error") {
			t.Errorf("Error = %v, want url template error", result.Error)
		}
	})

	t.Run("Execute with body template error", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		bodyTmpl := template.Must(template.New("body").Option("missingkey=error").Parse(`{"id": {{.missing}}}`))
		step := NewHTTPCallStep("test", urlTmpl, "POST", nil, bodyTmpl, "json", 0, nil)

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   &mockHTTPClient{},
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "body template error") {
			t.Errorf("Error = %v, want body template error", result.Error)
		}
	})

	t.Run("Execute with header template error", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		headerTmpl := template.Must(template.New("auth").Option("missingkey=error").Parse("Bearer {{.missing}}"))
		step := NewHTTPCallStep("test", urlTmpl, "GET", map[string]*template.Template{"Authorization": headerTmpl}, nil, "json", 0, nil)

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   &mockHTTPClient{},
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "header") {
			t.Errorf("Error = %v, want header template error", result.Error)
		}
	})

	t.Run("Execute with HTTP error", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, nil)

		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "connection refused") {
			t.Errorf("Error = %v, want connection error", result.Error)
		}
	})

	t.Run("Execute with non-2xx response", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, nil)

		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 404,
					Body:       io.NopCloser(strings.NewReader(`{"error": "not found"}`)),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false for 404")
		}
		if result.StatusCode != 404 {
			t.Errorf("StatusCode = %d, want 404", result.StatusCode)
		}
	})

	t.Run("Execute sets default Content-Type for body", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		bodyTmpl := template.Must(template.New("body").Parse(`{"name": "test"}`))
		step := NewHTTPCallStep("test", urlTmpl, "POST", nil, bodyTmpl, "json", 0, nil)

		var capturedReq *http.Request
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				capturedReq = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		_, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if capturedReq.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", capturedReq.Header.Get("Content-Type"))
		}
	})

	t.Run("Execute with invalid JSON response", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, nil)

		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("not valid json")),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false for invalid JSON")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "json parse error") {
			t.Errorf("Error = %v, want json parse error", result.Error)
		}
	})
}

// TestNormalizeJSONResponse tests the normalizeJSONResponse function.
func TestNormalizeJSONResponse(t *testing.T) {
	t.Run("array of maps", func(t *testing.T) {
		input := []any{
			map[string]any{"id": 1, "name": "Alice"},
			map[string]any{"id": 2, "name": "Bob"},
		}
		result := normalizeJSONResponse(input)

		if len(result) != 2 {
			t.Errorf("len(result) = %d, want 2", len(result))
		}
		if result[0]["name"] != "Alice" {
			t.Errorf("result[0][name] = %v, want Alice", result[0]["name"])
		}
	})

	t.Run("array of non-maps", func(t *testing.T) {
		input := []any{1, 2, 3}
		result := normalizeJSONResponse(input)

		if len(result) != 3 {
			t.Errorf("len(result) = %d, want 3", len(result))
		}
		if result[0]["value"] != 1 {
			t.Errorf("result[0][value] = %v, want 1", result[0]["value"])
		}
	})

	t.Run("single map", func(t *testing.T) {
		input := map[string]any{"id": 1, "name": "Alice"}
		result := normalizeJSONResponse(input)

		if len(result) != 1 {
			t.Errorf("len(result) = %d, want 1", len(result))
		}
		if result[0]["name"] != "Alice" {
			t.Errorf("result[0][name] = %v, want Alice", result[0]["name"])
		}
	})

	t.Run("scalar value", func(t *testing.T) {
		input := "hello"
		result := normalizeJSONResponse(input)

		if len(result) != 1 {
			t.Errorf("len(result) = %d, want 1", len(result))
		}
		if result[0]["value"] != "hello" {
			t.Errorf("result[0][value] = %v, want hello", result[0]["value"])
		}
	})
}

// TestResponseStep tests the ResponseStep implementation.
func TestResponseStep(t *testing.T) {
	t.Run("Type returns response", func(t *testing.T) {
		step := &ResponseStep{}
		if step.Type() != "response" {
			t.Errorf("Type() = %q, want %q", step.Type(), "response")
		}
	})

	t.Run("NewResponseStep with defaults", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse(`{"status": "ok"}`))
		step := NewResponseStep("test", 0, tmpl, nil, "")

		if step.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want 200 (default)", step.StatusCode)
		}
		if step.ContentType != "application/json" {
			t.Errorf("ContentType = %q, want application/json (default)", step.ContentType)
		}
	})

	t.Run("NewResponseStep with custom values", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse(`<html></html>`))
		step := NewResponseStep("test", 201, tmpl, nil, "text/html")

		if step.StatusCode != 201 {
			t.Errorf("StatusCode = %d, want 201", step.StatusCode)
		}
		if step.ContentType != "text/html" {
			t.Errorf("ContentType = %q, want text/html", step.ContentType)
		}
	})

	t.Run("Execute success", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse(`{"message": "{{.message}}"}`))
		step := NewResponseStep("send_response", 200, tmpl, nil, "application/json")

		recorder := httptest.NewRecorder()
		data := ExecutionData{
			TemplateData:   map[string]any{"message": "Hello, World!"},
			ResponseWriter: recorder,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Errorf("Success = false, want true")
		}
		if result.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want 200", result.StatusCode)
		}

		// Check response
		resp := recorder.Result()
		if resp.StatusCode != 200 {
			t.Errorf("Response status = %d, want 200", resp.StatusCode)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Response Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Hello, World!") {
			t.Errorf("Response body = %q, want to contain 'Hello, World!'", string(body))
		}
	})

	t.Run("Execute without ResponseWriter", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse(`{}`))
		step := NewResponseStep("test", 200, tmpl, nil, "application/json")

		data := ExecutionData{
			TemplateData:   map[string]any{},
			ResponseWriter: nil, // Simulating cron trigger
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "ResponseWriter") {
			t.Errorf("Error = %v, want ResponseWriter error", result.Error)
		}
	})

	t.Run("Execute with template error", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Option("missingkey=error").Parse(`{{.missing}}`))
		step := NewResponseStep("test", 200, tmpl, nil, "application/json")

		recorder := httptest.NewRecorder()
		data := ExecutionData{
			TemplateData:   map[string]any{},
			ResponseWriter: recorder,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() returned error: %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "template error") {
			t.Errorf("Error = %v, want template error", result.Error)
		}
	})

	t.Run("Execute with logger", func(t *testing.T) {
		tmpl := template.Must(template.New("test").Parse(`{}`))
		step := NewResponseStep("test_response", 200, tmpl, nil, "application/json")

		logged := false
		logger := &mockLogger{
			debugFunc: func(msg string, fields map[string]any) {
				if msg == "response_step_executed" {
					logged = true
					if fields["step"] != "test_response" {
						t.Errorf("logged step = %v, want test_response", fields["step"])
					}
				}
			},
		}

		recorder := httptest.NewRecorder()
		data := ExecutionData{
			TemplateData:   map[string]any{},
			ResponseWriter: recorder,
			Logger:         logger,
		}

		_, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !logged {
			t.Errorf("Logger was not called")
		}
	})
}

// TestResponseStepWriteError tests the ResponseStep with a failing ResponseWriter.
func TestResponseStepWriteError(t *testing.T) {
	tmpl := template.Must(template.New("test").Parse(`test`))
	step := NewResponseStep("test", 200, tmpl, nil, "application/json")

	// Create a ResponseWriter that fails on Write
	failingWriter := &failingResponseWriter{
		header: make(http.Header),
	}

	data := ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: failingWriter,
	}

	result, err := step.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "write response error") {
		t.Errorf("Error = %v, want write response error", result.Error)
	}
}

// failingResponseWriter is a ResponseWriter that fails on Write.
type failingResponseWriter struct {
	header http.Header
}

func (f *failingResponseWriter) Header() http.Header {
	return f.header
}

func (f *failingResponseWriter) Write(data []byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *failingResponseWriter) WriteHeader(statusCode int) {}

// TestHTTPCallRetry tests the retry logic in HTTPCallStep.
func TestHTTPCallRetry(t *testing.T) {
	t.Run("retry on 500 error", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		retry := &RetryConfig{
			Enabled:           true,
			MaxAttempts:       3,
			InitialBackoffSec: 0, // No backoff for test speed
		}
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, retry)

		attempts := 0
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				attempts++
				if attempts < 3 {
					return &http.Response{
						StatusCode: 500,
						Body:       io.NopCloser(strings.NewReader(`{"error": "server error"}`)),
						Header:     make(http.Header),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(`{"status": "ok"}`)),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.Success {
			t.Errorf("Success = false, want true")
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3", attempts)
		}
	})

	t.Run("retry exhausted", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		retry := &RetryConfig{
			Enabled:           true,
			MaxAttempts:       2,
			InitialBackoffSec: 0,
		}
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, retry)

		attempts := 0
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(strings.NewReader(`{"error": "server error"}`)),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// With 500 response, Success should be false even after all retries
		if result.Success {
			t.Errorf("Success = true, want false (server error)")
		}
		if attempts != 2 {
			t.Errorf("attempts = %d, want 2", attempts)
		}
	})

	t.Run("no retry on client error", func(t *testing.T) {
		urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
		retry := &RetryConfig{
			Enabled:     true,
			MaxAttempts: 3,
		}
		step := NewHTTPCallStep("test", urlTmpl, "GET", nil, nil, "json", 0, retry)

		attempts := 0
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{
					StatusCode: 400, // Client error - no retry
					Body:       io.NopCloser(strings.NewReader(`{"error": "bad request"}`)),
					Header:     make(http.Header),
				}, nil
			},
		}

		data := ExecutionData{
			TemplateData: map[string]any{},
			HTTPClient:   client,
		}

		result, err := step.Execute(context.Background(), data)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.Success {
			t.Errorf("Success = true, want false")
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1 (no retry for 4xx)", attempts)
		}
	})
}

// TestHTTPCallWithBody tests POST requests with body.
func TestHTTPCallWithBody(t *testing.T) {
	urlTmpl := template.Must(template.New("url").Parse("https://api.example.com/users"))
	bodyTmpl := template.Must(template.New("body").Parse(`{"name": "{{.name}}"}`))
	step := NewHTTPCallStep("create_user", urlTmpl, "POST", nil, bodyTmpl, "json", 0, nil)

	var capturedBody []byte
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.Method != "POST" {
				t.Errorf("Method = %q, want POST", req.Method)
			}
			body, _ := io.ReadAll(req.Body)
			capturedBody = body
			return &http.Response{
				StatusCode: 201,
				Body:       io.NopCloser(strings.NewReader(`{"id": 1}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	data := ExecutionData{
		TemplateData: map[string]any{"name": "Alice"},
		HTTPClient:   client,
	}

	result, err := step.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if !bytes.Contains(capturedBody, []byte("Alice")) {
		t.Errorf("Body = %q, want to contain 'Alice'", string(capturedBody))
	}
}

// TestHTTPCallWithHeaders tests requests with custom headers.
func TestHTTPCallWithHeaders(t *testing.T) {
	urlTmpl := template.Must(template.New("url").Parse("https://api.example.com"))
	authTmpl := template.Must(template.New("auth").Parse("Bearer {{.token}}"))
	headers := map[string]*template.Template{
		"Authorization": authTmpl,
	}
	step := NewHTTPCallStep("test", urlTmpl, "GET", headers, nil, "json", 0, nil)

	var capturedReq *http.Request
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	data := ExecutionData{
		TemplateData: map[string]any{"token": "secret123"},
		HTTPClient:   client,
	}

	_, err := step.Execute(context.Background(), data)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if capturedReq.Header.Get("Authorization") != "Bearer secret123" {
		t.Errorf("Authorization = %q, want 'Bearer secret123'", capturedReq.Header.Get("Authorization"))
	}
}
