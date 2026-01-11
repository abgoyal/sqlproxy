package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sql-proxy/internal/config"
)

// TestExecuteTemplate_Basic tests basic template execution
func TestExecuteTemplate_Basic(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		data     any
		expected string
	}{
		{
			name:     "simple variable",
			tmpl:     "Count: {{.Count}}",
			data:     &ExecutionContext{Count: 5},
			expected: "Count: 5",
		},
		{
			name:     "multiple variables",
			tmpl:     "{{.Query}}: {{.Count}} rows in {{.DurationMs}}ms",
			data:     &ExecutionContext{Query: "test", Count: 10, DurationMs: 123},
			expected: "test: 10 rows in 123ms",
		},
		{
			name:     "success flag",
			tmpl:     "{{if .Success}}OK{{else}}FAILED{{end}}",
			data:     &ExecutionContext{Success: true},
			expected: "OK",
		},
		{
			name:     "error message",
			tmpl:     "Error: {{.Error}}",
			data:     &ExecutionContext{Error: "connection timeout"},
			expected: "Error: connection timeout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executeTemplate("test", tc.tmpl, tc.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestExecuteTemplate_Functions tests custom template functions
func TestExecuteTemplate_Functions(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		data     map[string]any
		expected string
	}{
		{
			name:     "add function",
			tmpl:     "{{add ._index 1}}",
			data:     map[string]any{"_index": 0},
			expected: "1",
		},
		{
			name:     "mod function even",
			tmpl:     "{{if eq (mod ._index 2) 0}}even{{else}}odd{{end}}",
			data:     map[string]any{"_index": 2},
			expected: "even",
		},
		{
			name:     "mod function odd",
			tmpl:     "{{if eq (mod ._index 2) 0}}even{{else}}odd{{end}}",
			data:     map[string]any{"_index": 3},
			expected: "odd",
		},
		{
			name:     "json function",
			tmpl:     `{{json .data}}`,
			data:     map[string]any{"data": map[string]any{"name": "test", "value": 123}},
			expected: `{"name":"test","value":123}`,
		},
		{
			name:     "json function array",
			tmpl:     `{{json .items}}`,
			data:     map[string]any{"items": []string{"a", "b", "c"}},
			expected: `["a","b","c"]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executeTemplate("test", tc.tmpl, tc.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestExecuteItemTemplate tests item template with row data
func TestExecuteItemTemplate(t *testing.T) {
	row := map[string]any{
		"id":   1,
		"name": "Alice",
	}

	tests := []struct {
		name     string
		tmpl     string
		index    int
		count    int
		expected string
	}{
		{
			name:     "access row fields",
			tmpl:     `{"id": {{.id}}, "name": "{{.name}}"}`,
			index:    0,
			count:    3,
			expected: `{"id": 1, "name": "Alice"}`,
		},
		{
			name:     "access index and count",
			tmpl:     `Item {{add ._index 1}} of {{._count}}`,
			index:    0,
			count:    3,
			expected: `Item 1 of 3`,
		},
		{
			name:     "conditional on first item",
			tmpl:     `{{if eq ._index 0}}FIRST{{end}}{{.name}}`,
			index:    0,
			count:    3,
			expected: `FIRSTAlice`,
		},
		{
			name:     "conditional on last item",
			tmpl:     `{{.name}}{{if eq (add ._index 1) ._count}} (last){{end}}`,
			index:    2,
			count:    3,
			expected: `Alice (last)`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executeItemTemplate(tc.tmpl, row, tc.index, tc.count)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestBuildBody_RawJSON tests raw JSON output when no body config
func TestBuildBody_RawJSON(t *testing.T) {
	execCtx := &ExecutionContext{
		Query:      "test_query",
		Count:      2,
		Success:    true,
		DurationMs: 100,
		Params:     map[string]string{"date": "2024-01-01"},
		Data: []map[string]any{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
		},
	}

	body, err := buildBody(nil, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check key fields
	if result["query"] != "test_query" {
		t.Errorf("query = %v, want 'test_query'", result["query"])
	}
	if result["count"].(float64) != 2 {
		t.Errorf("count = %v, want 2", result["count"])
	}
	if result["success"] != true {
		t.Errorf("success = %v, want true", result["success"])
	}
}

// TestBuildBody_HeaderItemFooter tests templated body building
func TestBuildBody_HeaderItemFooter(t *testing.T) {
	bodyCfg := &config.WebhookBodyConfig{
		Header:    `{"items": [`,
		Item:      `{"id": {{.id}}, "name": "{{.name}}"}`,
		Footer:    `]}`,
		Separator: ",",
	}

	execCtx := &ExecutionContext{
		Query: "test",
		Count: 2,
		Data: []map[string]any{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
		},
	}

	body, err := buildBody(bodyCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"items": [{"id": 1, "name": "Alice"},{"id": 2, "name": "Bob"}]}`
	if body != expected {
		t.Errorf("got %q, want %q", body, expected)
	}

	// Verify it's valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
}

// TestBuildBody_EmptyTemplate tests alternate empty template
func TestBuildBody_EmptyTemplate(t *testing.T) {
	bodyCfg := &config.WebhookBodyConfig{
		Header:  `{"items": [`,
		Item:    `{"id": {{.id}}}`,
		Footer:  `]}`,
		Empty:   `{"items": [], "message": "No data for {{.Query}}"}`,
		OnEmpty: "send",
	}

	execCtx := &ExecutionContext{
		Query: "test_query",
		Count: 0,
		Data:  []map[string]any{},
	}

	body, err := buildBody(bodyCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"items": [], "message": "No data for test_query"}`
	if body != expected {
		t.Errorf("got %q, want %q", body, expected)
	}
}

// TestBuildBody_DefaultSeparator tests default comma separator when not specified
func TestBuildBody_DefaultSeparator(t *testing.T) {
	bodyCfg := &config.WebhookBodyConfig{
		Item:      `"{{.name}}"`,
		Separator: "", // empty defaults to comma
	}

	execCtx := &ExecutionContext{
		Count: 2,
		Data: []map[string]any{
			{"name": "Alice"},
			{"name": "Bob"},
		},
	}

	body, err := buildBody(bodyCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `"Alice","Bob"`
	if body != expected {
		t.Errorf("got %q, want %q", body, expected)
	}
}

// TestBuildBody_NewlineSeparator tests newline separator for list format
func TestBuildBody_NewlineSeparator(t *testing.T) {
	bodyCfg := &config.WebhookBodyConfig{
		Item:      `- {{.name}}`,
		Separator: "\n",
	}

	execCtx := &ExecutionContext{
		Count: 2,
		Data: []map[string]any{
			{"name": "Alice"},
			{"name": "Bob"},
		},
	}

	body, err := buildBody(bodyCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "- Alice\n- Bob"
	if body != expected {
		t.Errorf("got %q, want %q", body, expected)
	}
}

// TestBuildBody_ParamsAccess tests access to params in templates
func TestBuildBody_ParamsAccess(t *testing.T) {
	bodyCfg := &config.WebhookBodyConfig{
		Header: `Report for {{index .Params "date"}}:` + "\n",
		Item:   `{{.name}}`,
		Footer: "",
	}

	execCtx := &ExecutionContext{
		Count:  1,
		Params: map[string]string{"date": "2024-01-15"},
		Data:   []map[string]any{{"name": "Alice"}},
	}

	body, err := buildBody(bodyCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Report for 2024-01-15:\nAlice"
	if body != expected {
		t.Errorf("got %q, want %q", body, expected)
	}
}

// TestExecute_RawPayload tests webhook execution with raw JSON
func TestExecute_RawPayload(t *testing.T) {
	var receivedBody string
	var receivedMethod string
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	webhookCfg := &config.WebhookConfig{
		URL:    server.URL,
		Method: "POST",
		Headers: map[string]string{
			"X-Custom": "test-value",
		},
	}

	execCtx := &ExecutionContext{
		Query:   "test_query",
		Count:   1,
		Success: true,
		Data:    []map[string]any{{"id": 1}},
	}

	err := Execute(context.Background(), webhookCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify method
	if receivedMethod != "POST" {
		t.Errorf("method = %q, want POST", receivedMethod)
	}

	// Verify custom header
	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("X-Custom header = %q, want 'test-value'", receivedHeaders.Get("X-Custom"))
	}

	// Verify Content-Type
	if !strings.HasPrefix(receivedHeaders.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q, want application/json", receivedHeaders.Get("Content-Type"))
	}

	// Verify body is valid JSON with expected fields
	var body map[string]any
	if err := json.Unmarshal([]byte(receivedBody), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["query"] != "test_query" {
		t.Errorf("body.query = %v, want 'test_query'", body["query"])
	}
}

// TestExecute_TemplatedURL tests URL template execution
func TestExecute_TemplatedURL(t *testing.T) {
	var requestedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	webhookCfg := &config.WebhookConfig{
		URL: server.URL + "/webhook/{{.Query}}/{{.Count}}",
	}

	execCtx := &ExecutionContext{
		Query: "daily_report",
		Count: 42,
	}

	err := Execute(context.Background(), webhookCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestedPath != "/webhook/daily_report/42" {
		t.Errorf("path = %q, want '/webhook/daily_report/42'", requestedPath)
	}
}

// TestExecute_SkipOnEmpty tests on_empty: skip behavior
func TestExecute_SkipOnEmpty(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	webhookCfg := &config.WebhookConfig{
		URL: server.URL,
		Body: &config.WebhookBodyConfig{
			OnEmpty: "skip",
		},
	}

	execCtx := &ExecutionContext{
		Query: "test",
		Count: 0,
		Data:  []map[string]any{},
	}

	err := Execute(context.Background(), webhookCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 0 {
		t.Errorf("webhook was called %d times, expected 0 (skip on empty)", callCount)
	}
}

// TestExecute_SendOnEmpty tests on_empty: send (default) behavior
func TestExecute_SendOnEmpty(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	webhookCfg := &config.WebhookConfig{
		URL: server.URL,
		Body: &config.WebhookBodyConfig{
			OnEmpty: "send",
		},
	}

	execCtx := &ExecutionContext{
		Query: "test",
		Count: 0,
		Data:  []map[string]any{},
	}

	err := Execute(context.Background(), webhookCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("webhook was called %d times, expected 1 (send on empty)", callCount)
	}
}

// TestExecute_HTTPError tests error handling for non-2xx responses
func TestExecute_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	webhookCfg := &config.WebhookConfig{
		URL: server.URL,
	}

	execCtx := &ExecutionContext{Query: "test", Count: 0}

	err := Execute(context.Background(), webhookCfg, execCtx)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

// TestExecute_Timeout tests context timeout
func TestExecute_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	webhookCfg := &config.WebhookConfig{
		URL: server.URL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := Execute(ctx, webhookCfg, &ExecutionContext{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// TestExecute_HTTPMethods tests default POST and explicit GET methods
func TestExecute_HTTPMethods(t *testing.T) {
	tests := []struct {
		configMethod   string
		expectedMethod string
	}{
		{"", "POST"},      // empty defaults to POST
		{"POST", "POST"},
		{"GET", "GET"},
		{"PUT", "PUT"},
	}

	for _, tc := range tests {
		t.Run(tc.expectedMethod, func(t *testing.T) {
			var receivedMethod string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			webhookCfg := &config.WebhookConfig{
				URL:    server.URL,
				Method: tc.configMethod,
			}

			err := Execute(context.Background(), webhookCfg, &ExecutionContext{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if receivedMethod != tc.expectedMethod {
				t.Errorf("method = %q, want %q", receivedMethod, tc.expectedMethod)
			}
		})
	}
}

// TestBuildBody_SlackFormat tests building Slack-style webhook body
func TestBuildBody_SlackFormat(t *testing.T) {
	bodyCfg := &config.WebhookBodyConfig{
		Header: `{"text": "Query {{.Query}} completed with {{.Count}} results", "blocks": [`,
		Item: `{
			"type": "section",
			"text": {"type": "mrkdwn", "text": "*{{.name}}*: {{.status}}"}
		}`,
		Footer:    `]}`,
		Separator: ",",
	}

	execCtx := &ExecutionContext{
		Query: "status_check",
		Count: 2,
		Data: []map[string]any{
			{"name": "Server A", "status": "OK"},
			{"name": "Server B", "status": "WARNING"},
		},
	}

	body, err := buildBody(bodyCfg, execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if result["text"] != "Query status_check completed with 2 results" {
		t.Errorf("text = %v", result["text"])
	}

	blocks, ok := result["blocks"].([]any)
	if !ok || len(blocks) != 2 {
		t.Errorf("expected 2 blocks, got %v", result["blocks"])
	}
}

// TestExecuteTemplate_InvalidTemplate tests error handling for invalid templates
func TestExecuteTemplate_InvalidTemplate(t *testing.T) {
	_, err := executeTemplate("test", "{{.Invalid", nil)
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

// TestExecute_URLTemplateError tests error when URL template is invalid
func TestExecute_URLTemplateError(t *testing.T) {
	webhookCfg := &config.WebhookConfig{
		URL: "https://example.com/{{.Invalid", // Invalid template
	}

	err := Execute(context.Background(), webhookCfg, &ExecutionContext{})
	if err == nil {
		t.Fatal("expected error for invalid URL template")
	}
	if !strings.Contains(err.Error(), "url template error") {
		t.Errorf("error should mention url template: %v", err)
	}
}

// TestExecute_InvalidURL tests error when URL is malformed
func TestExecute_InvalidURL(t *testing.T) {
	webhookCfg := &config.WebhookConfig{
		URL: "://invalid-url", // Malformed URL
	}

	err := Execute(context.Background(), webhookCfg, &ExecutionContext{})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "creating request") {
		t.Errorf("error should mention creating request: %v", err)
	}
}

// TestBuildBody_TemplateErrors tests error messages identify which template failed
func TestBuildBody_TemplateErrors(t *testing.T) {
	invalidTemplate := "{{.Invalid"

	tests := []struct {
		name    string
		bodyCfg *config.WebhookBodyConfig
		execCtx *ExecutionContext
		errMsg  string
	}{
		{
			name:    "header error",
			bodyCfg: &config.WebhookBodyConfig{Header: invalidTemplate},
			execCtx: &ExecutionContext{},
			errMsg:  "header",
		},
		{
			name:    "item error",
			bodyCfg: &config.WebhookBodyConfig{Item: invalidTemplate},
			execCtx: &ExecutionContext{Count: 1, Data: []map[string]any{{"id": 1}}},
			errMsg:  "item",
		},
		{
			name:    "footer error",
			bodyCfg: &config.WebhookBodyConfig{Footer: invalidTemplate},
			execCtx: &ExecutionContext{},
			errMsg:  "footer",
		},
		{
			name:    "empty error",
			bodyCfg: &config.WebhookBodyConfig{Empty: invalidTemplate},
			execCtx: &ExecutionContext{Count: 0, Data: []map[string]any{}},
			errMsg:  "", // empty template error doesn't have a prefix
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildBody(tc.bodyCfg, tc.execCtx)
			if err == nil {
				t.Fatal("expected error for invalid template")
			}
			if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("error should mention %q: %v", tc.errMsg, err)
			}
		})
	}
}

// TestExecuteTemplate_ExecutionError tests template execution error (not parse error)
func TestExecuteTemplate_ExecutionError(t *testing.T) {
	// Template that calls a method on nil - this triggers execution error
	tmpl := "{{.Name.Method}}"
	_, err := executeTemplate("test", tmpl, struct{ Name *string }{nil})
	if err == nil {
		t.Error("expected execution error for nil method call")
	}
}

// TestJsonFunction_Error tests json function with unmarshalable value
func TestJsonFunction_Error(t *testing.T) {
	// Channels cannot be marshaled to JSON
	ch := make(chan int)
	result, err := executeTemplate("test", "{{json .ch}}", map[string]any{"ch": ch})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "json error") {
		t.Errorf("expected 'json error' in result, got %q", result)
	}
}

// TestJsonFunctions_Error tests json/jsonIndent functions with unmarshalable values
func TestJsonFunctions_Error(t *testing.T) {
	// Channels cannot be marshaled to JSON
	ch := make(chan int)

	tests := []struct {
		name string
		tmpl string
	}{
		{"json", "{{json .ch}}"},
		{"jsonIndent", "{{jsonIndent .ch}}"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executeTemplate("test", tc.tmpl, map[string]any{"ch": ch})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, "json error") {
				t.Errorf("expected 'json error' in result, got %q", result)
			}
		})
	}
}

// TestExecute_ConnectionError tests error when server is unreachable
func TestExecute_ConnectionError(t *testing.T) {
	webhookCfg := &config.WebhookConfig{
		URL: "http://localhost:99999", // Unreachable port
	}

	err := Execute(context.Background(), webhookCfg, &ExecutionContext{})
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !strings.Contains(err.Error(), "sending webhook") {
		t.Errorf("error should mention sending webhook: %v", err)
	}
}

// TestResolveRetryConfig tests retry configuration resolution
func TestResolveRetryConfig(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name           string
		cfg            *config.WebhookRetryConfig
		wantEnabled    bool
		wantMaxAttempts int
		wantInitialBackoff time.Duration
		wantMaxBackoff time.Duration
	}{
		{
			name:           "nil config uses defaults",
			cfg:            nil,
			wantEnabled:    true,
			wantMaxAttempts: 3,
			wantInitialBackoff: 1 * time.Second,
			wantMaxBackoff: 30 * time.Second,
		},
		{
			name:           "empty config uses defaults",
			cfg:            &config.WebhookRetryConfig{},
			wantEnabled:    true,
			wantMaxAttempts: 3,
			wantInitialBackoff: 1 * time.Second,
			wantMaxBackoff: 30 * time.Second,
		},
		{
			name:           "explicitly disabled",
			cfg:            &config.WebhookRetryConfig{Enabled: &disabled},
			wantEnabled:    false,
			wantMaxAttempts: 3, // Still has defaults but won't be used
			wantInitialBackoff: 1 * time.Second,
			wantMaxBackoff: 30 * time.Second,
		},
		{
			name:           "custom max attempts",
			cfg:            &config.WebhookRetryConfig{Enabled: &enabled, MaxAttempts: 5},
			wantEnabled:    true,
			wantMaxAttempts: 5,
			wantInitialBackoff: 1 * time.Second,
			wantMaxBackoff: 30 * time.Second,
		},
		{
			name:           "custom backoff",
			cfg:            &config.WebhookRetryConfig{Enabled: &enabled, InitialBackoffSec: 2, MaxBackoffSec: 60},
			wantEnabled:    true,
			wantMaxAttempts: 3, // Uses default when not specified
			wantInitialBackoff: 2 * time.Second,
			wantMaxBackoff: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveRetryConfig(tt.cfg)

			if result.Enabled != tt.wantEnabled {
				t.Errorf("Enabled = %v, want %v", result.Enabled, tt.wantEnabled)
			}
			if result.MaxAttempts != tt.wantMaxAttempts {
				t.Errorf("MaxAttempts = %v, want %v", result.MaxAttempts, tt.wantMaxAttempts)
			}
			if result.InitialBackoff != tt.wantInitialBackoff {
				t.Errorf("InitialBackoff = %v, want %v", result.InitialBackoff, tt.wantInitialBackoff)
			}
			if result.MaxBackoff != tt.wantMaxBackoff {
				t.Errorf("MaxBackoff = %v, want %v", result.MaxBackoff, tt.wantMaxBackoff)
			}
		})
	}
}

// TestExecute_RetryDisabled tests that retries are skipped when disabled
func TestExecute_RetryDisabled(t *testing.T) {
	// Server that always returns 500
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	disabled := false
	webhookCfg := &config.WebhookConfig{
		URL: server.URL,
		Retry: &config.WebhookRetryConfig{
			Enabled: &disabled,
		},
	}

	err := Execute(context.Background(), webhookCfg, &ExecutionContext{})
	if err == nil {
		t.Fatal("expected error")
	}

	// With retries disabled, should only attempt once
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

// TestExecute_CustomRetryConfig tests custom retry settings
func TestExecute_CustomRetryConfig(t *testing.T) {
	// Server that returns 500 twice, then 200
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer server.Close()

	enabled := true
	webhookCfg := &config.WebhookConfig{
		URL: server.URL,
		Retry: &config.WebhookRetryConfig{
			Enabled:           &enabled,
			MaxAttempts:       5,
			InitialBackoffSec: 1, // Use short backoff for tests
			MaxBackoffSec:     2,
		},
	}

	err := Execute(context.Background(), webhookCfg, &ExecutionContext{})
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}

	// Should succeed on 3rd attempt
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// TestExecutionContext_Version tests that version is included in ExecutionContext
func TestExecutionContext_Version(t *testing.T) {
	execCtx := &ExecutionContext{
		Query:   "test_query",
		Count:   5,
		Success: true,
		Version: "v1.2.3-abc123",
	}

	// Verify version is accessible for templates
	result, err := executeTemplate("test", "Version: {{.Version}}", execCtx)
	if err != nil {
		t.Fatalf("template error: %v", err)
	}

	if result != "Version: v1.2.3-abc123" {
		t.Errorf("expected 'Version: v1.2.3-abc123', got %q", result)
	}
}
