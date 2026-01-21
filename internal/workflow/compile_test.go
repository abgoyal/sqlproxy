package workflow

import (
	"bytes"
	"testing"
	"text/template"

	"sql-proxy/internal/publicid"
)

func TestCompile_BasicWorkflow(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test_workflow",
		Triggers: []TriggerConfig{
			{
				Type:   "http",
				Path:   "/api/test",
				Method: "GET",
			},
		},
		Steps: []StepConfig{
			{
				Name:     "fetch",
				Type:     "query",
				Database: "primary",
				SQL:      "SELECT * FROM items WHERE id = {{.trigger.params.id}}",
			},
			{
				Name:       "respond",
				Type:       "response",
				Template:   `{"data": {{.steps.fetch.data}}}`,
				StatusCode: 200,
			},
		},
	}

	compiled, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if compiled.Config.Name != "test_workflow" {
		t.Errorf("expected name 'test_workflow', got %q", compiled.Config.Name)
	}
	if len(compiled.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(compiled.Triggers))
	}
	if len(compiled.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(compiled.Steps))
	}

	// Check that SQL template was compiled
	if compiled.Steps[0].SQLTmpl == nil {
		t.Error("expected SQL template to be compiled")
	}

	// Check that response template was compiled
	if compiled.Steps[1].TemplateTmpl == nil {
		t.Error("expected response template to be compiled")
	}
}

func TestCompile_ConditionAliases(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test",
		Conditions: map[string]string{
			"has_data":  "steps.fetch.count > 0",
			"is_active": "trigger.params.status == 'active'",
		},
		Triggers: []TriggerConfig{
			{Type: "http", Path: "/test", Method: "GET"},
		},
		Steps: []StepConfig{
			{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT 1"},
			{Name: "respond", Type: "response", Template: "{}", Condition: "has_data"},
		},
	}

	compiled, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(compiled.Conditions) != 2 {
		t.Errorf("expected 2 compiled conditions, got %d", len(compiled.Conditions))
	}
	if compiled.Conditions["has_data"] == nil {
		t.Error("expected 'has_data' condition to be compiled")
	}
	if compiled.Conditions["is_active"] == nil {
		t.Error("expected 'is_active' condition to be compiled")
	}
}

func TestCompile_HTTPCallStep(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test",
		Triggers: []TriggerConfig{
			{Type: "http", Path: "/test", Method: "POST"},
		},
		Steps: []StepConfig{
			{
				Name:       "call_api",
				Type:       "httpcall",
				URL:        "https://api.example.com/items/{{.trigger.params.id}}",
				HTTPMethod: "POST",
				Headers: map[string]string{
					"Authorization": "Bearer {{.trigger.headers.api_key}}",
					"Content-Type":  "application/json",
				},
				Body: `{"name": "{{.trigger.params.name}}"}`,
			},
			{Type: "response", Template: "{}"},
		},
	}

	compiled, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	step := compiled.Steps[0]
	if step.URLTmpl == nil {
		t.Error("expected URL template to be compiled")
	}
	if step.BodyTmpl == nil {
		t.Error("expected body template to be compiled")
	}
	if len(step.HeaderTmpls) != 2 {
		t.Errorf("expected 2 header templates, got %d", len(step.HeaderTmpls))
	}
}

func TestCompile_BlockWithIteration(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test",
		Triggers: []TriggerConfig{
			{Type: "http", Path: "/test", Method: "GET"},
		},
		Steps: []StepConfig{
			{
				Name:     "fetch",
				Type:     "query",
				Database: "db",
				SQL:      "SELECT * FROM items",
			},
			{
				Name: "process_items",
				Iterate: &IterateConfig{
					Over:    "steps.fetch.data",
					As:      "item",
					OnError: "continue",
				},
				Steps: []StepConfig{
					{
						Name: "update_item",
						Type: "httpcall",
						URL:  "https://api.example.com/items/{{.item.id}}",
					},
				},
			},
			{Type: "response", Template: "{}"},
		},
	}

	compiled, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	blockStep := compiled.Steps[1]
	if blockStep.Iterate == nil {
		t.Fatal("expected Iterate to be compiled")
	}
	if blockStep.Iterate.OverExpr == nil {
		t.Error("expected iterate.over expression to be compiled")
	}
	if len(blockStep.BlockSteps) != 1 {
		t.Errorf("expected 1 block step, got %d", len(blockStep.BlockSteps))
	}
}

func TestCompile_CacheKeyTemplate(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test",
		Triggers: []TriggerConfig{
			{
				Type:   "http",
				Path:   "/test",
				Method: "GET",
				Cache: &CacheConfig{
					Enabled: true,
					Key:     "items:{{.trigger.params.status | default \"all\"}}",
					TTLSec:  300,
				},
			},
		},
		Steps: []StepConfig{
			{Type: "response", Template: "{}"},
		},
	}

	compiled, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if compiled.Triggers[0].CacheKey == nil {
		t.Error("expected cache key template to be compiled")
	}
}

func TestCompile_InvalidTemplateSyntax(t *testing.T) {
	tests := []struct {
		name string
		cfg  *WorkflowConfig
	}{
		{
			name: "invalid SQL template",
			cfg: &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps: []StepConfig{
					{Name: "q", Type: "query", Database: "db", SQL: "SELECT {{.bad syntax}}"},
					{Type: "response", Template: "{}"},
				},
			},
		},
		{
			name: "invalid URL template",
			cfg: &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps: []StepConfig{
					{Name: "h", Type: "httpcall", URL: "http://{{.bad syntax}}"},
					{Type: "response", Template: "{}"},
				},
			},
		},
		{
			name: "invalid response template",
			cfg: &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps: []StepConfig{
					{Type: "response", Template: "{{.bad syntax}}"},
				},
			},
		},
		{
			name: "invalid condition",
			cfg: &WorkflowConfig{
				Name:       "test",
				Conditions: map[string]string{"bad": "invalid !! syntax"},
				Triggers:   []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps:      []StepConfig{{Type: "response", Template: "{}"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.cfg)
			if err == nil {
				t.Error("expected compile error")
			}
		})
	}
}

func TestResolveCondition(t *testing.T) {
	// Create aliases map with test conditions
	aliases := map[string]*CompiledCondition{
		"found": {
			Source: "steps.fetch.count > 0",
			Prog:   nil, // Will be set below
		},
		"valid_id": {
			Source: "trigger.params.id != \"\"",
			Prog:   nil,
		},
		"is_owner": {
			Source: "steps.fetch.row.owner_id == 123",
			Prog:   nil,
		},
	}

	// Compile all aliases
	for name, cc := range aliases {
		prog, err := compileCondition(cc.Source)
		if err != nil {
			t.Fatalf("failed to compile alias %s: %v", name, err)
		}
		cc.Prog = prog
	}

	// Test cases
	tests := []struct {
		name     string
		cond     string
		env      map[string]any
		expected bool
	}{
		{
			name:     "direct alias true",
			cond:     "found",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"count": 5}}},
			expected: true,
		},
		{
			name:     "direct alias false",
			cond:     "found",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"count": 0}}},
			expected: false,
		},
		{
			name:     "negated alias true (count is 0)",
			cond:     "!found",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"count": 0}}},
			expected: true,
		},
		{
			name:     "negated alias false (count is 5)",
			cond:     "!found",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"count": 5}}},
			expected: false,
		},
		{
			name:     "direct expression",
			cond:     "steps.fetch.count == 10",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"count": 10}}},
			expected: true,
		},
		// Compound expression tests (the fix we implemented)
		{
			name: "compound AND - both true",
			cond: "found && valid_id",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 5}},
				"trigger": map[string]any{"params": map[string]any{"id": "abc123"}},
			},
			expected: true,
		},
		{
			name: "compound AND - first false",
			cond: "found && valid_id",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 0}},
				"trigger": map[string]any{"params": map[string]any{"id": "abc123"}},
			},
			expected: false,
		},
		{
			name: "compound AND - second false",
			cond: "found && valid_id",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 5}},
				"trigger": map[string]any{"params": map[string]any{"id": ""}},
			},
			expected: false,
		},
		{
			name: "compound OR - first true",
			cond: "found || valid_id",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 5}},
				"trigger": map[string]any{"params": map[string]any{"id": ""}},
			},
			expected: true,
		},
		{
			name: "compound OR - second true",
			cond: "found || valid_id",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 0}},
				"trigger": map[string]any{"params": map[string]any{"id": "abc"}},
			},
			expected: true,
		},
		{
			name: "compound OR - both false",
			cond: "found || valid_id",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 0}},
				"trigger": map[string]any{"params": map[string]any{"id": ""}},
			},
			expected: false,
		},
		{
			name: "negated alias in compound",
			cond: "!valid_id || !found",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 5}},
				"trigger": map[string]any{"params": map[string]any{"id": "abc"}},
			},
			expected: false,
		},
		{
			name: "negated alias in compound - one negation true",
			cond: "!valid_id || !found",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 0}},
				"trigger": map[string]any{"params": map[string]any{"id": "abc"}},
			},
			expected: true,
		},
		{
			name: "three aliases combined",
			cond: "found && valid_id && is_owner",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 5, "row": map[string]any{"owner_id": 123}}},
				"trigger": map[string]any{"params": map[string]any{"id": "abc"}},
			},
			expected: true,
		},
		{
			name: "alias with parentheses",
			cond: "(found && valid_id) || is_owner",
			env: map[string]any{
				"steps":   map[string]any{"fetch": map[string]any{"count": 0, "row": map[string]any{"owner_id": 123}}},
				"trigger": map[string]any{"params": map[string]any{"id": ""}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := resolveCondition(tt.cond, aliases)
			if err != nil {
				t.Fatalf("resolveCondition error: %v", err)
			}

			result, err := EvalCondition(prog, tt.env)
			if err != nil {
				t.Fatalf("EvalCondition error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestExpandAliases tests the alias expansion function directly.
func TestExpandAliases(t *testing.T) {
	aliases := map[string]*CompiledCondition{
		"found": {
			Source: "steps.fetch.count > 0",
		},
		"valid_id": {
			Source: "trigger.params.id != \"\"",
		},
		"can_modify": {
			Source: "steps.can_modify.count > 0",
		},
	}

	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "no aliases",
			expr:     "steps.fetch.count > 0",
			expected: "steps.fetch.count > 0",
		},
		{
			name:     "alias at start",
			expr:     "found && x",
			expected: "(steps.fetch.count > 0) && x",
		},
		{
			name:     "alias at end",
			expr:     "x && found",
			expected: "x && (steps.fetch.count > 0)",
		},
		{
			name:     "alias in middle",
			expr:     "a && found && b",
			expected: "a && (steps.fetch.count > 0) && b",
		},
		{
			name:     "multiple different aliases",
			expr:     "found && valid_id",
			expected: "(steps.fetch.count > 0) && (trigger.params.id != \"\")",
		},
		{
			name:     "negated alias",
			expr:     "!found",
			expected: "!(steps.fetch.count > 0)",
		},
		{
			name:     "alias inside parentheses",
			expr:     "(found || valid_id)",
			expected: "((steps.fetch.count > 0) || (trigger.params.id != \"\"))",
		},
		{
			name:     "should not match property path",
			expr:     "steps.can_modify.count > 0",
			expected: "steps.can_modify.count > 0",
		},
		{
			name:     "alias name appears in property path - should not match",
			expr:     "steps.can_modify.count > 0 && can_modify",
			expected: "steps.can_modify.count > 0 && (steps.can_modify.count > 0)",
		},
		{
			name:     "standalone alias vs property access",
			expr:     "can_modify && steps.can_modify.count > 0",
			expected: "(steps.can_modify.count > 0) && steps.can_modify.count > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandAliases(tt.expr, aliases)
			if result != tt.expected {
				t.Errorf("expandAliases(%q)\nexpected: %q\ngot:      %q", tt.expr, tt.expected, result)
			}
		})
	}
}

// TestExpandAliases_EmptyAliases ensures empty alias map returns expression unchanged.
func TestExpandAliases_EmptyAliases(t *testing.T) {
	expr := "steps.fetch.count > 0 && trigger.params.id != \"\""
	result := expandAliases(expr, nil)
	if result != expr {
		t.Errorf("expected unchanged expression with nil aliases, got %q", result)
	}

	result = expandAliases(expr, map[string]*CompiledCondition{})
	if result != expr {
		t.Errorf("expected unchanged expression with empty aliases, got %q", result)
	}
}

func TestEvalCondition(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		env      map[string]any
		expected bool
	}{
		{
			name:     "simple comparison",
			expr:     "row_count > 0",
			env:      map[string]any{"row_count": 5},
			expected: true,
		},
		{
			name:     "nested field access",
			expr:     "steps.fetch.row_count > 0",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"row_count": 10}}},
			expected: true,
		},
		{
			name:     "string comparison",
			expr:     "status == \"active\"",
			env:      map[string]any{"status": "active"},
			expected: true,
		},
		{
			name:     "logical AND",
			expr:     "a && b",
			env:      map[string]any{"a": true, "b": true},
			expected: true,
		},
		{
			name:     "logical OR",
			expr:     "a || b",
			env:      map[string]any{"a": false, "b": true},
			expected: true,
		},
		{
			name:     "false condition",
			expr:     "row_count > 100",
			env:      map[string]any{"row_count": 5},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := compileCondition(tt.expr)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
			result, err := EvalCondition(prog, tt.env)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvalExpression(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		env      map[string]any
		expected any
	}{
		{
			name:     "array access",
			expr:     "items",
			env:      map[string]any{"items": []any{1, 2, 3}},
			expected: []any{1, 2, 3},
		},
		{
			name:     "nested map",
			expr:     "steps.fetch.data",
			env:      map[string]any{"steps": map[string]any{"fetch": map[string]any{"data": "test"}}},
			expected: "test",
		},
		{
			name:     "arithmetic",
			expr:     "a + b",
			env:      map[string]any{"a": 5, "b": 3},
			expected: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := compileExpression(tt.expr)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
			result, err := EvalExpression(prog, tt.env)
			if err != nil {
				t.Fatalf("eval error: %v", err)
			}
			// Simple comparison - for slices/maps would need deeper comparison
			switch v := result.(type) {
			case int:
				if v != tt.expected.(int) {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			case string:
				if v != tt.expected.(string) {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

// TestTemplateFuncs tests all template functions available in workflow templates.
func TestTemplateFuncs(t *testing.T) {
	executeTemplate := func(tmplStr string, data any) (string, error) {
		tmpl, err := template.New("test").Funcs(TemplateFuncs).Parse(tmplStr)
		if err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	t.Run("json", func(t *testing.T) {
		result, err := executeTemplate(`{{json .data}}`, map[string]any{
			"data": map[string]any{"name": "test", "count": 42},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != `{"count":42,"name":"test"}` {
			t.Errorf("unexpected result: %s", result)
		}
	})

	t.Run("jsonIndent", func(t *testing.T) {
		result, err := executeTemplate(`{{jsonIndent .data}}`, map[string]any{
			"data": map[string]any{"name": "test"},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		expected := "{\n  \"name\": \"test\"\n}"
		if result != expected {
			t.Errorf("expected %q, got %q", expected, result)
		}
	})

	t.Run("upper", func(t *testing.T) {
		result, err := executeTemplate(`{{upper .name}}`, map[string]any{"name": "hello"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "HELLO" {
			t.Errorf("expected HELLO, got %s", result)
		}
	})

	t.Run("lower", func(t *testing.T) {
		result, err := executeTemplate(`{{lower .name}}`, map[string]any{"name": "HELLO"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "hello" {
			t.Errorf("expected hello, got %s", result)
		}
	})

	t.Run("trim", func(t *testing.T) {
		result, err := executeTemplate(`{{trim .value}}`, map[string]any{"value": "  hello  "})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "hello" {
			t.Errorf("expected 'hello', got '%s'", result)
		}
	})

	t.Run("replace", func(t *testing.T) {
		result, err := executeTemplate(`{{replace .text "old" "new"}}`, map[string]any{"text": "old value old"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "new value new" {
			t.Errorf("expected 'new value new', got '%s'", result)
		}
	})

	t.Run("contains", func(t *testing.T) {
		result, err := executeTemplate(`{{if contains .text "world"}}yes{{else}}no{{end}}`, map[string]any{"text": "hello world"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "yes" {
			t.Errorf("expected 'yes', got '%s'", result)
		}
	})

	t.Run("hasPrefix", func(t *testing.T) {
		result, err := executeTemplate(`{{if hasPrefix .path "/api"}}api{{else}}other{{end}}`, map[string]any{"path": "/api/users"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "api" {
			t.Errorf("expected 'api', got '%s'", result)
		}
	})

	t.Run("hasSuffix", func(t *testing.T) {
		result, err := executeTemplate(`{{if hasSuffix .file ".json"}}json{{else}}other{{end}}`, map[string]any{"file": "data.json"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "json" {
			t.Errorf("expected 'json', got '%s'", result)
		}
	})

	t.Run("default_with_empty_string", func(t *testing.T) {
		result, err := executeTemplate(`{{.status | default "active"}}`, map[string]any{"status": ""})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "active" {
			t.Errorf("expected 'active', got '%s'", result)
		}
	})

	t.Run("default_with_value", func(t *testing.T) {
		result, err := executeTemplate(`{{.status | default "active"}}`, map[string]any{"status": "inactive"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "inactive" {
			t.Errorf("expected 'inactive', got '%s'", result)
		}
	})

	t.Run("default_with_nil", func(t *testing.T) {
		result, err := executeTemplate(`{{.missing | default "fallback"}}`, map[string]any{})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "fallback" {
			t.Errorf("expected 'fallback', got '%s'", result)
		}
	})

	t.Run("coalesce", func(t *testing.T) {
		result, err := executeTemplate(`{{coalesce .a .b .c}}`, map[string]any{"a": "", "b": "", "c": "third"})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "third" {
			t.Errorf("expected 'third', got '%s'", result)
		}
	})

	t.Run("getOr_with_map_string_any", func(t *testing.T) {
		result, err := executeTemplate(`{{getOr .headers "X-Custom" "default"}}`, map[string]any{
			"headers": map[string]any{"X-Custom": "custom-value"},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "custom-value" {
			t.Errorf("expected 'custom-value', got '%s'", result)
		}
	})

	t.Run("getOr_missing_key", func(t *testing.T) {
		result, err := executeTemplate(`{{getOr .headers "X-Missing" "default"}}`, map[string]any{
			"headers": map[string]any{},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "default" {
			t.Errorf("expected 'default', got '%s'", result)
		}
	})

	t.Run("require_success", func(t *testing.T) {
		result, err := executeTemplate(`{{require .headers "Authorization"}}`, map[string]any{
			"headers": map[string]any{"Authorization": "Bearer token"},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "Bearer token" {
			t.Errorf("expected 'Bearer token', got '%s'", result)
		}
	})

	t.Run("require_missing_key", func(t *testing.T) {
		_, err := executeTemplate(`{{require .headers "Authorization"}}`, map[string]any{
			"headers": map[string]any{},
		})
		if err == nil {
			t.Error("expected error for missing required key")
		}
	})

	t.Run("has_true", func(t *testing.T) {
		result, err := executeTemplate(`{{if has .headers "X-Custom"}}yes{{else}}no{{end}}`, map[string]any{
			"headers": map[string]any{"X-Custom": "value"},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "yes" {
			t.Errorf("expected 'yes', got '%s'", result)
		}
	})

	t.Run("has_false", func(t *testing.T) {
		result, err := executeTemplate(`{{if has .headers "X-Missing"}}yes{{else}}no{{end}}`, map[string]any{
			"headers": map[string]any{},
		})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "no" {
			t.Errorf("expected 'no', got '%s'", result)
		}
	})

	// Math functions use tmpl.BaseFuncMap which returns float64
	t.Run("add", func(t *testing.T) {
		result, err := executeTemplate(`{{add .a .b}}`, map[string]any{"a": 5, "b": 3})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "8" {
			t.Errorf("expected '8', got '%s'", result)
		}
	})

	t.Run("sub", func(t *testing.T) {
		result, err := executeTemplate(`{{sub .a .b}}`, map[string]any{"a": 10, "b": 3})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "7" {
			t.Errorf("expected '7', got '%s'", result)
		}
	})

	t.Run("mul", func(t *testing.T) {
		result, err := executeTemplate(`{{mul .a .b}}`, map[string]any{"a": 4, "b": 5})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "20" {
			t.Errorf("expected '20', got '%s'", result)
		}
	})

	t.Run("div", func(t *testing.T) {
		result, err := executeTemplate(`{{div .a .b}}`, map[string]any{"a": 20, "b": 4})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "5" {
			t.Errorf("expected '5', got '%s'", result)
		}
	})

	t.Run("div_by_zero", func(t *testing.T) {
		// tmpl.BaseFuncMap returns 0 on division by zero (not an error)
		result, err := executeTemplate(`{{div .a .b}}`, map[string]any{"a": 10, "b": 0})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "0" {
			t.Errorf("expected '0' for div by zero, got '%s'", result)
		}
	})

	t.Run("mod", func(t *testing.T) {
		result, err := executeTemplate(`{{mod .a .b}}`, map[string]any{"a": 17, "b": 5})
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "2" {
			t.Errorf("expected '2', got '%s'", result)
		}
	})

	t.Run("mod_by_zero", func(t *testing.T) {
		// tmpl.BaseFuncMap returns 0 on modulo by zero (not an error)
		result, err := executeTemplate(`{{mod .a .b}}`, map[string]any{"a": 10, "b": 0})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "0" {
			t.Errorf("expected '0' for mod by zero, got '%s'", result)
		}
	})
}

// TestTemplateFuncs_InWorkflowContext tests template functions with realistic workflow data.
func TestTemplateFuncs_InWorkflowContext(t *testing.T) {
	executeTemplate := func(tmplStr string, data any) (string, error) {
		tmpl, err := template.New("test").Funcs(TemplateFuncs).Parse(tmplStr)
		if err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	// Simulate workflow context
	ctx := map[string]any{
		"trigger": map[string]any{
			"params": map[string]any{
				"user_id": 123,
				"name":    "john doe",
				"status":  "",
			},
		},
		"steps": map[string]any{
			"fetch": map[string]any{
				"data": []map[string]any{
					{"id": 1, "name": "Item 1"},
					{"id": 2, "name": "Item 2"},
				},
				"count":     2,
				"cache_hit": false,
			},
		},
		"workflow": map[string]any{
			"request_id": "req-123",
		},
	}

	t.Run("response_template_with_json", func(t *testing.T) {
		result, err := executeTemplate(`{"data": {{json .steps.fetch.data}}, "count": {{.steps.fetch.count}}}`, ctx)
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		expected := `{"data": [{"id":1,"name":"Item 1"},{"id":2,"name":"Item 2"}], "count": 2}`
		if result != expected {
			t.Errorf("expected %s, got %s", expected, result)
		}
	})

	t.Run("uppercase_param", func(t *testing.T) {
		result, err := executeTemplate(`{{upper .trigger.params.name}}`, ctx)
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "JOHN DOE" {
			t.Errorf("expected 'JOHN DOE', got '%s'", result)
		}
	})

	t.Run("default_for_empty_param", func(t *testing.T) {
		result, err := executeTemplate(`{{.trigger.params.status | default "active"}}`, ctx)
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "active" {
			t.Errorf("expected 'active', got '%s'", result)
		}
	})

	t.Run("cache_key_with_math", func(t *testing.T) {
		result, err := executeTemplate(`user:{{.trigger.params.user_id}}:page:{{add 1 0}}`, ctx)
		if err != nil {
			t.Fatalf("execute error: %v", err)
		}
		if result != "user:123:page:1" {
			t.Errorf("expected 'user:123:page:1', got '%s'", result)
		}
	})
}

// TestExprFunc_isValidPublicID tests the isValidPublicID expr function.
func TestExprFunc_isValidPublicID(t *testing.T) {
	// Helper to evaluate expression with given environment
	evalExpr := func(exprStr string, env map[string]any) (bool, error) {
		// Add exprFuncs to environment (same as addExprFuncs does at runtime)
		for name, fn := range exprFuncs {
			env[name] = fn
		}
		prog, err := compileCondition(exprStr)
		if err != nil {
			return false, err
		}
		return EvalCondition(prog, env)
	}

	t.Run("encoder_not_configured", func(t *testing.T) {
		// Ensure no encoder is set
		SetTemplateEncoder(nil)

		env := map[string]any{}
		result, err := evalExpr(`isValidPublicID("task", "tsk_ABC123")`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false when encoder not configured")
		}
	})

	// Create encoder for remaining tests
	enc, err := publicid.NewEncoder(
		"test-secret-key-must-be-32-chars!",
		[]publicid.NamespaceConfig{
			{Name: "task", Prefix: "tsk"},
			{Name: "user", Prefix: "usr"},
		},
	)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	// Set encoder and ensure cleanup
	SetTemplateEncoder(enc)
	t.Cleanup(func() { SetTemplateEncoder(nil) })

	t.Run("valid_public_id", func(t *testing.T) {
		// Generate a valid public ID
		validID, err := enc.Encode("task", 12345)
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		env := map[string]any{
			"id": validID,
		}
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != true {
			t.Errorf("expected true for valid public ID %q", validID)
		}
	})

	t.Run("invalid_public_id_format", func(t *testing.T) {
		env := map[string]any{
			"id": "tsk_INVALID!!!",
		}
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false for invalid public ID format")
		}
	})

	t.Run("wrong_namespace", func(t *testing.T) {
		// Generate ID for "task" namespace
		taskID, err := enc.Encode("task", 12345)
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		env := map[string]any{
			"id": taskID,
		}
		// Try to validate against "user" namespace
		result, err := evalExpr(`isValidPublicID("user", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false when validating with wrong namespace")
		}
	})

	t.Run("wrong_prefix_for_namespace", func(t *testing.T) {
		// Generate ID for "user" namespace (has usr_ prefix)
		userID, err := enc.Encode("user", 99999)
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		env := map[string]any{
			"id": userID,
		}
		// Try to validate against "task" namespace (expects tsk_ prefix)
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false when prefix doesn't match namespace")
		}
	})

	t.Run("non_string_input_int", func(t *testing.T) {
		env := map[string]any{
			"id": 12345,
		}
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false for non-string input (int)")
		}
	})

	t.Run("non_string_input_nil", func(t *testing.T) {
		env := map[string]any{
			"id": nil,
		}
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false for nil input")
		}
	})

	t.Run("non_string_input_bool", func(t *testing.T) {
		env := map[string]any{
			"id": true,
		}
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false for non-string input (bool)")
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		env := map[string]any{
			"id": "",
		}
		result, err := evalExpr(`isValidPublicID("task", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false for empty string")
		}
	})

	t.Run("unknown_namespace", func(t *testing.T) {
		validTaskID, err := enc.Encode("task", 12345)
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		env := map[string]any{
			"id": validTaskID,
		}
		result, err := evalExpr(`isValidPublicID("unknown", id)`, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != false {
			t.Error("expected false for unknown namespace")
		}
	})
}
