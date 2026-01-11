package tmpl

import (
	"testing"
)

// TestNew verifies engine creation with all functions
func TestNew(t *testing.T) {
	e := New()
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.templates == nil {
		t.Error("expected templates map to be initialized")
	}
	if e.funcs == nil {
		t.Error("expected funcs map to be initialized")
	}
}

// TestRequireFunc tests the require helper function
func TestRequireFunc(t *testing.T) {
	tests := []struct {
		name    string
		m       map[string]string
		key     string
		want    string
		wantErr bool
	}{
		{
			name:    "key exists with value",
			m:       map[string]string{"foo": "bar"},
			key:     "foo",
			want:    "bar",
			wantErr: false,
		},
		{
			name:    "key missing",
			m:       map[string]string{"foo": "bar"},
			key:     "missing",
			wantErr: true,
		},
		{
			name:    "key exists but empty",
			m:       map[string]string{"foo": ""},
			key:     "foo",
			wantErr: true,
		},
		{
			name:    "nil map",
			m:       nil,
			key:     "foo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requireFunc(tt.m, tt.key)
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
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestGetOrFunc tests the getOr helper function
func TestGetOrFunc(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]string
		key      string
		fallback string
		want     string
	}{
		{
			name:     "key exists",
			m:        map[string]string{"foo": "bar"},
			key:      "foo",
			fallback: "default",
			want:     "bar",
		},
		{
			name:     "key missing",
			m:        map[string]string{"foo": "bar"},
			key:      "missing",
			fallback: "default",
			want:     "default",
		},
		{
			name:     "key empty uses fallback",
			m:        map[string]string{"foo": ""},
			key:      "foo",
			fallback: "default",
			want:     "default",
		},
		{
			name:     "nil map uses fallback",
			m:        nil,
			key:      "foo",
			fallback: "default",
			want:     "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getOrFunc(tt.m, tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestHasFunc tests the has helper function
func TestHasFunc(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]string
		key  string
		want bool
	}{
		{
			name: "key exists with value",
			m:    map[string]string{"foo": "bar"},
			key:  "foo",
			want: true,
		},
		{
			name: "key missing",
			m:    map[string]string{"foo": "bar"},
			key:  "missing",
			want: false,
		},
		{
			name: "key exists but empty",
			m:    map[string]string{"foo": ""},
			key:  "foo",
			want: false,
		},
		{
			name: "nil map",
			m:    nil,
			key:  "foo",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasFunc(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

// TestJSONFunc tests JSON serialization
func TestJSONFunc(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"string", "hello", `"hello"`},
		{"int", 42, "42"},
		{"bool", true, "true"},
		{"nil", nil, "null"},
		{"map", map[string]int{"a": 1}, `{"a":1}`},
		{"slice", []int{1, 2, 3}, "[1,2,3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonFunc(tt.v)
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

// TestJSONIndentFunc tests indented JSON serialization
func TestJSONIndentFunc(t *testing.T) {
	v := map[string]int{"a": 1}
	got := jsonIndentFunc(v)
	expected := "{\n  \"a\": 1\n}"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

// TestDefaultFunc tests the default helper function
func TestDefaultFunc(t *testing.T) {
	tests := []struct {
		name string
		def  any
		val  any
		want any
	}{
		{"nil value", "default", nil, "default"},
		{"empty string", "default", "", "default"},
		{"non-empty string", "default", "value", "value"},
		{"zero int", 42, 0, 42},
		{"non-zero int", 42, 10, 10},
		{"zero int64", int64(42), int64(0), int64(42)},
		{"non-zero int64", int64(42), int64(10), int64(10)},
		{"zero float64", 3.14, float64(0), 3.14},
		{"non-zero float64", 3.14, 2.5, 2.5},
		{"false bool stays false", true, false, false}, // false is valid, not "empty"
		{"true bool stays true", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultFunc(tt.def, tt.val)
			if got != tt.want {
				t.Errorf("expected %v (%T), got %v (%T)", tt.want, tt.want, got, got)
			}
		})
	}
}

// TestCoalesceFunc tests the coalesce function
func TestCoalesceFunc(t *testing.T) {
	tests := []struct {
		name string
		vals []string
		want string
	}{
		{"first non-empty", []string{"a", "b", "c"}, "a"},
		{"skip empty", []string{"", "b", "c"}, "b"},
		{"skip multiple empty", []string{"", "", "c"}, "c"},
		{"all empty", []string{"", "", ""}, ""},
		{"no values", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coalesceFunc(tt.vals...)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestEngine_Register tests template registration
func TestEngine_Register(t *testing.T) {
	e := New()

	tests := []struct {
		name    string
		tmpl    string
		wantErr bool
	}{
		{"valid template", "Hello {{.Name}}", false},
		{"empty template", "", true},
		{"invalid syntax", "Hello {{.Name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.Register(tt.name, tt.tmpl, UsagePreQuery)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestEngine_Execute tests template execution
func TestEngine_Execute(t *testing.T) {
	e := New()

	// Register a template
	err := e.Register("test", "IP={{.ClientIP}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx := &Context{
		ClientIP: "192.168.1.1",
		Header:   make(map[string]string),
		Query:    make(map[string]string),
		Param:    make(map[string]any),
	}

	result, err := e.Execute("test", ctx)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result != "IP=192.168.1.1" {
		t.Errorf("expected 'IP=192.168.1.1', got %q", result)
	}
}

// TestEngine_Execute_NotRegistered tests executing unregistered template
func TestEngine_Execute_NotRegistered(t *testing.T) {
	e := New()
	ctx := &Context{}

	_, err := e.Execute("nonexistent", ctx)
	if err == nil {
		t.Error("expected error for unregistered template")
	}
}

// TestEngine_Execute_EmptyResult tests that empty results are rejected
func TestEngine_Execute_EmptyResult(t *testing.T) {
	e := New()

	// Register a template that produces empty output
	err := e.Register("empty", "{{if false}}text{{end}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx := &Context{
		Header: make(map[string]string),
		Query:  make(map[string]string),
		Param:  make(map[string]any),
	}

	_, err = e.Execute("empty", ctx)
	if err == nil {
		t.Error("expected error for empty result")
	}
}

// TestEngine_ExecuteInline tests inline template execution
func TestEngine_ExecuteInline(t *testing.T) {
	e := New()

	ctx := &Context{
		ClientIP: "10.0.0.1",
		Method:   "GET",
		Header:   make(map[string]string),
		Query:    make(map[string]string),
		Param:    make(map[string]any),
	}

	tests := []struct {
		name    string
		tmpl    string
		want    string
		wantErr bool
	}{
		{"simple", "{{.ClientIP}}", "10.0.0.1", false},
		{"with function", "{{.Method | lower}}", "get", false},
		{"empty template", "", "", true},
		{"invalid syntax", "{{.Invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.ExecuteInline(tt.tmpl, ctx, UsagePreQuery)
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
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestEngine_Validate tests template validation
func TestEngine_Validate(t *testing.T) {
	e := New()

	tests := []struct {
		name    string
		tmpl    string
		usage   Usage
		wantErr bool
	}{
		{"valid pre-query", "{{.ClientIP}}", UsagePreQuery, false},
		{"valid post-query", "{{.Result.Count}}", UsagePostQuery, false},
		{"empty template", "", UsagePreQuery, true},
		{"invalid syntax", "{{.Invalid", UsagePreQuery, true},
		{"result in pre-query", "{{.Result.Count}}", UsagePreQuery, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.Validate(tt.tmpl, tt.usage)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestEngine_ValidateWithParams tests template validation with param checking
func TestEngine_ValidateWithParams(t *testing.T) {
	e := New()

	tests := []struct {
		name       string
		tmpl       string
		paramNames []string
		wantErr    bool
	}{
		{
			name:       "valid param reference",
			tmpl:       "{{.Param.status}}",
			paramNames: []string{"status"},
			wantErr:    false,
		},
		{
			name:       "missing param reference",
			tmpl:       "{{.Param.missing}}",
			paramNames: []string{"status"},
			wantErr:    true,
		},
		{
			name:       "multiple params all valid",
			tmpl:       "{{.Param.a}}:{{.Param.b}}",
			paramNames: []string{"a", "b", "c"},
			wantErr:    false,
		},
		{
			name:       "no params referenced",
			tmpl:       "{{.ClientIP}}",
			paramNames: []string{"status"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.ValidateWithParams(tt.tmpl, UsagePreQuery, tt.paramNames)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestEngine_MathFunctions tests math helper functions in templates
func TestEngine_MathFunctions(t *testing.T) {
	e := New()
	ctx := &Context{
		Header: make(map[string]string),
		Query:  make(map[string]string),
		Param:  make(map[string]any),
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"add", "{{add 5 3}}", "8"},
		{"sub", "{{sub 10 3}}", "7"},
		{"mul", "{{mul 4 5}}", "20"},
		{"div", "{{div 20 4}}", "5"},
		{"div by zero", "{{div 20 0}}", "0"},
		{"mod", "{{mod 10 3}}", "1"},
		{"mod by zero", "{{mod 10 0}}", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.tmpl, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

// TestEngine_StringFunctions tests string helper functions in templates
func TestEngine_StringFunctions(t *testing.T) {
	e := New()
	ctx := &Context{
		Header: make(map[string]string),
		Query:  make(map[string]string),
		Param:  make(map[string]any),
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"upper", `{{"hello" | upper}}`, "HELLO"},
		{"lower", `{{"HELLO" | lower}}`, "hello"},
		{"trim", `{{"  hello  " | trim}}`, "hello"},
		{"replace", `{{replace "hello" "l" "L"}}`, "heLLo"},
		{"contains true", `{{if contains "hello" "ell"}}yes{{end}}`, "yes"},
		{"contains false", `{{if contains "hello" "xyz"}}yes{{else}}no{{end}}`, "no"},
		{"hasPrefix true", `{{if hasPrefix "hello" "hel"}}yes{{end}}`, "yes"},
		{"hasPrefix false", `{{if hasPrefix "hello" "xyz"}}yes{{else}}no{{end}}`, "no"},
		{"hasSuffix true", `{{if hasSuffix "hello" "llo"}}yes{{end}}`, "yes"},
		{"hasSuffix false", `{{if hasSuffix "hello" "xyz"}}yes{{else}}no{{end}}`, "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.tmpl, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

// TestEngine_ContextFunctions tests context-based helper functions
func TestEngine_ContextFunctions(t *testing.T) {
	e := New()
	ctx := &Context{
		Header: map[string]string{
			"Authorization": "Bearer token123",
			"X-Tenant":      "acme",
		},
		Query: map[string]string{
			"status": "active",
		},
		Param: map[string]any{
			"id": 42,
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"require header exists", `{{require .Header "Authorization"}}`, "Bearer token123"},
		{"getOr header exists", `{{getOr .Header "X-Tenant" "default"}}`, "acme"},
		{"getOr header missing", `{{getOr .Header "Missing" "default"}}`, "default"},
		{"has header exists", `{{if has .Header "Authorization"}}yes{{end}}`, "yes"},
		{"has header missing", `{{if has .Header "Missing"}}yes{{else}}no{{end}}`, "no"},
		{"require query exists", `{{require .Query "status"}}`, "active"},
		{"getOr query missing", `{{getOr .Query "missing" "all"}}`, "all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.tmpl, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

// TestEngine_RequireFuncError tests require function error case
func TestEngine_RequireFuncError(t *testing.T) {
	e := New()
	ctx := &Context{
		Header: map[string]string{},
		Query:  make(map[string]string),
		Param:  make(map[string]any),
	}

	_, err := e.ExecuteInline(`{{require .Header "Missing"}}`, ctx, UsagePreQuery)
	if err == nil {
		t.Error("expected error for missing required header")
	}
}

// TestEngine_PostQueryContext tests post-query context with Result
func TestEngine_PostQueryContext(t *testing.T) {
	e := New()
	ctx := &Context{
		Header: make(map[string]string),
		Query:  make(map[string]string),
		Param:  make(map[string]any),
		Result: &Result{
			Query:      "test_query",
			Success:    true,
			Count:      5,
			Data:       []map[string]any{{"id": 1}, {"id": 2}},
			Error:      "",
			DurationMs: 42,
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"result query", "{{.Result.Query}}", "test_query"},
		{"result success", "{{.Result.Success}}", "true"},
		{"result count", "{{.Result.Count}}", "5"},
		{"result duration", "{{.Result.DurationMs}}", "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.tmpl, ctx, UsagePostQuery)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

// TestEngine_ConcurrentAccess tests thread safety
func TestEngine_ConcurrentAccess(t *testing.T) {
	e := New()

	// Register a template
	err := e.Register("concurrent", "{{.ClientIP}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			ctx := &Context{
				ClientIP: "192.168.1.1",
				Header:   make(map[string]string),
				Query:    make(map[string]string),
				Param:    make(map[string]any),
			}
			_, err := e.Execute("concurrent", ctx)
			if err != nil {
				t.Errorf("concurrent execute %d failed: %v", idx, err)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestSampleContextMap tests sample context generation
func TestSampleContextMap(t *testing.T) {
	preQuery := sampleContextMap(UsagePreQuery)
	if preQuery["ClientIP"] == "" {
		t.Error("expected ClientIP in pre-query sample")
	}
	if _, ok := preQuery["Result"]; ok {
		t.Error("pre-query sample should not have Result")
	}

	postQuery := sampleContextMap(UsagePostQuery)
	if _, ok := postQuery["Result"]; !ok {
		t.Error("post-query sample should have Result")
	}
}

// TestJSONFunc_Error tests JSON serialization error handling
func TestJSONFunc_Error(t *testing.T) {
	// Channels cannot be marshaled to JSON
	ch := make(chan int)
	result := jsonFunc(ch)
	if result == "" {
		t.Error("expected error message for unmarshalable value")
	}
	if !contains(result, "json error") {
		t.Errorf("expected '[json error: ...]' message, got %q", result)
	}
}

// TestJSONIndentFunc_Error tests indented JSON serialization error handling
func TestJSONIndentFunc_Error(t *testing.T) {
	// Channels cannot be marshaled to JSON
	ch := make(chan int)
	result := jsonIndentFunc(ch)
	if result == "" {
		t.Error("expected error message for unmarshalable value")
	}
	if !contains(result, "json error") {
		t.Errorf("expected '[json error: ...]' message, got %q", result)
	}
}

// contains checks if str contains substr (helper for tests)
func contains(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || len(str) > 0 && containsSubstring(str, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestEngine_ExecuteInline_EmptyResult tests that empty results are rejected
func TestEngine_ExecuteInline_EmptyResult(t *testing.T) {
	e := New()
	ctx := &Context{
		Header: make(map[string]string),
		Query:  make(map[string]string),
		Param:  make(map[string]any),
	}

	// Template that produces empty output
	_, err := e.ExecuteInline("{{if false}}text{{end}}", ctx, UsagePreQuery)
	if err == nil {
		t.Error("expected error for empty result")
	}
	if !containsSubstring(err.Error(), "empty result") {
		t.Errorf("expected 'empty result' in error, got: %v", err)
	}
}

// TestEngine_Execute_TemplateError tests template execution error handling
func TestEngine_Execute_TemplateError(t *testing.T) {
	e := New()

	// Register a template that accesses a missing key
	err := e.Register("error_test", "{{.MissingField.SubField}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx := &Context{
		Header: make(map[string]string),
		Query:  make(map[string]string),
		Param:  make(map[string]any),
	}

	_, err = e.Execute("error_test", ctx)
	if err == nil {
		t.Error("expected error for missing field access")
	}
}

// TestEngine_Validate_StructuralError tests validation with structural template errors
func TestEngine_Validate_StructuralError(t *testing.T) {
	e := New()

	// Test a template that causes a structural error during execution
	// This tests the execution error path that's not about missing map keys
	err := e.Validate("{{range .ClientIP}}{{.}}{{end}}", UsagePreQuery)
	if err == nil {
		t.Error("expected error for ranging over string")
	}
}
