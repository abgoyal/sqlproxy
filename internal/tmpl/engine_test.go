package tmpl

import (
	"strings"
	"testing"

	"sql-proxy/internal/publicid"
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

// TestDefaultFuncInTemplates tests both direct and piped forms of default
func TestDefaultFuncInTemplates(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"name":     "Alice",
				"empty":    "",
				"missing":  nil,
				"zero_int": 0,
			},
		},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		// Direct form: {{default "default" .value}}
		{"direct with value", `{{default "N/A" .trigger.params.name}}`, "Alice"},
		{"direct with empty", `{{default "N/A" .trigger.params.empty}}`, "N/A"},
		{"direct with missing", `{{default "N/A" .trigger.params.missing}}`, "N/A"},
		{"direct with zero", `{{default 42 .trigger.params.zero_int}}`, "42"},

		// Piped form: {{.value | default "default"}}
		{"piped with value", `{{.trigger.params.name | default "N/A"}}`, "Alice"},
		{"piped with empty", `{{.trigger.params.empty | default "N/A"}}`, "N/A"},
		{"piped with missing", `{{.trigger.params.missing | default "N/A"}}`, "N/A"},
		{"piped with zero", `{{.trigger.params.zero_int | default 42}}`, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("ExecuteInline error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
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
	err := e.Register("test", "IP={{.trigger.client_ip}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.1",
			Headers:  make(map[string]string),
			Query:    make(map[string]string),
			Params:   make(map[string]any),
		},
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
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
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
		Trigger: &TriggerContext{
			ClientIP: "10.0.0.1",
			Method:   "GET",
			Headers:  make(map[string]string),
			Query:    make(map[string]string),
			Params:   make(map[string]any),
		},
	}

	tests := []struct {
		name    string
		tmpl    string
		want    string
		wantErr bool
	}{
		{"simple", "{{.trigger.client_ip}}", "10.0.0.1", false},
		{"with function", "{{.trigger.method | lower}}", "get", false},
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
		{"valid pre-query", "{{.trigger.client_ip}}", UsagePreQuery, false},
		{"empty template", "", UsagePreQuery, true},
		{"invalid syntax", "{{.Invalid", UsagePreQuery, true},
		{"invalid path prefix", "{{.Param.id}}", UsagePreQuery, true},
		{"invalid path foo", "{{.foo.bar}}", UsagePreQuery, true},
		{"invalid result path", "{{.result.count}}", UsagePreQuery, true},
		{"valid steps path", "{{.steps.fetch.data}}", UsagePreQuery, false},
		{"valid workflow path", "{{.workflow.request_id}}", UsagePreQuery, false},
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
			tmpl:       "{{.trigger.params.status}}",
			paramNames: []string{"status"},
			wantErr:    false,
		},
		{
			name:       "missing param reference",
			tmpl:       "{{.trigger.params.missing}}",
			paramNames: []string{"status"},
			wantErr:    true,
		},
		{
			name:       "multiple params all valid",
			tmpl:       "{{.trigger.params.a}}:{{.trigger.params.b}}",
			paramNames: []string{"a", "b", "c"},
			wantErr:    false,
		},
		{
			name:       "no params referenced",
			tmpl:       "{{.trigger.client_ip}}",
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
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
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
		{"divOr normal", "{{divOr 20 4 -1}}", "5"},
		{"divOr by zero", "{{divOr 20 0 -1}}", "-1"},
		{"divOr by zero custom default", "{{divOr 100 0 999}}", "999"},
		{"mod", "{{mod 10 3}}", "1"},
		{"mod by zero", "{{mod 10 0}}", "0"},
		{"modOr normal", "{{modOr 10 3 -1}}", "1"},
		{"modOr by zero", "{{modOr 10 0 -1}}", "-1"},
		{"modOr by zero custom default", "{{modOr 100 0 999}}", "999"},
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
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
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
		Trigger: &TriggerContext{
			Headers: map[string]string{
				"Authorization": "Bearer token123",
				"X-Tenant":      "acme",
			},
			Query: map[string]string{
				"status": "active",
			},
			Params: map[string]any{
				"id": 42,
			},
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"require header exists", `{{require .trigger.headers "Authorization"}}`, "Bearer token123"},
		{"getOr header exists", `{{getOr .trigger.headers "X-Tenant" "default"}}`, "acme"},
		{"getOr header missing", `{{getOr .trigger.headers "Missing" "default"}}`, "default"},
		{"has header exists", `{{if has .trigger.headers "Authorization"}}yes{{end}}`, "yes"},
		{"has header missing", `{{if has .trigger.headers "Missing"}}yes{{else}}no{{end}}`, "no"},
		{"require query exists", `{{require .trigger.query "status"}}`, "active"},
		{"getOr query missing", `{{getOr .trigger.query "missing" "all"}}`, "all"},
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
		Trigger: &TriggerContext{
			Headers: map[string]string{},
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
	}

	_, err := e.ExecuteInline(`{{require .trigger.headers "Missing"}}`, ctx, UsagePreQuery)
	if err == nil {
		t.Error("expected error for missing required header")
	}
}

// TestEngine_ConcurrentAccess tests thread safety
func TestEngine_ConcurrentAccess(t *testing.T) {
	e := New()

	// Register a template
	err := e.Register("concurrent", "{{.trigger.client_ip}}", UsagePreQuery)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			ctx := &Context{
				Trigger: &TriggerContext{
					ClientIP: "192.168.1.1",
					Headers:  make(map[string]string),
					Query:    make(map[string]string),
					Params:   make(map[string]any),
				},
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
	sample := sampleContextMap(UsagePreQuery)
	if sample["RequestID"] == "" {
		t.Error("expected RequestID in sample")
	}
	if _, ok := sample["trigger"]; !ok {
		t.Error("expected trigger in sample")
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
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
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
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
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
	err := e.Validate("{{range .trigger.client_ip}}{{.}}{{end}}", UsagePreQuery)
	if err == nil {
		t.Error("expected error for ranging over string")
	}
}

// TestToNumber tests the numeric type conversion helper
func TestToNumber(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want float64
	}{
		{"int", 42, 42.0},
		{"int8", int8(8), 8.0},
		{"int16", int16(16), 16.0},
		{"int32", int32(32), 32.0},
		{"int64", int64(64), 64.0},
		{"uint", uint(10), 10.0},
		{"uint8", uint8(8), 8.0},
		{"uint16", uint16(16), 16.0},
		{"uint32", uint32(32), 32.0},
		{"uint64", uint64(64), 64.0},
		{"float32", float32(3.14), 3.14},
		{"float64", float64(3.14), 3.14},
		{"string int", "42", 42.0},
		{"string float", "3.14", 3.14},
		{"string negative", "-5.5", -5.5},
		{"string invalid returns 0", "not a number", 0.0},
		{"nil returns 0", nil, 0.0},
	}

	const epsilon = 0.0001 // Allow small float precision differences

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toNumber(tt.v)
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			if diff > epsilon {
				t.Errorf("toNumber(%v) = %v, want %v (diff %v > epsilon %v)", tt.v, got, tt.want, diff, epsilon)
			}
		})
	}
}

// TestEngine_MathFunctions_Float tests math functions with float values.
// Uses values that are exactly representable in IEEE 754 floating point
// to avoid precision issues (10.5 = 21/2, 2.0 = 2/1, results are dyadic rationals).
func TestEngine_MathFunctions_Float(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params: map[string]any{
				"a": 10.5,
				"b": 2.0,
			},
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"add floats", "{{add .trigger.params.a .trigger.params.b}}", "12.5"},
		{"sub floats", "{{sub .trigger.params.a .trigger.params.b}}", "8.5"},
		{"mul floats", "{{mul .trigger.params.a .trigger.params.b}}", "21"},
		{"div floats", "{{div .trigger.params.a .trigger.params.b}}", "5.25"},
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

// TestEngine_MathFunctions_Extended tests extended math functions
func TestEngine_MathFunctions_Extended(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"round up", "{{round 3.7}}", "4"},
		{"round down", "{{round 3.2}}", "3"},
		{"round half", "{{round 3.5}}", "4"},
		{"floor", "{{floor 3.9}}", "3"},
		{"ceil", "{{ceil 3.1}}", "4"},
		{"trunc positive", "{{trunc 3.9}}", "3"},
		{"trunc negative", "{{trunc -3.9}}", "-3"},
		{"abs positive", "{{abs 5}}", "5"},
		{"abs negative", "{{abs -5}}", "5"},
		{"min", "{{min 5 3}}", "3"},
		{"max", "{{max 5 3}}", "5"},
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

// TestEngine_NumericFormatFunctions tests numeric formatting functions
func TestEngine_NumericFormatFunctions(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			Headers: make(map[string]string),
			Query:   make(map[string]string),
			Params:  make(map[string]any),
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"int64 from int", "{{int64 42}}", "42"},
		{"int64 from float truncates", "{{int64 3.9}}", "3"},
		{"int64 from negative float", "{{int64 -3.9}}", "-3"},
		{"zeropad", `{{zeropad 42 5}}`, "00042"},
		{"zeropad negative", `{{zeropad 7 3}}`, "007"},
		{"pad with zeros", `{{pad 42 5 "0"}}`, "00042"},
		{"pad with spaces", `{{pad 42 5 " "}}`, "   42"},
		{"pad default space", `{{pad "hi" 5 ""}}`, "   hi"},
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

// TestHeaderFunc tests header access with canonical form handling
func TestHeaderFunc(t *testing.T) {
	tests := []struct {
		name    string
		headers any
		key     string
		def     string
		want    string
	}{
		{
			name:    "canonical form match",
			headers: map[string]string{"X-Api-Key": "secret123"},
			key:     "x-api-key",
			want:    "secret123",
		},
		{
			name:    "exact match",
			headers: map[string]string{"X-Api-Key": "secret123"},
			key:     "X-Api-Key",
			want:    "secret123",
		},
		{
			name:    "missing with default",
			headers: map[string]string{},
			key:     "X-Api-Key",
			def:     "no-key",
			want:    "no-key",
		},
		{
			name:    "map[string]any",
			headers: map[string]any{"Authorization": "Bearer token"},
			key:     "authorization",
			want:    "Bearer token",
		},
		{
			name:    "nil headers",
			headers: nil,
			key:     "X-Api-Key",
			def:     "default",
			want:    "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if tt.def != "" {
				got = headerFunc(tt.headers, tt.key, tt.def)
			} else {
				got = headerFunc(tt.headers, tt.key)
			}
			if got != tt.want {
				t.Errorf("headerFunc() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCookieFunc tests cookie access with default value
func TestCookieFunc(t *testing.T) {
	tests := []struct {
		name    string
		cookies any
		key     string
		def     string
		want    string
	}{
		{
			name:    "existing cookie",
			cookies: map[string]string{"session": "abc123"},
			key:     "session",
			want:    "abc123",
		},
		{
			name:    "missing with default",
			cookies: map[string]string{},
			key:     "session",
			def:     "none",
			want:    "none",
		},
		{
			name:    "map[string]any",
			cookies: map[string]any{"user": "john"},
			key:     "user",
			want:    "john",
		},
		{
			name:    "nil cookies",
			cookies: nil,
			key:     "session",
			def:     "default",
			want:    "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if tt.def != "" {
				got = cookieFunc(tt.cookies, tt.key, tt.def)
			} else {
				got = cookieFunc(tt.cookies, tt.key)
			}
			if got != tt.want {
				t.Errorf("cookieFunc() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestArrayHelpers tests first, last, len, pluck, isEmpty functions
func TestArrayHelpers(t *testing.T) {
	t.Run("first", func(t *testing.T) {
		if got := firstFunc([]int{1, 2, 3}); got != 1 {
			t.Errorf("first([1,2,3]) = %v, want 1", got)
		}
		if got := firstFunc([]string{"a", "b"}); got != "a" {
			t.Errorf("first([a,b]) = %v, want a", got)
		}
		if got := firstFunc([]int{}); got != nil {
			t.Errorf("first([]) = %v, want nil", got)
		}
		if got := firstFunc(nil); got != nil {
			t.Errorf("first(nil) = %v, want nil", got)
		}
	})

	t.Run("last", func(t *testing.T) {
		if got := lastFunc([]int{1, 2, 3}); got != 3 {
			t.Errorf("last([1,2,3]) = %v, want 3", got)
		}
		if got := lastFunc([]string{"a", "b"}); got != "b" {
			t.Errorf("last([a,b]) = %v, want b", got)
		}
		if got := lastFunc([]int{}); got != nil {
			t.Errorf("last([]) = %v, want nil", got)
		}
	})

	t.Run("len", func(t *testing.T) {
		if got := lenFunc([]int{1, 2, 3}); got != 3 {
			t.Errorf("len([1,2,3]) = %v, want 3", got)
		}
		if got := lenFunc("hello"); got != 5 {
			t.Errorf("len(hello) = %v, want 5", got)
		}
		if got := lenFunc(map[string]int{"a": 1, "b": 2}); got != 2 {
			t.Errorf("len(map) = %v, want 2", got)
		}
		if got := lenFunc(nil); got != 0 {
			t.Errorf("len(nil) = %v, want 0", got)
		}
		if got := lenFunc(42); got != 0 {
			t.Errorf("len(42) = %v, want 0", got)
		}
	})

	t.Run("pluck", func(t *testing.T) {
		data := []map[string]any{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
			{"id": 3, "name": "Charlie"},
		}
		ids := pluckFunc(data, "id")
		if len(ids) != 3 {
			t.Errorf("pluck(data, id) len = %d, want 3", len(ids))
		}
		if ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
			t.Errorf("pluck(data, id) = %v, want [1,2,3]", ids)
		}

		names := pluckFunc(data, "name")
		if names[0] != "Alice" {
			t.Errorf("pluck(data, name)[0] = %v, want Alice", names[0])
		}

		// Missing field
		missing := pluckFunc(data, "missing")
		if len(missing) != 0 {
			t.Errorf("pluck(data, missing) len = %d, want 0", len(missing))
		}

		// Not a slice
		notSlice := pluckFunc("not a slice", "field")
		if notSlice != nil {
			t.Errorf("pluck(string, field) = %v, want nil", notSlice)
		}
	})

	t.Run("isEmpty", func(t *testing.T) {
		if !isEmptyFunc(nil) {
			t.Error("isEmpty(nil) = false, want true")
		}
		if !isEmptyFunc("") {
			t.Error("isEmpty(\"\") = false, want true")
		}
		if isEmptyFunc("hello") {
			t.Error("isEmpty(hello) = true, want false")
		}
		if !isEmptyFunc([]int{}) {
			t.Error("isEmpty([]) = false, want true")
		}
		if isEmptyFunc([]int{1}) {
			t.Error("isEmpty([1]) = true, want false")
		}
		if !isEmptyFunc(0) {
			t.Error("isEmpty(0) = false, want true")
		}
		if isEmptyFunc(42) {
			t.Error("isEmpty(42) = true, want false")
		}
		if !isEmptyFunc(0.0) {
			t.Error("isEmpty(0.0) = false, want true")
		}
		if !isEmptyFunc(false) {
			t.Error("isEmpty(false) = false, want true")
		}
		if isEmptyFunc(true) {
			t.Error("isEmpty(true) = true, want false")
		}
	})
}

// TestTypeConversions tests float, string, bool functions
func TestTypeConversions(t *testing.T) {
	t.Run("float", func(t *testing.T) {
		if got := floatFunc(42); got != 42.0 {
			t.Errorf("float(42) = %v, want 42.0", got)
		}
		if got := floatFunc("not a number"); got != 0.0 {
			t.Errorf("float(string) = %v, want 0.0", got)
		}
		if got := floatFunc(3.14); got != 3.14 {
			t.Errorf("float(3.14) = %v, want 3.14", got)
		}
	})

	t.Run("string", func(t *testing.T) {
		if got := stringFunc(42); got != "42" {
			t.Errorf("string(42) = %q, want 42", got)
		}
		if got := stringFunc(3.14); got != "3.14" {
			t.Errorf("string(3.14) = %q, want 3.14", got)
		}
		if got := stringFunc(true); got != "true" {
			t.Errorf("string(true) = %q, want true", got)
		}
		if got := stringFunc(nil); got != "" {
			t.Errorf("string(nil) = %q, want empty", got)
		}
	})

	t.Run("bool", func(t *testing.T) {
		if !boolFunc(true) {
			t.Error("bool(true) = false, want true")
		}
		if boolFunc(false) {
			t.Error("bool(false) = true, want false")
		}
		if boolFunc(nil) {
			t.Error("bool(nil) = true, want false")
		}
		if !boolFunc("yes") {
			t.Error("bool(yes) = false, want true")
		}
		if boolFunc("") {
			t.Error("bool(\"\") = true, want false")
		}
		if boolFunc("false") {
			t.Error("bool(false) = true, want false")
		}
		if boolFunc("0") {
			t.Error("bool(0) = true, want false")
		}
		if !boolFunc(1) {
			t.Error("bool(1) = false, want true")
		}
		if boolFunc(0) {
			t.Error("bool(0) = true, want false")
		}
		if !boolFunc(3.14) {
			t.Error("bool(3.14) = false, want true")
		}
		if boolFunc(0.0) {
			t.Error("bool(0.0) = true, want false")
		}
	})
}

// TestEngine_NewFunctionsInTemplates tests new functions work in templates
func TestEngine_NewFunctionsInTemplates(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			Headers: map[string]string{
				"X-Api-Key":     "secret",
				"Authorization": "Bearer token",
			},
			Query: make(map[string]string),
			Params: map[string]any{
				"items": []map[string]any{
					{"id": 1, "name": "Alice"},
					{"id": 2, "name": "Bob"},
				},
				"empty_list": []map[string]any{},
			},
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"header", `{{header .trigger.headers "x-api-key"}}`, "secret"},
		{"header with default", `{{header .trigger.headers "missing" "none"}}`, "none"},
		{"first", `{{first .trigger.params.items | json}}`, `{"id":1,"name":"Alice"}`},
		{"last", `{{last .trigger.params.items | json}}`, `{"id":2,"name":"Bob"}`},
		{"len", `{{len .trigger.params.items}}`, "2"},
		{"pluck", `{{pluck .trigger.params.items "name" | json}}`, `["Alice","Bob"]`},
		{"isEmpty empty slice", `{{if isEmpty .trigger.params.empty_list}}empty{{end}}`, "empty"},
		{"isEmpty slice", `{{if isEmpty .trigger.params.items}}empty{{else}}has items{{end}}`, "has items"},
		{"float", `{{float 42}}`, "42"},
		{"string", `{{string 123}}`, "123"},
		{"bool truthy", `{{if bool 1}}yes{{end}}`, "yes"},
		{"bool falsy", `{{if bool 0}}yes{{else}}no{{end}}`, "no"},
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

// TestIPNetworkFunc tests the ipNetwork template function
func TestIPNetworkFunc(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		prefixes []int
		want     string
	}{
		// IPv4 tests
		{"ipv4 default prefix", "192.168.1.100", nil, "192.168.1.100"},
		{"ipv4 /32 exact", "192.168.1.100", []int{32}, "192.168.1.100"},
		{"ipv4 /24 network", "192.168.1.100", []int{24}, "192.168.1.0"},
		{"ipv4 /16 network", "192.168.1.100", []int{16}, "192.168.0.0"},
		{"ipv4 /8 network", "192.168.1.100", []int{8}, "192.0.0.0"},
		{"ipv4 two prefixes", "192.168.1.100", []int{24, 64}, "192.168.1.0"},

		// IPv6 tests
		{"ipv6 default prefix", "2001:db8::1234", nil, "2001:db8::"},
		{"ipv6 /64 network", "2001:db8:1234:5678:9abc:def0:1234:5678", []int{32, 64}, "2001:db8:1234:5678::"},
		{"ipv6 /48 network", "2001:db8:1234:5678::1", []int{32, 48}, "2001:db8:1234::"},
		{"ipv6 /128 exact", "2001:db8::1234", []int{32, 128}, "2001:db8::1234"},
		{"ipv6 single prefix valid", "2001:db8::1234", []int{48}, "2001:db8::"},

		// IPv4-mapped IPv6 tests
		{"ipv4-mapped ipv6 default", "::ffff:192.168.1.100", nil, "192.168.1.100"},
		{"ipv4-mapped ipv6 /24", "::ffff:192.168.1.100", []int{24}, "192.168.1.0"},

		// Edge cases
		{"invalid ip", "not-an-ip", nil, "not-an-ip"},
		{"localhost ipv4", "127.0.0.1", []int{8}, "127.0.0.0"},
		{"localhost ipv6", "::1", []int{32, 128}, "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ipNetworkFunc(tt.ip, tt.prefixes...)
			if result != tt.want {
				t.Errorf("ipNetwork(%q, %v) = %q, want %q", tt.ip, tt.prefixes, result, tt.want)
			}
		})
	}
}

// TestIPPrefixFunc tests the ipPrefix template function
func TestIPPrefixFunc(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		prefixes []int
		want     string
	}{
		// IPv4 tests
		{"ipv4 default", "192.168.1.100", nil, "192.168.1.100/32"},
		{"ipv4 /24", "192.168.1.100", []int{24}, "192.168.1.0/24"},
		{"ipv4 /16", "192.168.1.100", []int{16}, "192.168.0.0/16"},

		// IPv6 tests
		{"ipv6 default", "2001:db8::1234", nil, "2001:db8::/64"},
		{"ipv6 /48", "2001:db8:1234:5678::1", []int{32, 48}, "2001:db8:1234::/48"},

		// IPv4-mapped IPv6 tests
		{"ipv4-mapped", "::ffff:192.168.1.100", []int{24}, "192.168.1.0/24"},

		// Invalid IP
		{"invalid ip", "not-an-ip", nil, "not-an-ip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ipPrefixFunc(tt.ip, tt.prefixes...)
			if result != tt.want {
				t.Errorf("ipPrefix(%q, %v) = %q, want %q", tt.ip, tt.prefixes, result, tt.want)
			}
		})
	}
}

// TestNormalizeIPFunc tests the normalizeIP template function
func TestNormalizeIPFunc(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		// IPv4-mapped IPv6 normalization
		{"ipv4-mapped to ipv4", "::ffff:192.168.1.1", "192.168.1.1"},
		{"ipv4-mapped to ipv4 2", "::ffff:10.0.0.1", "10.0.0.1"},

		// IPv6 compression
		{"ipv6 compression", "2001:0db8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"ipv6 already compressed", "2001:db8::1", "2001:db8::1"},

		// IPv4 passthrough
		{"ipv4 passthrough", "192.168.1.1", "192.168.1.1"},

		// Invalid IP
		{"invalid ip", "not-an-ip", "not-an-ip"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeIPFunc(tt.ip)
			if result != tt.want {
				t.Errorf("normalizeIP(%q) = %q, want %q", tt.ip, result, tt.want)
			}
		})
	}
}

// TestIPFunctionsInTemplates tests that IP functions work in actual templates
func TestIPFunctionsInTemplates(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.100",
		},
	}

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"ipNetwork in template", `{{ipNetwork .trigger.client_ip 24}}`, "192.168.1.0"},
		{"ipPrefix in template", `{{ipPrefix .trigger.client_ip 24}}`, "192.168.1.0/24"},
		{"normalizeIP in template", `{{normalizeIP .trigger.client_ip}}`, "192.168.1.100"},
		{"rate limit key pattern", `api:{{ipNetwork .trigger.client_ip 24}}`, "api:192.168.1.0"},
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

// TestUUIDFunc tests UUID generation through template engine
func TestUUIDFunc(t *testing.T) {
	e := New()

	// Test uuid() through template
	if err := e.Register("uuid_test", "{{uuid}}", UsagePreQuery); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	ctx := &Context{Trigger: &TriggerContext{}}
	result, err := e.Execute("uuid_test", ctx)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 36 {
		t.Errorf("uuid() length = %d, want 36", len(result))
	}
	// Check hyphen positions
	if result[8] != '-' || result[13] != '-' || result[18] != '-' || result[23] != '-' {
		t.Errorf("uuid() = %q, invalid hyphen positions", result)
	}

	// Test uuid4() alias through template
	if err := e.Register("uuid4_test", "{{uuid4}}", UsagePreQuery); err != nil {
		t.Fatalf("Register uuid4 template failed: %v", err)
	}
	result4, err := e.Execute("uuid4_test", ctx)
	if err != nil {
		t.Fatalf("Execute uuid4 failed: %v", err)
	}
	if len(result4) != 36 {
		t.Errorf("uuid4() length = %d, want 36", len(result4))
	}

	// Test uniqueness - each call should produce a different ID
	result2, err := e.Execute("uuid_test", ctx)
	if err != nil {
		t.Fatalf("Execute second call failed: %v", err)
	}
	if result == result2 {
		t.Error("uuid() should generate unique IDs")
	}
}

// TestUUIDShortFunc tests UUID without hyphens
func TestUUIDShortFunc(t *testing.T) {
	id := uuidShortFunc()
	if len(id) != 32 {
		t.Errorf("uuidShort() length = %d, want 32", len(id))
	}
	// Should not contain hyphens
	if strings.Contains(id, "-") {
		t.Errorf("uuidShort() = %q, should not contain hyphens", id)
	}
	// Should only contain hex characters
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("uuidShort() = %q, contains non-hex character %c", id, c)
			break
		}
	}
}

// TestShortIDFunc tests short ID generation
func TestShortIDFunc(t *testing.T) {
	// Test default length
	id := shortIDFunc()
	if len(id) != 12 {
		t.Errorf("shortID() default length = %d, want 12", len(id))
	}

	// Test custom length
	id8 := shortIDFunc(8)
	if len(id8) != 8 {
		t.Errorf("shortID(8) length = %d, want 8", len(id8))
	}

	id20 := shortIDFunc(20)
	if len(id20) != 20 {
		t.Errorf("shortID(20) length = %d, want 20", len(id20))
	}

	// Test invalid lengths fall back to default
	idNeg := shortIDFunc(-5)
	if len(idNeg) != 12 {
		t.Errorf("shortID(-5) length = %d, want 12 (default)", len(idNeg))
	}

	idZero := shortIDFunc(0)
	if len(idZero) != 12 {
		t.Errorf("shortID(0) length = %d, want 12 (default)", len(idZero))
	}

	idTooLong := shortIDFunc(100)
	if len(idTooLong) != 32 {
		t.Errorf("shortID(100) length = %d, want 32 (capped)", len(idTooLong))
	}

	// Test uniqueness
	id2 := shortIDFunc()
	if id == id2 {
		t.Error("shortID() should generate unique IDs")
	}

	// Test characters are from base62 alphabet
	for _, c := range id {
		if !strings.ContainsRune(base62Alphabet, c) {
			t.Errorf("shortID() = %q, contains invalid character %c", id, c)
			break
		}
	}
}

// TestNanoidFunc tests NanoID generation
func TestNanoidFunc(t *testing.T) {
	// Test default length
	id := nanoidFunc()
	if len(id) != 21 {
		t.Errorf("nanoid() default length = %d, want 21", len(id))
	}

	// Test custom length
	id10 := nanoidFunc(10)
	if len(id10) != 10 {
		t.Errorf("nanoid(10) length = %d, want 10", len(id10))
	}

	// Test uniqueness
	id2 := nanoidFunc()
	if id == id2 {
		t.Error("nanoid() should generate unique IDs")
	}

	// Test characters are from nanoid alphabet
	for _, c := range id {
		if !strings.ContainsRune(nanoidAlphabet, c) {
			t.Errorf("nanoid() = %q, contains invalid character %c", id, c)
			break
		}
	}
}

// TestIDFunctionsInTemplates tests UUID/ID functions in actual templates
func TestIDFunctionsInTemplates(t *testing.T) {
	e := New()
	ctx := &Context{}

	// Test uuid generates 36-char string
	result, err := e.ExecuteInline(`{{uuid}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("uuid template error: %v", err)
	}
	if len(result) != 36 {
		t.Errorf("uuid in template: length = %d, want 36", len(result))
	}

	// Test uuid4 (alias)
	result, err = e.ExecuteInline(`{{uuid4}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("uuid4 template error: %v", err)
	}
	if len(result) != 36 {
		t.Errorf("uuid4 in template: length = %d, want 36", len(result))
	}

	// Test uuidShort
	result, err = e.ExecuteInline(`{{uuidShort}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("uuidShort template error: %v", err)
	}
	if len(result) != 32 {
		t.Errorf("uuidShort in template: length = %d, want 32", len(result))
	}

	// Test shortID with length
	result, err = e.ExecuteInline(`{{shortID 16}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("shortID template error: %v", err)
	}
	if len(result) != 16 {
		t.Errorf("shortID 16 in template: length = %d, want 16", len(result))
	}

	// Test nanoid with length
	result, err = e.ExecuteInline(`{{nanoid 24}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("nanoid template error: %v", err)
	}
	if len(result) != 24 {
		t.Errorf("nanoid 24 in template: length = %d, want 24", len(result))
	}

	// Test in composite template
	result, err = e.ExecuteInline(`order:{{shortID 8}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("composite template error: %v", err)
	}
	if !strings.HasPrefix(result, "order:") {
		t.Errorf("composite template = %q, want prefix 'order:'", result)
	}
	if len(result) != 14 { // "order:" (6) + shortID (8)
		t.Errorf("composite template length = %d, want 14", len(result))
	}
}

// TestPublicIDFunc tests the publicID template function
func TestPublicIDFunc(t *testing.T) {
	e := New()

	// Test without encoder configured - should return error
	_, err := e.publicIDFunc("user", 42)
	if err == nil {
		t.Error("expected error when encoder not configured")
	}
	if !strings.Contains(err.Error(), "encoder not configured") {
		t.Errorf("error should mention encoder not configured, got: %v", err)
	}

	// Configure encoder
	enc, err := publicid.NewEncoder("this-is-a-secret-key-that-is-32chars", []publicid.NamespaceConfig{
		{Name: "user", Prefix: "usr"},
		{Name: "order", Prefix: "ord"},
	})
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}
	e.SetPublicIDEncoder(enc)

	tests := []struct {
		name      string
		namespace string
		id        any
		wantErr   bool
		errMsg    string
	}{
		{"int id", "user", 42, false, ""},
		{"int64 id", "user", int64(12345), false, ""},
		{"float64 id", "user", float64(999), false, ""},
		{"different namespace", "order", 1, false, ""},
		{"zero id", "user", 0, false, ""},
		{"large id", "user", int64(9223372036854775807), false, ""},
		{"unknown namespace", "unknown", 1, true, "unknown namespace"},
		{"invalid type string", "user", "not-a-number", true, "invalid id type"},
		{"invalid type bool", "user", true, true, "invalid id type"},
		{"invalid type slice", "user", []int{1, 2}, true, "invalid id type"},
		{"invalid type map", "user", map[string]int{"a": 1}, true, "invalid id type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.publicIDFunc(tt.namespace, tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("publicIDFunc() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
			}
			if !tt.wantErr && result == "" {
				t.Error("publicIDFunc() returned empty string")
			}
		})
	}
}

// TestPrivateIDFunc tests the privateID template function
func TestPrivateIDFunc(t *testing.T) {
	e := New()

	// Test without encoder configured - should return error
	_, err := e.privateIDFunc("user", "usr_00000000001")
	if err == nil {
		t.Error("expected error when encoder not configured")
	}
	if !strings.Contains(err.Error(), "encoder not configured") {
		t.Errorf("error should mention encoder not configured, got: %v", err)
	}

	// Configure encoder
	enc, err := publicid.NewEncoder("this-is-a-secret-key-that-is-32chars", []publicid.NamespaceConfig{
		{Name: "user", Prefix: "usr"},
		{Name: "order", Prefix: "ord"},
	})
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}
	e.SetPublicIDEncoder(enc)

	// First encode a valid ID to get a valid public ID for testing
	validPublicID, err := e.publicIDFunc("user", int64(42))
	if err != nil {
		t.Fatalf("failed to encode test ID: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		publicID  string
		wantErr   bool
		errMsg    string
	}{
		{"valid public ID", "user", validPublicID, false, ""},
		{"unknown namespace", "unknown", validPublicID, true, "unknown namespace"},
		{"invalid prefix for namespace", "user", "ord_00000000001", true, "invalid prefix"},
		{"wrong namespace prefix", "order", validPublicID, true, "invalid prefix"},
		{"invalid base62 characters", "user", "usr_!@#$%^&*()", true, "invalid"},
		{"empty public ID", "user", "", true, "invalid prefix"},
		{"prefix only error", "user", "usr_", true, "invalid length"},     // Empty encoded part rejected
		{"missing prefix separator", "user", "usr00000000001", true, "invalid prefix"},
		{"too short encoded part", "user", "usr_abc", true, "invalid length"}, // Must be exactly 11 chars
		{"no prefix configured namespace", "order", "ord_" + strings.Repeat("0", 11), false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.privateIDFunc(tt.namespace, tt.publicID)
			if (err != nil) != tt.wantErr {
				t.Errorf("privateIDFunc() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

// TestPublicPrivateIDRoundTrip tests encoding and decoding produces original value
func TestPublicPrivateIDRoundTrip(t *testing.T) {
	e := New()
	enc, err := publicid.NewEncoder("this-is-a-secret-key-that-is-32chars", []publicid.NamespaceConfig{
		{Name: "user", Prefix: "usr"},
	})
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}
	e.SetPublicIDEncoder(enc)

	tests := []int64{0, 1, 42, 12345, 9223372036854775807}
	for _, id := range tests {
		publicID, err := e.publicIDFunc("user", id)
		if err != nil {
			t.Errorf("publicIDFunc(%d) error: %v", id, err)
			continue
		}

		decoded, err := e.privateIDFunc("user", publicID)
		if err != nil {
			t.Errorf("privateIDFunc(%q) error: %v", publicID, err)
			continue
		}

		if decoded != id {
			t.Errorf("round-trip failed: got %d, want %d", decoded, id)
		}
	}
}

// TestPublicIDInTemplates tests publicID/privateID functions in templates
func TestPublicIDInTemplates(t *testing.T) {
	e := New()
	enc, err := publicid.NewEncoder("this-is-a-secret-key-that-is-32chars", []publicid.NamespaceConfig{
		{Name: "user", Prefix: "usr"},
	})
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}
	e.SetPublicIDEncoder(enc)

	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "192.168.1.1",
			Method:   "GET",
			Path:     "/test",
		},
	}

	// Test publicID in template
	result, err := e.ExecuteInline(`{{publicID "user" 42}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("publicID template error: %v", err)
	}
	if !strings.HasPrefix(result, "usr_") {
		t.Errorf("publicID in template = %q, want prefix 'usr_'", result)
	}

	// Test with Param
	ctx.Trigger.Params = map[string]any{"user_id": int64(123)}
	result, err = e.ExecuteInline(`{{publicID "user" .trigger.params.user_id}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("publicID with Param error: %v", err)
	}
	if !strings.HasPrefix(result, "usr_") {
		t.Errorf("publicID with Param = %q, want prefix 'usr_'", result)
	}
}

// ============================================================================
// Phase 10: Validation helpers tests
// ============================================================================

func TestValidationHelpers(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		// isEmail
		{"valid email", `{{isEmail "user@example.com"}}`, "true"},
		{"invalid email no @", `{{isEmail "userexample.com"}}`, "false"},
		{"invalid email no domain", `{{isEmail "user@"}}`, "false"},

		// isUUID
		{"valid uuid", `{{isUUID "550e8400-e29b-41d4-a716-446655440000"}}`, "true"},
		{"invalid uuid", `{{isUUID "not-a-uuid"}}`, "false"},

		// isURL
		{"valid url", `{{isURL "https://example.com/path"}}`, "true"},
		{"invalid url no scheme", `{{isURL "example.com"}}`, "false"},

		// isIP
		{"valid ipv4", `{{isIP "192.168.1.1"}}`, "true"},
		{"valid ipv6", `{{isIP "2001:db8::1"}}`, "true"},
		{"invalid ip", `{{isIP "not-an-ip"}}`, "false"},

		// isIPv4
		{"isIPv4 true", `{{isIPv4 "192.168.1.1"}}`, "true"},
		{"isIPv4 false for v6", `{{isIPv4 "2001:db8::1"}}`, "false"},

		// isIPv6
		{"isIPv6 true", `{{isIPv6 "2001:db8::1"}}`, "true"},
		{"isIPv6 false for v4", `{{isIPv6 "192.168.1.1"}}`, "false"},

		// isNumeric
		{"numeric int", `{{isNumeric "123"}}`, "true"},
		{"numeric float", `{{isNumeric "123.45"}}`, "true"},
		{"numeric negative", `{{isNumeric "-123"}}`, "true"},
		{"not numeric", `{{isNumeric "abc"}}`, "false"},

		// matches
		{"matches true", `{{matches "^[a-z]+$" "hello"}}`, "true"},
		{"matches false", `{{matches "^[a-z]+$" "Hello123"}}`, "false"},
	}

	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Phase 11: Encoding/hashing tests
// ============================================================================

func TestEncodingHashingFuncs(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		// URL encoding
		{"urlEncode", `{{urlEncode "hello world"}}`, "hello+world"},
		{"urlEncode special", `{{urlEncode "a=b&c=d"}}`, "a%3Db%26c%3Dd"},
		{"urlDecode", `{{urlDecode "hello+world"}}`, "hello world"},
		{"urlDecodeOr success", `{{urlDecodeOr "hello+world" "default"}}`, "hello world"},
		{"urlDecodeOr invalid", `{{urlDecodeOr "%ZZ" "default"}}`, "default"},

		// Base64
		{"base64Encode", `{{base64Encode "hello"}}`, "aGVsbG8="},
		{"base64Decode", `{{base64Decode "aGVsbG8="}}`, "hello"},
		{"base64DecodeOr success", `{{base64DecodeOr "aGVsbG8=" "default"}}`, "hello"},
		{"base64DecodeOr invalid", `{{base64DecodeOr "!!invalid!!" "default"}}`, "default"},

		// Hashing (known values)
		{"sha256", `{{sha256 "hello"}}`, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"md5", `{{md5 "hello"}}`, "5d41402abc4b2a76b9719d911017c592"},

		// HMAC
		{"hmacSHA256", `{{hmacSHA256 "key" "message"}}`, "6e9ef29b75fffc5b7abae527d58fdadb2fe42e7219011976917343065f58ed4a"},
	}

	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Phase 13: String helpers tests
// ============================================================================

func TestStringHelpers(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		// truncate
		{"truncate short", `{{truncate "hello" 10}}`, "hello"},
		{"truncate long", `{{truncate "hello world" 8}}`, "hello..."},
		{"truncate custom suffix", `{{truncate "hello world" 8 "!"}}`, "hello w!"},

		// split/join
		{"split", `{{len (split "," "a,b,c")}}`, "3"},
		{"join", `{{join "-" (split "," "a,b,c")}}`, "a-b-c"},

		// substr
		{"substr from start", `{{substr "hello" 0 3}}`, "hel"},
		{"substr middle", `{{substr "hello" 1 3}}`, "ell"},
		{"substr negative", `{{substr "hello" -2}}`, "lo"},

		// quote
		{"quote", `{{quote "hello"}}`, `"hello"`},

		// sprintf
		{"sprintf", `{{sprintf "%s-%d" "test" 42}}`, "test-42"},

		// repeat
		{"repeat", `{{repeat "ab" 3}}`, "ababab"},
	}

	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Phase 14: Date/time tests
// ============================================================================

func TestDateTimeFuncs(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	// Test now() returns a non-empty string
	result, err := e.ExecuteInline(`{{now}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("now() error: %v", err)
	}
	if result == "" {
		t.Error("now() returned empty string")
	}

	// Test now with format
	result, err = e.ExecuteInline(`{{now "YYYY-MM-DD"}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("now(format) error: %v", err)
	}
	if len(result) != 10 { // YYYY-MM-DD is 10 chars
		t.Errorf("now(format) = %q, expected 10 chars", result)
	}

	// Test formatTime with Unix timestamp
	result, err = e.ExecuteInline(`{{formatTime 1704067200 "YYYY-MM-DD"}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("formatTime error: %v", err)
	}
	if result != "2024-01-01" {
		t.Errorf("formatTime = %q, want '2024-01-01'", result)
	}

	// Test unixTime returns a number
	result, err = e.ExecuteInline(`{{unixTime}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("unixTime error: %v", err)
	}
	if result == "" || result == "0" {
		t.Error("unixTime returned empty or zero")
	}
}

// ============================================================================
// Phase 17: JSON helpers tests
// ============================================================================

func TestJSONHelpers(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"data": map[string]any{
					"a": 1,
					"b": 2,
					"c": 3,
				},
			},
		},
	}

	// Test pick
	result, err := e.ExecuteInline(`{{json (pick .trigger.params.data "a" "c")}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("pick error: %v", err)
	}
	// Result should contain a and c but not b
	if !strings.Contains(result, `"a"`) || !strings.Contains(result, `"c"`) {
		t.Errorf("pick result missing expected keys: %s", result)
	}
	if strings.Contains(result, `"b"`) {
		t.Errorf("pick result should not contain 'b': %s", result)
	}

	// Test omit
	result, err = e.ExecuteInline(`{{json (omit .trigger.params.data "b")}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("omit error: %v", err)
	}
	if strings.Contains(result, `"b"`) {
		t.Errorf("omit result should not contain 'b': %s", result)
	}
}

// ============================================================================
// Phase 18: Conditional helpers tests
// ============================================================================

func TestConditionalHelpers(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"ternary true", `{{ternary true "yes" "no"}}`, "yes"},
		{"ternary false", `{{ternary false "yes" "no"}}`, "no"},
		{"when true", `{{when true "shown"}}`, "shown"},
		// when false returns empty string, so we use it with a prefix
		{"when false with prefix", `prefix{{when false "hidden"}}suffix`, "prefixsuffix"},
	}

	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Phase 19: Safe navigation tests
// ============================================================================

func TestDigFunc(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"nested": map[string]any{
					"deep": map[string]any{
						"value": "found",
					},
				},
				"arr": []any{
					map[string]any{"name": "first"},
					map[string]any{"name": "second"},
				},
			},
		},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"dig nested", `{{dig .trigger.params "nested" "deep" "value"}}`, "found"},
		{"dig array", `{{dig .trigger.params "arr" 0 "name"}}`, "first"},
		{"dig missing returns nil", `{{dig .trigger.params "missing" "path"}}`, "<no value>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Phase 20: Debug helpers tests
// ============================================================================

func TestDebugHelpers(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"str": "hello",
				"num": 42,
			},
		},
	}

	// Test typeOf
	result, err := e.ExecuteInline(`{{typeOf .trigger.params.str}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("typeOf error: %v", err)
	}
	if result != "string" {
		t.Errorf("typeOf string = %q, want 'string'", result)
	}

	result, err = e.ExecuteInline(`{{typeOf .trigger.params.num}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("typeOf error: %v", err)
	}
	if result != "int" {
		t.Errorf("typeOf int = %q, want 'int'", result)
	}

	// Test keys
	result, err = e.ExecuteInline(`{{len (keys .trigger.params)}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("keys error: %v", err)
	}
	if result != "2" {
		t.Errorf("keys count = %q, want '2'", result)
	}
}

// ============================================================================
// Phase 21: Numeric formatting tests
// ============================================================================

func TestNumericFormatting(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"formatNumber int", `{{formatNumber 1234567}}`, "1,234,567"},
		{"formatNumber decimals", `{{formatNumber 1234.5678 2}}`, "1,234.57"},
		{"formatNumber negative", `{{formatNumber -1234567}}`, "-1,234,567"},

		{"formatPercent", `{{formatPercent 0.1234}}`, "12.3%"},
		{"formatPercent 2 decimals", `{{formatPercent 0.1234 2}}`, "12.34%"},

		{"formatBytes small", `{{formatBytes 500}}`, "500 B"},
		{"formatBytes KB", `{{formatBytes 1536}}`, "1.5 KB"},
		{"formatBytes MB", `{{formatBytes 1572864}}`, "1.5 MB"},
		{"formatBytes GB", `{{formatBytes 1610612736}}`, "1.5 GB"},
		{"formatBytes negative small", `{{formatBytes -500}}`, "-500 B"},
		{"formatBytes negative KB", `{{formatBytes -1536}}`, "-1.5 KB"},
		{"formatBytes negative MB", `{{formatBytes -1572864}}`, "-1.5 MB"},
	}

	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Additional coverage tests for edge cases and negative tests
// ============================================================================

func TestParseTimeFunc(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"parse RFC3339", `{{parseTime "2024-01-01T00:00:00Z"}}`, "1704067200"},
		{"parse with format", `{{parseTime "2024-01-01" "YYYY-MM-DD"}}`, "1704067200"},
		{"parse invalid returns 0", `{{parseTime "invalid"}}`, "0"},
		// parseTimeOr - with explicit default
		{"parseTimeOr success", `{{parseTimeOr "2024-01-01T00:00:00Z" -1}}`, "1704067200"},
		{"parseTimeOr invalid", `{{parseTimeOr "invalid" -1}}`, "-1"},
		{"parseTimeOr custom default", `{{parseTimeOr "invalid" 999}}`, "999"},
		{"parseTimeOr with format", `{{parseTimeOr "2024-01-01" -1 "YYYY-MM-DD"}}`, "1704067200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestMergeFunc(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"m1": map[string]any{"a": 1, "b": 2},
				"m2": map[string]any{"b": 3, "c": 4},
			},
		},
	}

	// Test that merge combines maps with later values overriding
	result, err := e.ExecuteInline(`{{json (merge .trigger.params.m1 .trigger.params.m2)}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("merge error: %v", err)
	}
	// b should be 3 (from m2), a should be 1, c should be 4
	if !strings.Contains(result, `"a":1`) || !strings.Contains(result, `"b":3`) || !strings.Contains(result, `"c":4`) {
		t.Errorf("merge result incorrect: %s", result)
	}
}

func TestValuesFunc(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"data": map[string]any{"a": 1, "b": 2},
			},
		},
	}

	// Test values returns correct count
	result, err := e.ExecuteInline(`{{len (values .trigger.params.data)}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("values error: %v", err)
	}
	if result != "2" {
		t.Errorf("values count = %q, want '2'", result)
	}

	// Test values on non-map returns nil/empty
	ctx.Trigger.Params["notmap"] = "string"
	result, err = e.ExecuteInline(`{{len (values .trigger.params.notmap)}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("values error: %v", err)
	}
	if result != "0" {
		t.Errorf("values on non-map should return nil (len 0), got len %s", result)
	}
}

func TestFormatTimeEdgeCases(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	tests := []struct {
		name     string
		template string
		check    func(string) bool
	}{
		{
			"formatTime with string",
			`{{formatTime "2024-01-01T12:00:00Z" "YYYY-MM-DD"}}`,
			func(s string) bool { return s == "2024-01-01" },
		},
		{
			"formatTime with invalid string returns original",
			`{{formatTime "not-a-date" "YYYY-MM-DD"}}`,
			func(s string) bool { return s == "not-a-date" },
		},
		{
			"formatTime with float64",
			`{{formatTime 1704067200.0 "YYYY-MM-DD"}}`,
			func(s string) bool { return s == "2024-01-01" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if !tt.check(result) {
				t.Errorf("got %q", result)
			}
		})
	}
}

func TestDigFuncEdgeCases(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"strmap": map[string]string{"key": "value"},
				"arr":    []any{"first", "second"},
				"mapArr": []map[string]any{{"id": 1}, {"id": 2}},
			},
		},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"dig into string map", `{{dig .trigger.params "strmap" "key"}}`, "value"},
		{"dig string map missing key", `{{dig .trigger.params "strmap" "missing"}}`, "<no value>"},
		{"dig into slice with string index", `{{dig .trigger.params "arr" "0"}}`, "first"},
		{"dig into mapArr", `{{dig .trigger.params "mapArr" 1 "id"}}`, "2"},
		{"dig out of bounds", `{{dig .trigger.params "arr" 99}}`, "<no value>"},
		{"dig negative index", `{{dig .trigger.params "arr" -1}}`, "<no value>"},
		{"dig with int64 index", `prefix{{dig .trigger.params "arr" 0}}suffix`, "prefixfirstsuffix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestTypeOfNil(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params:   map[string]any{"nilval": nil},
		},
	}

	result, err := e.ExecuteInline(`{{typeOf .trigger.params.nilval}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("typeOf error: %v", err)
	}
	if result != "nil" {
		t.Errorf("typeOf nil = %q, want 'nil'", result)
	}
}

func TestUrlDecodeError(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	// Invalid URL encoding should return original
	result, err := e.ExecuteInline(`{{urlDecode "%ZZ"}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("urlDecode error: %v", err)
	}
	if result != "%ZZ" {
		t.Errorf("urlDecode invalid = %q, want '%%ZZ'", result)
	}
}

func TestBase64DecodeError(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	// Invalid base64 now returns original string for consistency with urlDecode
	result, err := e.ExecuteInline(`prefix{{base64Decode "!!invalid!!"}}suffix`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("base64Decode error: %v", err)
	}
	if result != "prefix!!invalid!!suffix" {
		t.Errorf("base64Decode invalid = %q, want 'prefix!!invalid!!suffix'", result)
	}
}

func TestMatchesInvalidRegex(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	// Invalid regex should return false
	result, err := e.ExecuteInline(`{{matches "[invalid" "test"}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("matches error: %v", err)
	}
	if result != "false" {
		t.Errorf("matches invalid regex = %q, want 'false'", result)
	}
}

func TestSubstrEdgeCases(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		// start beyond length returns empty - wrap to avoid empty result error
		{"start beyond length", `prefix{{substr "hello" 100}}suffix`, "prefixsuffix"},
		{"negative start clamped", `{{substr "hello" -100}}`, "hello"},
		{"length exceeds string", `{{substr "hello" 0 100}}`, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("template error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	// When maxLen is less than suffix length
	result, err := e.ExecuteInline(`{{truncate "hello world" 2}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("truncate error: %v", err)
	}
	if result != "he" {
		t.Errorf("truncate small maxLen = %q, want 'he'", result)
	}
}

func TestJoinNonSlice(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	// Join on non-slice should return string representation
	result, err := e.ExecuteInline(`{{join "-" "notslice"}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("join error: %v", err)
	}
	if result != "notslice" {
		t.Errorf("join non-slice = %q, want 'notslice'", result)
	}
}

func TestKeysNonMap(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params:   map[string]any{"notmap": "string"},
		},
	}

	// keys on non-map returns nil which becomes empty
	result, err := e.ExecuteInline(`{{len (keys .trigger.params.notmap)}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("keys error: %v", err)
	}
	if result != "0" {
		t.Errorf("keys non-map len = %q, want '0'", result)
	}
}

func TestToIntConversions(t *testing.T) {
	e := New()
	ctx := &Context{
		Trigger: &TriggerContext{
			ClientIP: "127.0.0.1",
			Method:   "GET",
			Path:     "/test",
			Params: map[string]any{
				"arr": []any{"a", "b", "c"},
			},
		},
	}

	// Test dig with string index (uses toInt)
	result, err := e.ExecuteInline(`{{dig .trigger.params "arr" "1"}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("dig with string index error: %v", err)
	}
	if result != "b" {
		t.Errorf("dig string index = %q, want 'b'", result)
	}

	// Test dig with int64
	ctx.Trigger.Params["idx"] = int64(2)
	result, err = e.ExecuteInline(`{{dig .trigger.params "arr" 2}}`, ctx, UsagePreQuery)
	if err != nil {
		t.Fatalf("dig with int64 index error: %v", err)
	}
	if result != "c" {
		t.Errorf("dig int64 index = %q, want 'c'", result)
	}
}

func TestAndFunc(t *testing.T) {
	tests := []struct {
		name string
		args []bool
		want bool
	}{
		{"no args", []bool{}, true},
		{"single true", []bool{true}, true},
		{"single false", []bool{false}, false},
		{"two true", []bool{true, true}, true},
		{"two false", []bool{false, false}, false},
		{"true false", []bool{true, false}, false},
		{"false true", []bool{false, true}, false},
		{"three true", []bool{true, true, true}, true},
		{"three mixed", []bool{true, false, true}, false},
		{"four true", []bool{true, true, true, true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := andFunc(tt.args...)
			if got != tt.want {
				t.Errorf("andFunc(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestOrFunc(t *testing.T) {
	tests := []struct {
		name string
		args []bool
		want bool
	}{
		{"no args", []bool{}, false},
		{"single true", []bool{true}, true},
		{"single false", []bool{false}, false},
		{"two true", []bool{true, true}, true},
		{"two false", []bool{false, false}, false},
		{"true false", []bool{true, false}, true},
		{"false true", []bool{false, true}, true},
		{"three false", []bool{false, false, false}, false},
		{"three mixed", []bool{false, true, false}, true},
		{"four false", []bool{false, false, false, false}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orFunc(tt.args...)
			if got != tt.want {
				t.Errorf("orFunc(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestBooleanOperatorsInTemplates(t *testing.T) {
	e := New()
	ctx := &Context{Trigger: &TriggerContext{ClientIP: "127.0.0.1", Method: "GET", Path: "/test"}}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"and two args", `{{and true true}}`, "true"},
		{"and three args", `{{and true true true}}`, "true"},
		{"and three args with false", `{{and true false true}}`, "false"},
		{"or two args", `{{or false true}}`, "true"},
		{"or three args", `{{or false false false}}`, "false"},
		{"or three args with true", `{{or false true false}}`, "true"},
		{"not true", `{{not true}}`, "false"},
		{"not false", `{{not false}}`, "true"},
		// Complex expressions like in validation workflow
		{"complex and", `{{and (gt 5 0) (ge 3 0) (le 3 10)}}`, "true"},
		{"complex and fails", `{{and (gt 5 0) (ge 11 0) (le 11 10)}}`, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.ExecuteInline(tt.template, ctx, UsagePreQuery)
			if err != nil {
				t.Fatalf("ExecuteInline error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

// ============================================================================
// Direct unit tests for validation functions
// ============================================================================

func TestIsEmailFunc(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"user@example.com", true},
		{"user.name@example.com", true},
		{"user+tag@example.com", true},
		{"user@sub.example.com", true},
		{"user@example.co.uk", true},
		{"", false},
		{"user", false},
		{"@example.com", false},
		{"user@", false},
		{"user@.com", false},
		{"user@example", false},
		{"user example.com", false},
		{"user@@example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isEmailFunc(tt.input)
			if got != tt.want {
				t.Errorf("isEmailFunc(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsUUIDFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid uuid v4", "550e8400-e29b-41d4-a716-446655440000", true},
		{"valid uuid v1", "6ba7b810-9dad-11d1-80b4-00c04fd430c8", true},
		{"uppercase uuid", "550E8400-E29B-41D4-A716-446655440000", true},
		{"nil uuid", "00000000-0000-0000-0000-000000000000", true},
		{"empty string", "", false},
		{"too short", "550e8400-e29b-41d4-a716", false},
		{"invalid characters", "550e8400-e29b-41d4-a716-44665544zzzz", false},
		{"no hyphens accepted", "550e8400e29b41d4a716446655440000", true}, // Go's uuid.Parse accepts this format
		{"random string", "not-a-uuid-at-all", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUUIDFunc(tt.input)
			if got != tt.want {
				t.Errorf("isUUIDFunc(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsURLFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"https url", "https://example.com", true},
		{"http url", "http://example.com", true},
		{"url with path", "https://example.com/path/to/page", true},
		{"url with query", "https://example.com?foo=bar", true},
		{"url with port", "https://example.com:8080", true},
		{"ftp url", "ftp://files.example.com", true},
		{"no scheme", "example.com", false},
		{"no host", "https://", false},
		{"empty string", "", false},
		{"relative path", "/path/to/page", false},
		{"mailto without host", "mailto:", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isURLFunc(tt.input)
			if got != tt.want {
				t.Errorf("isURLFunc(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsIPFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ipv4 localhost", "127.0.0.1", true},
		{"ipv4 private", "192.168.1.1", true},
		{"ipv4 broadcast", "255.255.255.255", true},
		{"ipv6 localhost", "::1", true},
		{"ipv6 full", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", true},
		{"ipv6 compressed", "2001:db8::1", true},
		{"ipv4-mapped ipv6", "::ffff:192.168.1.1", true},
		{"empty string", "", false},
		{"hostname", "example.com", false},
		{"ipv4 with port", "192.168.1.1:8080", false},
		{"ipv4 out of range", "256.256.256.256", false},
		{"partial ipv4", "192.168.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPFunc(tt.input)
			if got != tt.want {
				t.Errorf("isIPFunc(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsIPv4Func(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ipv4 localhost", "127.0.0.1", true},
		{"ipv4 private", "192.168.1.1", true},
		{"ipv4 public", "8.8.8.8", true},
		{"ipv6 localhost", "::1", false},
		{"ipv6 address", "2001:db8::1", false},
		{"ipv4-mapped ipv6", "::ffff:192.168.1.1", true}, // Treated as IPv4
		{"empty string", "", false},
		{"hostname", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPv4Func(tt.input)
			if got != tt.want {
				t.Errorf("isIPv4Func(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsIPv6Func(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ipv6 localhost", "::1", true},
		{"ipv6 full", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", true},
		{"ipv6 compressed", "2001:db8::1", true},
		{"ipv4-mapped ipv6", "::ffff:192.168.1.1", false}, // Treated as IPv4, not IPv6
		{"ipv4 localhost", "127.0.0.1", false},
		{"ipv4 private", "192.168.1.1", false},
		{"empty string", "", false},
		{"hostname", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPv6Func(tt.input)
			if got != tt.want {
				t.Errorf("isIPv6Func(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsNumericFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"positive integer", "123", true},
		{"negative integer", "-123", true},
		{"zero", "0", true},
		{"positive float", "123.45", true},
		{"negative float", "-123.45", true},
		{"scientific notation", "1.23e10", true},
		{"negative scientific", "-1.23e-10", true},
		{"leading zeros", "00123", true},
		{"empty string", "", false},
		{"letters", "abc", false},
		{"mixed", "123abc", false},
		{"multiple dots", "1.2.3", false},
		{"only minus", "-", false},
		{"spaces", "1 2 3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNumericFunc(tt.input)
			if got != tt.want {
				t.Errorf("isNumericFunc(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchesFunc(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"simple match", "hello", "hello world", true},
		{"no match", "hello", "goodbye world", false},
		{"anchor start", "^hello", "hello world", true},
		{"anchor start no match", "^hello", "say hello", false},
		{"anchor end", "world$", "hello world", true},
		{"anchor end no match", "world$", "world peace", false},
		{"full match", "^hello$", "hello", true},
		{"full match no match", "^hello$", "hello world", false},
		{"character class", "[a-z]+", "hello", true},
		{"character class no match", "^[a-z]+$", "Hello", false},
		{"digits", `\d+`, "abc123def", true},
		{"digits no match", `^\d+$`, "abc123", false},
		{"empty pattern", "", "anything", true},
		{"empty input", "hello", "", false},
		{"invalid regex", "[invalid", "test", false},
		{"special chars escaped", `\.`, "test.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesFunc(tt.pattern, tt.input)
			if got != tt.want {
				t.Errorf("matchesFunc(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Direct unit tests for encoding functions
// ============================================================================

func TestUrlEncodeFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello", "hello"},
		{"space", "hello world", "hello+world"},
		{"special chars", "a=b&c=d", "a%3Db%26c%3Dd"},
		{"unicode", "hello", "hello"},
		{"slash", "path/to/file", "path%2Fto%2Ffile"},
		{"question mark", "query?param", "query%3Fparam"},
		{"plus sign", "a+b", "a%2Bb"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlEncodeFunc(tt.input)
			if got != tt.want {
				t.Errorf("urlEncodeFunc(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestUrlDecodeFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello", "hello"},
		{"plus to space", "hello+world", "hello world"},
		{"percent encoded", "a%3Db%26c%3Dd", "a=b&c=d"},
		{"slash encoded", "path%2Fto%2Ffile", "path/to/file"},
		{"invalid encoding returns original", "%ZZ", "%ZZ"},
		{"incomplete encoding returns original", "%2", "%2"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlDecodeFunc(tt.input)
			if got != tt.want {
				t.Errorf("urlDecodeFunc(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestUrlDecodeOrFunc(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultVal string
		want       string
	}{
		{"valid encoding", "hello+world", "default", "hello world"},
		{"invalid encoding uses default", "%ZZ", "fallback", "fallback"},
		{"empty default", "%ZZ", "", ""},
		{"empty input", "", "default", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urlDecodeOrFunc(tt.input, tt.defaultVal)
			if got != tt.want {
				t.Errorf("urlDecodeOrFunc(%q, %q) = %q, want %q", tt.input, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestBase64EncodeFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hello", "hello", "aGVsbG8="},
		{"empty string", "", ""},
		{"with padding", "a", "YQ=="},
		{"unicode", "hello world", "aGVsbG8gd29ybGQ="},
		{"binary-like", "\x00\x01\x02", "AAEC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := base64EncodeFunc(tt.input)
			if got != tt.want {
				t.Errorf("base64EncodeFunc(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBase64DecodeFunc(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hello", "aGVsbG8=", "hello"},
		{"empty string", "", ""},
		{"with padding", "YQ==", "a"},
		{"unicode", "aGVsbG8gd29ybGQ=", "hello world"},
		{"invalid returns original", "!!invalid!!", "!!invalid!!"},
		{"no padding returns original", "YQ", "YQ"}, // StdEncoding requires proper padding
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := base64DecodeFunc(tt.input)
			if got != tt.want {
				t.Errorf("base64DecodeFunc(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBase64DecodeOrFunc(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultVal string
		want       string
	}{
		{"valid encoding", "aGVsbG8=", "default", "hello"},
		{"invalid encoding uses default", "!!invalid!!", "fallback", "fallback"},
		{"empty default", "!!invalid!!", "", ""},
		{"empty input", "", "default", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := base64DecodeOrFunc(tt.input, tt.defaultVal)
			if got != tt.want {
				t.Errorf("base64DecodeOrFunc(%q, %q) = %q, want %q", tt.input, tt.defaultVal, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Direct unit tests for hash functions
// ============================================================================

func TestSHA256Func(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hello", "hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"empty string", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hello world", "hello world", "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sha256Func(tt.input)
			if got != tt.want {
				t.Errorf("sha256Func(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMD5Func(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hello", "hello", "5d41402abc4b2a76b9719d911017c592"},
		{"empty string", "", "d41d8cd98f00b204e9800998ecf8427e"},
		{"hello world", "hello world", "5eb63bbbe01eeed093cb22bb8f5acdc3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := md5Func(tt.input)
			if got != tt.want {
				t.Errorf("md5Func(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHmacSHA256Func(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		message string
		want    string
	}{
		{"standard", "key", "message", "6e9ef29b75fffc5b7abae527d58fdadb2fe42e7219011976917343065f58ed4a"},
		{"empty key", "", "message", "eb08c1f56d5ddee07f7bdf80468083da06b64cf4fac64fe3a90883df5feacae4"},
		{"empty message", "key", "", "5d5d139563c95b5967b9bd9a8c9b233a9dedb45072794cd232dc1b74832607d0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hmacSHA256Func(tt.key, tt.message)
			if got != tt.want {
				t.Errorf("hmacSHA256Func(%q, %q) = %q, want %q", tt.key, tt.message, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Additional IP function edge case tests
// ============================================================================

func TestIPNetworkFuncEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		prefixes []int
		want     string
	}{
		{"zero prefix", "192.168.1.100", []int{0}, "192.168.1.100"},
		{"negative prefix", "192.168.1.100", []int{-1}, "192.168.1.100"},
		{"prefix too large for ipv4", "192.168.1.100", []int{33}, "192.168.1.100"},
		{"ipv6 prefix 0", "2001:db8::1", []int{32, 0}, "2001:db8::"},
		{"ipv6 prefix too large", "2001:db8::1", []int{32, 129}, "2001:db8::"},
		{"empty string", "", nil, ""},
		{"whitespace", " ", nil, " "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ipNetworkFunc(tt.ip, tt.prefixes...)
			if result != tt.want {
				t.Errorf("ipNetworkFunc(%q, %v) = %q, want %q", tt.ip, tt.prefixes, result, tt.want)
			}
		})
	}
}

func TestIPPrefixFuncEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		prefixes []int
		want     string
	}{
		{"invalid returns original", "not-an-ip", nil, "not-an-ip"},
		{"empty returns original", "", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ipPrefixFunc(tt.ip, tt.prefixes...)
			if result != tt.want {
				t.Errorf("ipPrefixFunc(%q, %v) = %q, want %q", tt.ip, tt.prefixes, result, tt.want)
			}
		})
	}
}

// ============================================================================
// Additional UUID/ID function tests
// ============================================================================

func TestShortIDFuncCharacterSet(t *testing.T) {
	// Generate many IDs and verify all characters are from base62 alphabet
	for i := 0; i < 100; i++ {
		id := shortIDFunc(16)
		for _, c := range id {
			if !strings.ContainsRune(base62Alphabet, c) {
				t.Errorf("shortIDFunc generated invalid character %c in %q", c, id)
				return
			}
		}
	}
}

func TestNanoidFuncCharacterSet(t *testing.T) {
	// Generate many IDs and verify all characters are from nanoid alphabet
	for i := 0; i < 100; i++ {
		id := nanoidFunc(16)
		for _, c := range id {
			if !strings.ContainsRune(nanoidAlphabet, c) {
				t.Errorf("nanoidFunc generated invalid character %c in %q", c, id)
				return
			}
		}
	}
}

func TestNanoidFuncEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		length int
		want   int
	}{
		{"default", -1, 21},
		{"zero", 0, 21},
		{"negative", -10, 21},
		{"small", 5, 5},
		{"large capped", 50, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			if tt.length < 0 && tt.name == "default" {
				got = nanoidFunc()
			} else {
				got = nanoidFunc(tt.length)
			}
			if len(got) != tt.want {
				t.Errorf("nanoidFunc(%d) length = %d, want %d", tt.length, len(got), tt.want)
			}
		})
	}
}

// TestExprFuncs verifies ExprFuncs returns all expected functions
func TestExprFuncs(t *testing.T) {
	funcs := ExprFuncs()

	// Note: "contains" and "matches" are built-in expr operators, not functions
	expected := []string{
		// Safe division/modulo
		"divOr", "modOr",
		// String functions
		"upper", "lower", "trim", "hasPrefix", "hasSuffix",
		// Collection helpers
		"len", "isEmpty", "first", "last",
		// Validation helpers
		"isEmail", "isUUID", "isURL", "isIP", "isIPv4", "isIPv6", "isNumeric",
		// Coalesce
		"coalesce",
	}

	for _, name := range expected {
		if _, ok := funcs[name]; !ok {
			t.Errorf("ExprFuncs missing expected function %q", name)
		}
	}
}

// TestExprFuncs_DivOr tests the divOr function from ExprFuncs
func TestExprFuncs_DivOr(t *testing.T) {
	funcs := ExprFuncs()
	divOr := funcs["divOr"].(func(any, any, any) float64)

	tests := []struct {
		name     string
		a, b     any
		fallback any
		want     float64
	}{
		{"normal division", 10, 2, -1, 5},
		{"division by zero", 10, 0, -1, -1},
		{"division by zero custom fallback", 100, 0, 999, 999},
		{"float division", 7.5, 2.5, -1, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := divOr(tt.a, tt.b, tt.fallback)
			if got != tt.want {
				t.Errorf("divOr(%v, %v, %v) = %v, want %v", tt.a, tt.b, tt.fallback, got, tt.want)
			}
		})
	}
}

// TestExprFuncs_ModOr tests the modOr function from ExprFuncs
func TestExprFuncs_ModOr(t *testing.T) {
	funcs := ExprFuncs()
	modOr := funcs["modOr"].(func(any, any, any) float64)

	tests := []struct {
		name     string
		a, b     any
		fallback any
		want     float64
	}{
		{"normal modulo", 10, 3, -1, 1},
		{"modulo by zero", 10, 0, -1, -1},
		{"modulo by zero custom fallback", 100, 0, 999, 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modOr(tt.a, tt.b, tt.fallback)
			if got != tt.want {
				t.Errorf("modOr(%v, %v, %v) = %v, want %v", tt.a, tt.b, tt.fallback, got, tt.want)
			}
		})
	}
}
