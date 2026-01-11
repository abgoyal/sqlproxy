package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sql-proxy/internal/config"
)

// BenchmarkExecuteTemplate benchmarks template execution with various complexity
func BenchmarkExecuteTemplate(b *testing.B) {
	benchmarks := []struct {
		name string
		tmpl string
		data any
	}{
		{
			name: "simple",
			tmpl: "Hello {{.Name}}",
			data: map[string]any{"Name": "World"},
		},
		{
			name: "json_function",
			tmpl: `{"data": {{json .}}}`,
			data: map[string]any{"id": 1, "name": "test", "active": true},
		},
		{
			name: "conditional",
			tmpl: `{{if .Success}}OK{{else}}FAIL{{end}}`,
			data: &ExecutionContext{Success: true},
		},
		{
			name: "range",
			tmpl: `{{range .Data}}{{.id}},{{end}}`,
			data: &ExecutionContext{
				Data: []map[string]any{
					{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}, {"id": 5},
				},
			},
		},
		{
			name: "complex",
			tmpl: `{"query":"{{.Query}}","count":{{.Count}},"success":{{.Success}},"data":[{{range $i, $row := .Data}}{{if $i}},{{end}}{{json $row}}{{end}}]}`,
			data: &ExecutionContext{
				Query:   "test_query",
				Count:   10,
				Success: true,
				Data: []map[string]any{
					{"id": 1, "name": "a"},
					{"id": 2, "name": "b"},
					{"id": 3, "name": "c"},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := executeTemplate("bench", bm.tmpl, bm.data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBuildBody benchmarks body building for webhook payloads
func BenchmarkBuildBody(b *testing.B) {
	benchmarks := []struct {
		name    string
		cfg     *config.WebhookBodyConfig
		execCtx *ExecutionContext
	}{
		{
			name: "no_body_config",
			cfg:  nil,
			execCtx: &ExecutionContext{
				Query: "test",
				Count: 5,
				Data: []map[string]any{
					{"id": 1, "name": "a"},
					{"id": 2, "name": "b"},
				},
			},
		},
		{
			name: "simple_item_template",
			cfg: &config.WebhookBodyConfig{
				Header: `{"data":[`,
				Item:   `{"id":{{.id}}}`,
				Footer: `]}`,
			},
			execCtx: &ExecutionContext{
				Query: "test",
				Count: 5,
				Data: []map[string]any{
					{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}, {"id": 5},
				},
			},
		},
		{
			name: "json_serialization",
			cfg: &config.WebhookBodyConfig{
				Header: `{"query":"{{.Query}}","items":[`,
				Item:   `{{json .}}`,
				Footer: `]}`,
			},
			execCtx: &ExecutionContext{
				Query: "test",
				Count: 10,
				Data: []map[string]any{
					{"id": 1, "name": "item1", "value": 100},
					{"id": 2, "name": "item2", "value": 200},
					{"id": 3, "name": "item3", "value": 300},
					{"id": 4, "name": "item4", "value": 400},
					{"id": 5, "name": "item5", "value": 500},
				},
			},
		},
		{
			name: "many_rows",
			cfg: &config.WebhookBodyConfig{
				Header: `[`,
				Item:   `{{json .}}`,
				Footer: `]`,
			},
			execCtx: func() *ExecutionContext {
				data := make([]map[string]any, 100)
				for i := 0; i < 100; i++ {
					data[i] = map[string]any{"id": i, "name": "item", "active": true}
				}
				return &ExecutionContext{Count: 100, Data: data}
			}(),
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := buildBody(bm.cfg, bm.execCtx)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkExecute benchmarks end-to-end webhook execution
func BenchmarkExecute(b *testing.B) {
	// Create a fast test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	disabled := false
	cfg := &config.WebhookConfig{
		URL:    server.URL,
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Retry: &config.WebhookRetryConfig{
			Enabled: &disabled, // Disable retries for benchmark
		},
	}

	execCtx := &ExecutionContext{
		Query:   "benchmark_query",
		Count:   10,
		Success: true,
		Data: []map[string]any{
			{"id": 1, "name": "test1"},
			{"id": 2, "name": "test2"},
			{"id": 3, "name": "test3"},
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := Execute(ctx, cfg, execCtx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExecute_WithBodyTemplate benchmarks execution with body templates
func BenchmarkExecute_WithBodyTemplate(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	disabled := false
	cfg := &config.WebhookConfig{
		URL:    server.URL,
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: &config.WebhookBodyConfig{
			Header: `{"query":"{{.Query}}","count":{{.Count}},"data":[`,
			Item:   `{"id":{{.id}},"name":"{{.name}}"}`,
			Footer: `]}`,
		},
		Retry: &config.WebhookRetryConfig{
			Enabled: &disabled,
		},
	}

	execCtx := &ExecutionContext{
		Query:   "benchmark_query",
		Count:   10,
		Success: true,
		Data: []map[string]any{
			{"id": 1, "name": "test1"},
			{"id": 2, "name": "test2"},
			{"id": 3, "name": "test3"},
			{"id": 4, "name": "test4"},
			{"id": 5, "name": "test5"},
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := Execute(ctx, cfg, execCtx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResolveRetryConfig benchmarks retry config resolution
func BenchmarkResolveRetryConfig(b *testing.B) {
	enabled := true
	cfg := &config.WebhookRetryConfig{
		Enabled:           &enabled,
		MaxAttempts:       5,
		InitialBackoffSec: 2,
		MaxBackoffSec:     60,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = resolveRetryConfig(cfg)
	}
}

// BenchmarkBuildBody_Parallel benchmarks concurrent body building
func BenchmarkBuildBody_Parallel(b *testing.B) {
	cfg := &config.WebhookBodyConfig{
		Header: `{"data":[`,
		Item:   `{{json .}}`,
		Footer: `]}`,
	}

	execCtx := &ExecutionContext{
		Query: "test",
		Count: 10,
		Data: []map[string]any{
			{"id": 1, "name": "a"},
			{"id": 2, "name": "b"},
			{"id": 3, "name": "c"},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := buildBody(cfg, execCtx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
