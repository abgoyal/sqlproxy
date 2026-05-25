package workflow

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"

	"sql-proxy/internal/workflow/step"
)

func TestExecuteQueryStep_TemplateError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "query", Database: "testdb"},
		SQLTmpl: template.Must(template.New("test").Option("missingkey=error").Parse("SELECT * FROM {{.missing}}")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeQueryStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "sql template error") {
		t.Errorf("Error = %v, want sql template error", result.Error)
	}
}

func TestExecuteQueryStep_DBError(t *testing.T) {
	dbm := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return nil, errors.New("connection refused")
		},
	}
	exec := NewExecutor(dbm, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "query", Database: "testdb"},
		SQLTmpl: template.Must(template.New("test").Parse("SELECT 1")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeQueryStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "connection refused") {
		t.Errorf("Error = %v, want connection refused", result.Error)
	}
}

func TestExecuteQueryStep_Success(t *testing.T) {
	dbm := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			if database != "testdb" {
				t.Errorf("database = %q, want testdb", database)
			}
			if params["status"] != "active" {
				t.Errorf("params[status] = %v, want active", params["status"])
			}
			return &step.QueryResult{
				Rows: []map[string]any{{"id": 1}, {"id": 2}},
			}, nil
		},
	}
	exec := NewExecutor(dbm, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "get_users", Type: "query", Database: "testdb"},
		SQLTmpl: template.Must(template.New("test").Parse("SELECT * FROM users WHERE status = @status")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{
			"trigger": map[string]any{
				"params": map[string]any{"status": "active"},
			},
		},
	}

	result, err := exec.executeQueryStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
}

func TestExecuteHTTPCallStep_URLTemplateError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Option("missingkey=error").Parse("https://api.example.com/{{.missing}}")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "url template error") {
		t.Errorf("Error = %v, want url template error", result.Error)
	}
}

func TestExecuteHTTPCallStep_BodyTemplateError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:   &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "POST"},
		URLTmpl:  template.Must(template.New("url").Parse("https://api.example.com")),
		BodyTmpl: template.Must(template.New("body").Option("missingkey=error").Parse(`{"id": {{.missing}}}`)),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "body template error") {
		t.Errorf("Error = %v, want body template error", result.Error)
	}
}

func TestExecuteHTTPCallStep_HeaderTemplateError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
		HeaderTmpls: map[string]*template.Template{
			"Authorization": template.Must(template.New("auth").Option("missingkey=error").Parse("Bearer {{.missing}}")),
		},
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "header") {
		t.Errorf("Error = %v, want header template error", result.Error)
	}
}

func TestExecuteHTTPCallStep_ConnectionError(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "connection refused") {
		t.Errorf("Error = %v, want connection error", result.Error)
	}
}

func TestExecuteHTTPCallStep_Non2xxResponse(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 404,
				Body:       io.NopCloser(strings.NewReader(`{"error": "not found"}`)),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false for 404")
	}
	if result.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
	}
}

func TestExecuteHTTPCallStep_JSONParse(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`[{"id": 1}, {"id": 2}]`)),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
}

func TestExecuteHTTPCallStep_TextParse(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("Hello, World!")),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET", Parse: "text"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if len(result.Data) != 1 || result.Data[0]["body"] != "Hello, World!" {
		t.Errorf("Data = %v, want body='Hello, World!'", result.Data)
	}
}

func TestExecuteHTTPCallStep_FormParse(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("name=Alice&age=30")),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET", Parse: "form"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if len(result.Data) != 1 {
		t.Fatalf("len(Data) = %d, want 1", len(result.Data))
	}
	if result.Data[0]["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", result.Data[0]["name"])
	}
}

func TestExecuteHTTPCallStep_InvalidJSON(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("not valid json")),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false for invalid JSON")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "json parse error") {
		t.Errorf("Error = %v, want json parse error", result.Error)
	}
}

func TestExecuteHTTPCallStep_DefaultContentType(t *testing.T) {
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
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:   &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "POST"},
		URLTmpl:  template.Must(template.New("url").Parse("https://api.example.com")),
		BodyTmpl: template.Must(template.New("body").Parse(`{"name": "test"}`)),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	_, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedReq.Header.Get("Content-Type"))
	}
}

func TestExecuteHTTPCallStep_CustomHeaders(t *testing.T) {
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
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config:  &StepConfig{Name: "test", Type: "httpcall", HTTPMethod: "GET"},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
		HeaderTmpls: map[string]*template.Template{
			"Authorization": template.Must(template.New("auth").Parse("Bearer {{.token}}")),
		},
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{"token": "secret123"},
	}

	_, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq.Header.Get("Authorization") != "Bearer secret123" {
		t.Errorf("Authorization = %q, want 'Bearer secret123'", capturedReq.Header.Get("Authorization"))
	}
}

func TestExecuteHTTPCallStep_RetryOn500(t *testing.T) {
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
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "GET",
			Retry:      &RetryConfig{Enabled: true, MaxAttempts: 3, InitialBackoffSec: 0},
		},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestExecuteHTTPCallStep_RetryExhausted(t *testing.T) {
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
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "GET",
			Retry:      &RetryConfig{Enabled: true, MaxAttempts: 2, InitialBackoffSec: 0},
		},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestExecuteHTTPCallStep_NoRetryOn4xx(t *testing.T) {
	attempts := 0
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{
				StatusCode: 400,
				Body:       io.NopCloser(strings.NewReader(`{"error": "bad request"}`)),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "GET",
			Retry:      &RetryConfig{Enabled: true, MaxAttempts: 3},
		},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry for 4xx)", attempts)
	}
}

func TestExecuteResponseStep_Success(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response", StatusCode: 200},
		TemplateTmpl: template.Must(template.New("test").Parse(`{"message": "{{.message}}"}`)),
	}

	recorder := httptest.NewRecorder()
	execData := step.ExecutionData{
		TemplateData:   map[string]any{"message": "Hello, World!"},
		ResponseWriter: recorder,
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}

	resp := recorder.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Response status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Hello, World!") {
		t.Errorf("body = %q, want to contain 'Hello, World!'", string(body))
	}
}

func TestExecuteResponseStep_NoResponseWriter(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response", StatusCode: 200},
		TemplateTmpl: template.Must(template.New("test").Parse(`{}`)),
	}

	execData := step.ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: nil,
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "ResponseWriter") {
		t.Errorf("Error = %v, want ResponseWriter error", result.Error)
	}
}

func TestExecuteResponseStep_TemplateError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response", StatusCode: 200},
		TemplateTmpl: template.Must(template.New("test").Option("missingkey=error").Parse(`{{.missing}}`)),
	}

	recorder := httptest.NewRecorder()
	execData := step.ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: recorder,
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "template error") {
		t.Errorf("Error = %v, want template error", result.Error)
	}
}

func TestExecuteResponseStep_WriteError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response", StatusCode: 200},
		TemplateTmpl: template.Must(template.New("test").Parse(`test`)),
	}

	execData := step.ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: &failingResponseWriter{header: make(http.Header)},
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "write response error") {
		t.Errorf("Error = %v, want write response error", result.Error)
	}
}

func TestExecuteResponseStep_DefaultStatusCode(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response"},
		TemplateTmpl: template.Must(template.New("test").Parse(`{}`)),
	}

	recorder := httptest.NewRecorder()
	execData := step.ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: recorder,
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200 (default)", result.StatusCode)
	}
}

func TestExecuteHTTPCallStep_ContextCancelledDuringRetry(t *testing.T) {
	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			attempts++
			cancel()
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "GET",
			Retry:      &RetryConfig{Enabled: true, MaxAttempts: 5, InitialBackoffSec: 1},
		},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(ctx, cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error != context.Canceled {
		t.Errorf("Error = %v, want context.Canceled", result.Error)
	}
	if attempts > 2 {
		t.Errorf("attempts = %d, want <= 2 (should stop on context cancel)", attempts)
	}
}

func TestExecuteHTTPCallStep_RetryWithBodyAndHeaders(t *testing.T) {
	attempts := 0
	var lastBody string
	var lastAuth string
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			attempts++
			if req.Body != nil {
				b, _ := io.ReadAll(req.Body)
				lastBody = string(b)
			}
			lastAuth = req.Header.Get("Authorization")
			if attempts < 2 {
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "POST",
			Retry:      &RetryConfig{Enabled: true, MaxAttempts: 3, InitialBackoffSec: 0},
		},
		URLTmpl:  template.Must(template.New("url").Parse("https://api.example.com")),
		BodyTmpl: template.Must(template.New("body").Parse(`{"user":"{{.user}}"}`)),
		HeaderTmpls: map[string]*template.Template{
			"Authorization": template.Must(template.New("auth").Parse("Bearer {{.token}}")),
		},
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{"user": "alice", "token": "abc"},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if !strings.Contains(lastBody, "alice") {
		t.Errorf("retry body = %q, want to contain 'alice'", lastBody)
	}
	if lastAuth != "Bearer abc" {
		t.Errorf("retry Authorization = %q, want 'Bearer abc'", lastAuth)
	}
}

func TestExecuteHTTPCallStep_RetryConnectionError(t *testing.T) {
	attempts := 0
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("connection reset")
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "GET",
			Retry:      &RetryConfig{Enabled: true, MaxAttempts: 3, InitialBackoffSec: 0},
		},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true after retry")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestExecuteHTTPCallStep_StepTimeout(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		},
	}
	exec := NewExecutor(&mockDBManager{}, client, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:       "test",
			Type:       "httpcall",
			HTTPMethod: "GET",
			TimeoutSec: 1,
		},
		URLTmpl: template.Must(template.New("url").Parse("https://api.example.com")),
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	result, err := exec.executeHTTPCallStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil {
		t.Fatal("Error = nil, want context deadline exceeded")
	}
}

func TestExecuteResponseStep_HeaderTemplateError(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response", StatusCode: 200},
		TemplateTmpl: template.Must(template.New("test").Parse(`{}`)),
		HeaderTmpls: map[string]*template.Template{
			"X-Custom": template.Must(template.New("h").Option("missingkey=error").Parse("{{.missing}}")),
		},
	}

	recorder := httptest.NewRecorder()
	execData := step.ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: recorder,
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "header") {
		t.Errorf("Error = %v, want header template error", result.Error)
	}
}

func TestExecuteResponseStep_CustomHeaders(t *testing.T) {
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config:       &StepConfig{Name: "test", Type: "response", StatusCode: 201},
		TemplateTmpl: template.Must(template.New("test").Parse(`{}`)),
		HeaderTmpls: map[string]*template.Template{
			"X-Custom": template.Must(template.New("h").Parse("custom-value")),
		},
	}

	recorder := httptest.NewRecorder()
	execData := step.ExecutionData{
		TemplateData:   map[string]any{},
		ResponseWriter: recorder,
	}

	result, err := exec.executeResponseStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.StatusCode != 201 {
		t.Errorf("StatusCode = %d, want 201", result.StatusCode)
	}
	if recorder.Header().Get("X-Custom") != "custom-value" {
		t.Errorf("X-Custom = %q, want 'custom-value'", recorder.Header().Get("X-Custom"))
	}
}

func TestExecuteQueryStep_PassesQueryOptions(t *testing.T) {
	lockTimeout := 5000
	var capturedOpts step.QueryOptions
	dbm := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			capturedOpts = opts
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	exec := NewExecutor(dbm, &mockHTTPClient{}, nil, &testLogger{})
	cs := &CompiledStep{
		Config: &StepConfig{
			Name:             "test",
			Type:             "query",
			Database:         "testdb",
			Isolation:        "read_committed",
			LockTimeoutMs:    &lockTimeout,
			DeadlockPriority: "low",
			JSONColumns:      []string{"data"},
		},
		SQLTmpl:      template.Must(template.New("test").Parse("SELECT 1")),
		IsWrite:      true,
		HasReturning: false,
	}

	execData := step.ExecutionData{
		TemplateData: map[string]any{},
	}

	_, err := exec.executeQueryStep(context.Background(), cs, execData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedOpts.Isolation != "read_committed" {
		t.Errorf("Isolation = %q, want read_committed", capturedOpts.Isolation)
	}
	if capturedOpts.LockTimeoutMs == nil || *capturedOpts.LockTimeoutMs != 5000 {
		t.Errorf("LockTimeoutMs = %v, want 5000", capturedOpts.LockTimeoutMs)
	}
	if capturedOpts.DeadlockPriority != "low" {
		t.Errorf("DeadlockPriority = %q, want low", capturedOpts.DeadlockPriority)
	}
	if len(capturedOpts.JSONColumns) != 1 || capturedOpts.JSONColumns[0] != "data" {
		t.Errorf("JSONColumns = %v, want [data]", capturedOpts.JSONColumns)
	}
	if capturedOpts.IsWrite == nil || *capturedOpts.IsWrite != true {
		t.Errorf("IsWrite = %v, want true", capturedOpts.IsWrite)
	}
	if capturedOpts.HasReturning == nil || *capturedOpts.HasReturning != false {
		t.Errorf("HasReturning = %v, want false", capturedOpts.HasReturning)
	}
}

type failingResponseWriter struct {
	header http.Header
}

func (f *failingResponseWriter) Header() http.Header        { return f.header }
func (f *failingResponseWriter) Write([]byte) (int, error)  { return 0, errors.New("write failed") }
func (f *failingResponseWriter) WriteHeader(statusCode int) {}
