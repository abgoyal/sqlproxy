package tmpl

import (
	"net/http/httptest"
	"testing"
)

// ============================================================================
// Realistic Template Benchmarks
//
// These benchmarks measure template performance for actual patterns used in
// the sql-proxy project: cache keys, rate limit keys, etc.
// ============================================================================

// BenchmarkEngine_CacheKey_Simple benchmarks simple cache key like "items:{{.trigger.params.status}}"
func BenchmarkEngine_CacheKey_Simple(b *testing.B) {
	e := New()
	_ = e.Register("cache_key", "items:{{.trigger.params.status}}", UsagePreQuery)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.1",
			Headers:  map[string]string{"Authorization": "Bearer token"},
			Query:    map[string]string{"status": "active"},
			Params:   map[string]any{"status": "active"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Execute("cache_key", ctx)
	}
}

// BenchmarkEngine_CacheKey_MultiParam benchmarks cache key with multiple params
func BenchmarkEngine_CacheKey_MultiParam(b *testing.B) {
	e := New()
	_ = e.Register("cache_key", "report:{{.trigger.params.from}}:{{.trigger.params.to}}:{{.trigger.params.status}}", UsagePreQuery)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.1",
			Headers:  map[string]string{},
			Query:    map[string]string{},
			Params: map[string]any{
				"from":   "2024-01-01",
				"to":     "2024-01-31",
				"status": "completed",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Execute("cache_key", ctx)
	}
}

// BenchmarkEngine_CacheKey_WithDefault benchmarks cache key with default fallback
func BenchmarkEngine_CacheKey_WithDefault(b *testing.B) {
	e := New()
	_ = e.Register("cache_key", `items:{{.trigger.params.status | default "all"}}`, UsagePreQuery)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.1",
			Headers:  map[string]string{},
			Query:    map[string]string{},
			Params:   map[string]any{}, // status not provided, will use default
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Execute("cache_key", ctx)
	}
}

// BenchmarkEngine_RateLimit_ClientIP benchmarks simple rate limit key
func BenchmarkEngine_RateLimit_ClientIP(b *testing.B) {
	e := New()
	_ = e.Register("rate_key", "{{.trigger.client_ip}}", UsagePreQuery)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.100",
			Headers:  map[string]string{},
			Query:    map[string]string{},
			Params:   map[string]any{},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Execute("rate_key", ctx)
	}
}

// BenchmarkEngine_RateLimit_Composite benchmarks composite rate limit key
func BenchmarkEngine_RateLimit_Composite(b *testing.B) {
	e := New()
	_ = e.Register("rate_key", `{{.trigger.client_ip}}:{{getOr .trigger.headers "X-Tenant-ID" "default"}}`, UsagePreQuery)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.100",
			Headers:  map[string]string{"X-Tenant-ID": "acme-corp"},
			Query:    map[string]string{},
			Params:   map[string]any{},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Execute("rate_key", ctx)
	}
}

// BenchmarkEngine_RateLimit_HeaderRequired benchmarks rate limit with required header
func BenchmarkEngine_RateLimit_HeaderRequired(b *testing.B) {
	e := New()
	_ = e.Register("rate_key", `{{require .trigger.headers "Authorization"}}`, UsagePreQuery)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.100",
			Headers:  map[string]string{"Authorization": "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
			Query:    map[string]string{},
			Params:   map[string]any{},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Execute("rate_key", ctx)
	}
}

// BenchmarkEngine_ExecuteInline benchmarks inline (non-cached) template execution
func BenchmarkEngine_ExecuteInline(b *testing.B) {
	e := New()
	tmpl := "{{.trigger.client_ip}}:{{.trigger.params.tenant}}"

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.100",
			Headers:  map[string]string{},
			Query:    map[string]string{},
			Params:   map[string]any{"tenant": "acme"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.ExecuteInline(tmpl, ctx, UsagePreQuery)
	}
}

// BenchmarkEngine_Register benchmarks template registration/compilation
func BenchmarkEngine_Register(b *testing.B) {
	tmpl := `{{.trigger.client_ip}}:{{getOr .trigger.headers "X-Tenant" "default"}}:{{.trigger.params.id}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := New()
		_ = e.Register("test", tmpl, UsagePreQuery)
	}
}

// BenchmarkEngine_Validate benchmarks template validation
func BenchmarkEngine_Validate(b *testing.B) {
	e := New()
	tmpl := `{{.trigger.client_ip}}:{{getOr .trigger.headers "X-Tenant" "default"}}:{{.trigger.params.id}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Validate(tmpl, UsagePreQuery)
	}
}

// BenchmarkEngine_ValidateWithParams benchmarks template validation with param checking
func BenchmarkEngine_ValidateWithParams(b *testing.B) {
	e := New()
	tmpl := `{{.trigger.params.status}}:{{.trigger.params.from}}:{{.trigger.params.to}}`
	params := []string{"status", "from", "to", "limit", "offset"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.ValidateWithParams(tmpl, UsagePreQuery, params)
	}
}

// ============================================================================
// Context Building Benchmarks
// ============================================================================

// BenchmarkContextBuilder_Simple benchmarks basic context building
func BenchmarkContextBuilder_Simple(b *testing.B) {
	builder := NewContextBuilder(false, "1.0.0")
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build(req, nil)
	}
}

// BenchmarkContextBuilder_WithHeaders benchmarks context with many headers
func BenchmarkContextBuilder_WithHeaders(b *testing.B) {
	builder := NewContextBuilder(true, "1.0.0")
	req := httptest.NewRequest("GET", "/api/test?status=active&limit=100", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
	req.Header.Set("X-Request-ID", "req-12345")
	req.Header.Set("X-Tenant-ID", "acme-corp")
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.1:12345"

	params := map[string]any{
		"status": "active",
		"limit":  100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build(req, params)
	}
}

// ============================================================================
// Reference Extraction Benchmarks
// ============================================================================

// BenchmarkExtractParamRefs_Simple benchmarks simple param extraction
func BenchmarkExtractParamRefs_Simple(b *testing.B) {
	tmpl := "{{.trigger.params.status}}"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExtractParamRefs(tmpl)
	}
}

// BenchmarkExtractParamRefs_Complex benchmarks complex param extraction
func BenchmarkExtractParamRefs_Complex(b *testing.B) {
	tmpl := `{{.trigger.params.status}}:{{.trigger.params.from | default "today"}}:{{.trigger.params.to}}:{{if .trigger.params.includeDeleted}}all{{end}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExtractParamRefs(tmpl)
	}
}

// BenchmarkExtractHeaderRefs benchmarks header reference extraction
func BenchmarkExtractHeaderRefs(b *testing.B) {
	tmpl := `{{.trigger.headers.Authorization}}:{{require .trigger.headers "X-API-Key"}}:{{getOr .trigger.headers "X-Tenant" "default"}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExtractHeaderRefs(tmpl)
	}
}

// ============================================================================
// Helper Function Benchmarks
// ============================================================================

// BenchmarkFunc_RequireFunc benchmarks require function
func BenchmarkFunc_RequireFunc(b *testing.B) {
	m := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = requireFunc(m, "key")
	}
}

// BenchmarkFunc_GetOrFunc benchmarks getOr function
func BenchmarkFunc_GetOrFunc(b *testing.B) {
	m := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getOrFunc(m, "missing", "default")
	}
}

// BenchmarkFunc_HasFunc benchmarks has function
func BenchmarkFunc_HasFunc(b *testing.B) {
	m := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasFunc(m, "key")
	}
}

// BenchmarkFunc_JSONFunc benchmarks JSON serialization
func BenchmarkFunc_JSONFunc(b *testing.B) {
	data := map[string]any{
		"id":     123,
		"name":   "test",
		"active": true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jsonFunc(data)
	}
}

// BenchmarkFunc_CoalesceFunc benchmarks coalesce function
func BenchmarkFunc_CoalesceFunc(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = coalesceFunc("", "", "value", "other")
	}
}

// ============================================================================
// Concurrent Access Benchmarks
// ============================================================================

// BenchmarkEngine_Concurrent_SameTemplate benchmarks concurrent access to same template
func BenchmarkEngine_Concurrent_SameTemplate(b *testing.B) {
	e := New()
	_ = e.Register("concurrent", "{{.trigger.client_ip}}:{{.trigger.params.id}}", UsagePreQuery)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := &Context{
			Trigger: &TriggerContext{
				ClientIP: "192.168.1.1",
				Headers:  map[string]string{},
				Query:    map[string]string{},
				Params:   map[string]any{"id": 42},
			},
		}
		for pb.Next() {
			_, _ = e.Execute("concurrent", ctx)
		}
	})
}

// BenchmarkEngine_Concurrent_DifferentTemplates benchmarks concurrent access to different templates
func BenchmarkEngine_Concurrent_DifferentTemplates(b *testing.B) {
	e := New()
	_ = e.Register("template1", "{{.trigger.client_ip}}", UsagePreQuery)
	_ = e.Register("template2", "{{.trigger.params.status}}", UsagePreQuery)
	_ = e.Register("template3", `{{getOr .trigger.headers "X-Tenant" "default"}}`, UsagePreQuery)

	templates := []string{"template1", "template2", "template3"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := &Context{
			Trigger: &TriggerContext{
				ClientIP: "192.168.1.1",
				Headers:  map[string]string{"X-Tenant": "acme"},
				Query:    map[string]string{},
				Params:   map[string]any{"status": "active"},
			},
		}
		i := 0
		for pb.Next() {
			_, _ = e.Execute(templates[i%3], ctx)
			i++
		}
	})
}

// BenchmarkContextBuilder_Concurrent benchmarks concurrent context building
func BenchmarkContextBuilder_Concurrent(b *testing.B) {
	builder := NewContextBuilder(true, "1.0.0")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest("GET", "/api/test?id=123", nil)
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		req.RemoteAddr = "192.168.1.1:12345"
		params := map[string]any{"id": 123}

		for pb.Next() {
			_ = builder.Build(req, params)
		}
	})
}
