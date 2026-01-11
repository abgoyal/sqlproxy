package tmpl

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewContextBuilder tests builder creation
func TestNewContextBuilder(t *testing.T) {
	b := NewContextBuilder(true, "1.0.0")
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
	if !b.trustProxyHeaders {
		t.Error("expected trustProxyHeaders to be true")
	}
	if b.version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", b.version)
	}
}

// TestContextBuilder_Build tests context creation from HTTP request
func TestContextBuilder_Build(t *testing.T) {
	b := NewContextBuilder(false, "test")

	req := httptest.NewRequest("GET", "/api/users?status=active&limit=10", nil)
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("X-Custom", "custom-value")
	req.RemoteAddr = "192.168.1.100:54321"

	params := map[string]any{
		"status": "active",
		"limit":  10,
	}

	ctx := b.Build(req, params)

	// Check basic fields
	if ctx.ClientIP != "192.168.1.100" {
		t.Errorf("expected ClientIP '192.168.1.100', got %q", ctx.ClientIP)
	}
	if ctx.Method != "GET" {
		t.Errorf("expected Method 'GET', got %q", ctx.Method)
	}
	if ctx.Path != "/api/users" {
		t.Errorf("expected Path '/api/users', got %q", ctx.Path)
	}
	if ctx.Version != "test" {
		t.Errorf("expected Version 'test', got %q", ctx.Version)
	}
	if ctx.Timestamp == "" {
		t.Error("expected Timestamp to be set")
	}

	// Check headers
	if ctx.Header["Authorization"] != "Bearer token123" {
		t.Errorf("expected Authorization header, got %q", ctx.Header["Authorization"])
	}
	if ctx.Header["X-Custom"] != "custom-value" {
		t.Errorf("expected X-Custom header, got %q", ctx.Header["X-Custom"])
	}

	// Check query params
	if ctx.Query["status"] != "active" {
		t.Errorf("expected query status 'active', got %q", ctx.Query["status"])
	}
	if ctx.Query["limit"] != "10" {
		t.Errorf("expected query limit '10', got %q", ctx.Query["limit"])
	}

	// Check params
	if ctx.Param["status"] != "active" {
		t.Errorf("expected param status 'active', got %v", ctx.Param["status"])
	}
	if ctx.Param["limit"] != 10 {
		t.Errorf("expected param limit 10, got %v", ctx.Param["limit"])
	}
}

// TestContextBuilder_Build_NilParams tests build with nil params
func TestContextBuilder_Build_NilParams(t *testing.T) {
	b := NewContextBuilder(false, "test")
	req := httptest.NewRequest("GET", "/api/test", nil)

	ctx := b.Build(req, nil)

	if ctx.Param == nil {
		t.Error("expected Param map to be initialized even with nil input")
	}
}

// TestContextBuilder_ResolveClientIP_NoProxy tests IP resolution without proxy headers
func TestContextBuilder_ResolveClientIP_NoProxy(t *testing.T) {
	b := NewContextBuilder(false, "test")

	tests := []struct {
		name       string
		remoteAddr string
		wantIP     string
	}{
		{"with port", "192.168.1.1:12345", "192.168.1.1"},
		{"without port", "192.168.1.1", "192.168.1.1"},
		{"ipv6 with port", "[::1]:12345", "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			req.Header.Set("X-Forwarded-For", "10.0.0.1") // Should be ignored

			ctx := b.Build(req, nil)

			if ctx.ClientIP != tt.wantIP {
				t.Errorf("expected IP %q, got %q", tt.wantIP, ctx.ClientIP)
			}
		})
	}
}

// TestContextBuilder_ResolveClientIP_WithProxy tests IP resolution with proxy headers
func TestContextBuilder_ResolveClientIP_WithProxy(t *testing.T) {
	b := NewContextBuilder(true, "test")

	tests := []struct {
		name     string
		xff      string
		xri      string
		remote   string
		wantIP   string
	}{
		{
			name:   "X-Forwarded-For single",
			xff:    "10.0.0.1",
			remote: "192.168.1.1:12345",
			wantIP: "10.0.0.1",
		},
		{
			name:   "X-Forwarded-For chain",
			xff:    "10.0.0.1, 10.0.0.2, 10.0.0.3",
			remote: "192.168.1.1:12345",
			wantIP: "10.0.0.1",
		},
		{
			name:   "X-Forwarded-For with spaces",
			xff:    "  10.0.0.1  ,  10.0.0.2",
			remote: "192.168.1.1:12345",
			wantIP: "10.0.0.1",
		},
		{
			name:   "X-Real-IP",
			xri:    "10.0.0.5",
			remote: "192.168.1.1:12345",
			wantIP: "10.0.0.5",
		},
		{
			name:   "X-Forwarded-For takes priority over X-Real-IP",
			xff:    "10.0.0.1",
			xri:    "10.0.0.5",
			remote: "192.168.1.1:12345",
			wantIP: "10.0.0.1",
		},
		{
			name:   "fallback to RemoteAddr",
			remote: "192.168.1.1:12345",
			wantIP: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remote
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			ctx := b.Build(req, nil)

			if ctx.ClientIP != tt.wantIP {
				t.Errorf("expected IP %q, got %q", tt.wantIP, ctx.ClientIP)
			}
		})
	}
}

// TestContextBuilder_GetRequestID tests request ID extraction
func TestContextBuilder_GetRequestID(t *testing.T) {
	b := NewContextBuilder(false, "test")

	tests := []struct {
		name      string
		headers   map[string]string
		wantID    string
		wantEmpty bool
	}{
		{
			name:    "X-Request-ID",
			headers: map[string]string{"X-Request-ID": "req-123"},
			wantID:  "req-123",
		},
		{
			name:    "X-Correlation-ID",
			headers: map[string]string{"X-Correlation-ID": "corr-456"},
			wantID:  "corr-456",
		},
		{
			name:    "X-Request-ID takes priority",
			headers: map[string]string{"X-Request-ID": "req-123", "X-Correlation-ID": "corr-456"},
			wantID:  "req-123",
		},
		{
			name:      "no header",
			headers:   map[string]string{},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ctx := b.Build(req, nil)

			if tt.wantEmpty {
				if ctx.RequestID != "" {
					t.Errorf("expected empty RequestID, got %q", ctx.RequestID)
				}
			} else {
				if ctx.RequestID != tt.wantID {
					t.Errorf("expected RequestID %q, got %q", tt.wantID, ctx.RequestID)
				}
			}
		})
	}
}

// TestContext_WithResult tests adding result to context
func TestContext_WithResult(t *testing.T) {
	ctx := &Context{
		ClientIP: "10.0.0.1",
	}

	result := &Result{
		Query:      "test_query",
		Success:    true,
		Count:      10,
		DurationMs: 42,
	}

	// Method should return same context for chaining
	returned := ctx.WithResult(result)
	if returned != ctx {
		t.Error("expected WithResult to return same context")
	}

	if ctx.Result != result {
		t.Error("expected Result to be set")
	}
}

// TestContext_ToMap tests context conversion to map
func TestContext_ToMap(t *testing.T) {
	ctx := &Context{
		ClientIP:  "192.168.1.1",
		Method:    "POST",
		Path:      "/api/test",
		RequestID: "req-123",
		Timestamp: "2024-01-15T10:30:00Z",
		Version:   "1.0.0",
		Header:    map[string]string{"Auth": "token"},
		Query:     map[string]string{"q": "search"},
		Param:     map[string]any{"id": 42},
	}

	// Test pre-query (no Result)
	m := ctx.toMap(UsagePreQuery)

	if m["ClientIP"] != "192.168.1.1" {
		t.Error("expected ClientIP in map")
	}
	if m["Method"] != "POST" {
		t.Error("expected Method in map")
	}
	if _, ok := m["Result"]; ok {
		t.Error("pre-query should not include Result")
	}

	// Add Result and test post-query
	ctx.Result = &Result{
		Query:   "test",
		Success: true,
		Count:   5,
	}

	m = ctx.toMap(UsagePostQuery)
	if _, ok := m["Result"]; !ok {
		t.Error("post-query should include Result")
	}

	result := m["Result"].(map[string]any)
	if result["Query"] != "test" {
		t.Error("expected Result.Query in map")
	}
	if result["Success"] != true {
		t.Error("expected Result.Success in map")
	}
	if result["Count"] != 5 {
		t.Error("expected Result.Count in map")
	}
}

// TestExtractParamRefs tests param reference extraction
func TestExtractParamRefs(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{
			name: "single param",
			tmpl: "{{.Param.status}}",
			want: []string{"status"},
		},
		{
			name: "multiple params",
			tmpl: "{{.Param.foo}}-{{.Param.bar}}",
			want: []string{"foo", "bar"},
		},
		{
			name: "param with pipe",
			tmpl: "{{.Param.status | upper}}",
			want: []string{"status"},
		},
		{
			name: "no params",
			tmpl: "{{.ClientIP}}",
			want: []string{},
		},
		{
			name: "duplicate params",
			tmpl: "{{.Param.id}}-{{.Param.id}}",
			want: []string{"id"},
		},
		{
			name: "underscore in name",
			tmpl: "{{.Param.user_id}}",
			want: []string{"user_id"},
		},
		{
			name: "complex template",
			tmpl: `{{if .Param.enabled}}{{.Param.value | default "none"}}{{end}}`,
			want: []string{"enabled", "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractParamRefs(tt.tmpl)

			if len(got) != len(tt.want) {
				t.Errorf("expected %d refs, got %d: %v", len(tt.want), len(got), got)
				return
			}

			for i, w := range tt.want {
				if i >= len(got) || got[i] != w {
					t.Errorf("expected ref %d to be %q, got %v", i, w, got)
				}
			}
		})
	}
}

// TestExtractHeaderRefs tests header reference extraction
func TestExtractHeaderRefs(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{
			name: "dot notation",
			tmpl: "{{.Header.Authorization}}",
			want: []string{"Authorization"},
		},
		{
			name: "require function",
			tmpl: `{{require .Header "X-API-Key"}}`,
			want: []string{"X-API-Key"},
		},
		{
			name: "getOr function",
			tmpl: `{{getOr .Header "X-Tenant" "default"}}`,
			want: []string{"X-Tenant"},
		},
		{
			name: "multiple headers",
			tmpl: `{{.Header.Authorization}}-{{require .Header "X-API-Key"}}`,
			want: []string{"Authorization", "X-API-Key"},
		},
		{
			name: "no headers",
			tmpl: "{{.ClientIP}}",
			want: []string{},
		},
		{
			name: "hyphenated header",
			tmpl: "{{.Header.X-Custom-Header}}",
			want: []string{"X-Custom-Header"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHeaderRefs(tt.tmpl)

			if len(got) != len(tt.want) {
				t.Errorf("expected %d refs, got %d: %v", len(tt.want), len(got), got)
				return
			}

			// Check all expected refs are present (order may vary)
			for _, w := range tt.want {
				found := false
				for _, g := range got {
					if g == w {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected ref %q not found in %v", w, got)
				}
			}
		})
	}
}

// TestExtractQueryRefs tests query reference extraction
func TestExtractQueryRefs(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{
			name: "dot notation",
			tmpl: "{{.Query.status}}",
			want: []string{"status"},
		},
		{
			name: "require function",
			tmpl: `{{require .Query "search"}}`,
			want: []string{"search"},
		},
		{
			name: "getOr function",
			tmpl: `{{getOr .Query "page" "1"}}`,
			want: []string{"page"},
		},
		{
			name: "multiple queries",
			tmpl: `{{.Query.status}}-{{getOr .Query "limit" "10"}}`,
			want: []string{"status", "limit"},
		},
		{
			name: "no queries",
			tmpl: "{{.ClientIP}}",
			want: []string{},
		},
		{
			name: "underscore in name",
			tmpl: "{{.Query.page_size}}",
			want: []string{"page_size"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractQueryRefs(tt.tmpl)

			if len(got) != len(tt.want) {
				t.Errorf("expected %d refs, got %d: %v", len(tt.want), len(got), got)
				return
			}

			// Check all expected refs are present (order may vary)
			for _, w := range tt.want {
				found := false
				for _, g := range got {
					if g == w {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected ref %q not found in %v", w, got)
				}
			}
		})
	}
}

// TestContext_Integration tests full context usage with engine
func TestContext_Integration(t *testing.T) {
	// Create builder and engine
	builder := NewContextBuilder(true, "2.0.0")
	engine := New()

	// Register templates
	err := engine.Register("rate_limit_key", "{{.ClientIP}}:{{getOr .Header \"X-Tenant\" \"default\"}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	err = engine.Register("cache_key", "users:{{.Param.status}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Create request
	req := httptest.NewRequest("GET", "/api/users?status=active", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Tenant", "acme")
	req.RemoteAddr = "192.168.1.1:12345"

	params := map[string]any{"status": "active"}

	// Build context
	ctx := builder.Build(req, params)

	// Execute rate limit key template
	result, err := engine.Execute("rate_limit_key", ctx)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if result != "10.0.0.1:acme" {
		t.Errorf("expected '10.0.0.1:acme', got %q", result)
	}

	// Execute cache key template
	result, err = engine.Execute("cache_key", ctx)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if result != "users:active" {
		t.Errorf("expected 'users:active', got %q", result)
	}
}

// TestContext_PostQuery_Integration tests post-query context with webhooks
func TestContext_PostQuery_Integration(t *testing.T) {
	builder := NewContextBuilder(false, "1.0.0")
	engine := New()

	err := engine.Register("webhook_body", `{"query":"{{.Result.Query}}","count":{{.Result.Count}},"success":{{.Result.Success}}}`, UsagePostQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	ctx := builder.Build(req, nil)

	ctx.WithResult(&Result{
		Query:   "daily_report",
		Success: true,
		Count:   42,
	})

	result, err := engine.Execute("webhook_body", ctx)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	expected := `{"query":"daily_report","count":42,"success":true}`
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// BenchmarkContextBuilder_Build benchmarks context creation
func BenchmarkContextBuilder_Build(b *testing.B) {
	builder := NewContextBuilder(true, "1.0.0")

	req, _ := http.NewRequest("GET", "/api/test?status=active&limit=10", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.RemoteAddr = "192.168.1.1:12345"

	params := map[string]any{"status": "active", "limit": 10}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build(req, params)
	}
}

// BenchmarkExtractParamRefs benchmarks param extraction
func BenchmarkExtractParamRefs(b *testing.B) {
	tmpl := `{{.Param.status}}:{{.Param.limit | default "10"}}:{{.Param.offset}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExtractParamRefs(tmpl)
	}
}
