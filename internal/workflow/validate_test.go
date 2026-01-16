package workflow

import (
	"strings"
	"testing"
)

func TestValidate_BasicWorkflow(t *testing.T) {
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
				SQL:      "SELECT 1",
			},
			{
				Name:     "respond",
				Type:     "response",
				Template: `{"success": true}`,
			},
		},
	}

	ctx := &ValidationContext{
		Databases: map[string]bool{"primary": true},
	}

	result := Validate(cfg, ctx)
	if !result.Valid {
		t.Errorf("expected valid workflow, got errors: %v", result.Errors)
	}
}

func TestValidate_MissingName(t *testing.T) {
	cfg := &WorkflowConfig{
		Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
		Steps:    []StepConfig{{Type: "response", Template: "{}"}},
	}

	result := Validate(cfg, nil)
	if result.Valid {
		t.Error("expected validation to fail for missing name")
	}
	if !containsError(result.Errors, "name is required") {
		t.Errorf("expected 'name is required' error, got: %v", result.Errors)
	}
}

func TestValidate_MissingTriggers(t *testing.T) {
	cfg := &WorkflowConfig{
		Name:  "test",
		Steps: []StepConfig{{Type: "response", Template: "{}"}},
	}

	result := Validate(cfg, nil)
	if result.Valid {
		t.Error("expected validation to fail for missing triggers")
	}
	if !containsError(result.Errors, "at least one trigger is required") {
		t.Errorf("expected trigger error, got: %v", result.Errors)
	}
}

func TestValidate_MissingSteps(t *testing.T) {
	cfg := &WorkflowConfig{
		Name:     "test",
		Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
	}

	result := Validate(cfg, nil)
	if result.Valid {
		t.Error("expected validation to fail for missing steps")
	}
	if !containsError(result.Errors, "at least one step is required") {
		t.Errorf("expected step error, got: %v", result.Errors)
	}
}

func TestValidate_HTTPTrigger(t *testing.T) {
	tests := []struct {
		name        string
		trigger     TriggerConfig
		expectError string
	}{
		{
			name:        "missing path",
			trigger:     TriggerConfig{Type: "http", Method: "GET"},
			expectError: "path is required",
		},
		{
			name:        "path without slash",
			trigger:     TriggerConfig{Type: "http", Path: "api/test", Method: "GET"},
			expectError: "path must start with '/'",
		},
		{
			name:        "reserved path prefix",
			trigger:     TriggerConfig{Type: "http", Path: "/_/internal", Method: "GET"},
			expectError: "path cannot start with '/_/'",
		},
		{
			name:        "missing method",
			trigger:     TriggerConfig{Type: "http", Path: "/test"},
			expectError: "method is required",
		},
		{
			name:        "invalid method",
			trigger:     TriggerConfig{Type: "http", Path: "/test", Method: "INVALID"},
			expectError: "method must be GET, POST, PUT, DELETE, PATCH, HEAD, or OPTIONS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{tt.trigger},
				Steps:    []StepConfig{{Type: "response", Template: "{}"}},
			}
			result := Validate(cfg, nil)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

func TestValidate_CronTrigger(t *testing.T) {
	tests := []struct {
		name        string
		trigger     TriggerConfig
		expectError string
	}{
		{
			name:        "missing schedule",
			trigger:     TriggerConfig{Type: "cron"},
			expectError: "schedule is required",
		},
		{
			name:        "invalid cron expression",
			trigger:     TriggerConfig{Type: "cron", Schedule: "invalid"},
			expectError: "invalid schedule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{tt.trigger},
				Steps:    []StepConfig{{Name: "run", Type: "query", Database: "db", SQL: "SELECT 1"}},
			}
			ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
			result := Validate(cfg, ctx)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}

	t.Run("valid cron expression", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{Type: "cron", Schedule: "0 8 * * *"},
			},
			Steps: []StepConfig{
				{Name: "run", Type: "query", Database: "db", SQL: "SELECT 1"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
	})
}

func TestValidate_QueryStep(t *testing.T) {
	tests := []struct {
		name        string
		step        StepConfig
		ctx         *ValidationContext
		expectError string
	}{
		{
			name:        "missing database",
			step:        StepConfig{Name: "q", Type: "query", SQL: "SELECT 1"},
			expectError: "database is required",
		},
		{
			name:        "unknown database",
			step:        StepConfig{Name: "q", Type: "query", Database: "unknown", SQL: "SELECT 1"},
			ctx:         &ValidationContext{Databases: map[string]bool{"primary": true}},
			expectError: "unknown database 'unknown'",
		},
		{
			name:        "missing SQL",
			step:        StepConfig{Name: "q", Type: "query", Database: "db"},
			ctx:         &ValidationContext{Databases: map[string]bool{"db": true}},
			expectError: "sql is required",
		},
		{
			name:        "write on readonly",
			step:        StepConfig{Name: "q", Type: "query", Database: "db", SQL: "INSERT INTO t VALUES (1)"},
			ctx:         &ValidationContext{Databases: map[string]bool{"db": true}},
			expectError: "write operation but database 'db' is read-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps:    []StepConfig{tt.step, {Type: "response", Template: "{}"}},
			}
			result := Validate(cfg, tt.ctx)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

func TestValidate_HTTPCallStep(t *testing.T) {
	tests := []struct {
		name        string
		step        StepConfig
		expectError string
	}{
		{
			name:        "missing URL",
			step:        StepConfig{Name: "h", Type: "httpcall"},
			expectError: "url is required",
		},
		{
			name:        "invalid HTTP method",
			step:        StepConfig{Name: "h", Type: "httpcall", URL: "http://example.com", HTTPMethod: "INVALID"},
			expectError: "invalid http_method",
		},
		{
			name:        "invalid parse mode",
			step:        StepConfig{Name: "h", Type: "httpcall", URL: "http://example.com", Parse: "xml"},
			expectError: "invalid parse mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps:    []StepConfig{tt.step, {Type: "response", Template: "{}"}},
			}
			result := Validate(cfg, nil)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

func TestValidate_ResponseStep(t *testing.T) {
	tests := []struct {
		name        string
		step        StepConfig
		expectError string
	}{
		{
			name:        "missing template",
			step:        StepConfig{Type: "response"},
			expectError: "template is required",
		},
		{
			name:        "invalid status code",
			step:        StepConfig{Type: "response", Template: "{}", StatusCode: 999},
			expectError: "status_code must be 100-599",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps:    []StepConfig{tt.step},
			}
			result := Validate(cfg, nil)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

func TestValidate_BlockStep(t *testing.T) {
	t.Run("valid block with iteration", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{
					Name:     "fetch",
					Type:     "query",
					Database: "db",
					SQL:      "SELECT * FROM items",
				},
				{
					Name: "process", // Name required for multi-step workflow
					Iterate: &IterateConfig{
						Over: "steps.fetch.data",
						As:   "item",
					},
					Steps: []StepConfig{
						{
							Name: "call_api",
							Type: "httpcall",
							URL:  "http://example.com/{{.item.id}}",
						},
					},
				},
				{
					Type:     "response",
					Template: `{"success": true}`,
				},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": false}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
	})

	t.Run("response step in block is error", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{
					Name: "bad_block",
					Steps: []StepConfig{
						{Type: "response", Template: "{}"},
					},
				},
			},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "response steps not allowed in blocks") {
			t.Errorf("expected response-in-block error, got: %v", result.Errors)
		}
	})

	t.Run("empty block is error", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{Name: "empty_block", Steps: []StepConfig{}}, // Empty steps slice creates a block
				{Type: "response", Template: "{}"},
			},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "block must have at least one step") {
			t.Errorf("expected empty-block error, got: %v", result.Errors)
		}
	})

	t.Run("iterate on leaf step is error", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{
					Name:     "bad_iterate",
					Type:     "query",
					Database: "db",
					SQL:      "SELECT 1",
					Iterate: &IterateConfig{
						Over: "steps.fetch.data",
						As:   "item",
					},
				},
				{Type: "response", Template: "{}"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "iterate requires nested steps") {
			t.Errorf("expected iterate-requires-steps error, got: %v", result.Errors)
		}
	})

	t.Run("block with type is error", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{
					Name: "bad_block",
					Type: "query",
					Steps: []StepConfig{
						{Name: "inner", Type: "query", Database: "db", SQL: "SELECT 1"},
					},
				},
				{Type: "response", Template: "{}"},
			},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "step with nested steps cannot have type") {
			t.Errorf("expected block-with-type error, got: %v", result.Errors)
		}
	})

	t.Run("block with sql is error", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{
					Name: "bad_block",
					SQL:  "SELECT 1",
					Steps: []StepConfig{
						{Name: "inner", Type: "httpcall", URL: "http://example.com"},
					},
				},
				{Type: "response", Template: "{}"},
			},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "step with nested steps cannot have sql") {
			t.Errorf("expected block-with-sql error, got: %v", result.Errors)
		}
	})
}

func TestValidate_ConditionAliases(t *testing.T) {
	t.Run("valid condition alias", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Conditions: map[string]string{
				"has_data": "steps.fetch.count > 0",
			},
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT 1"},
				{Type: "response", Template: "{}", Condition: "has_data"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
	})

	t.Run("invalid condition expression", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Conditions: map[string]string{
				"bad": "invalid !! syntax",
			},
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps:    []StepConfig{{Type: "response", Template: "{}"}},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "invalid expression") {
			t.Errorf("expected expression error, got: %v", result.Errors)
		}
	})
}

func TestValidate_Warnings(t *testing.T) {
	t.Run("no response step warning", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{Name: "q", Type: "query", Database: "db", SQL: "SELECT 1"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid with warnings, got errors: %v", result.Errors)
		}
		if !containsWarning(result.Warnings, "no response step") {
			t.Errorf("expected no-response warning, got: %v", result.Warnings)
		}
	})

	t.Run("cron trigger ignores HTTP fields", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{Type: "cron", Schedule: "0 * * * *", Path: "/ignored"},
			},
			Steps: []StepConfig{
				{Name: "q", Type: "query", Database: "db", SQL: "SELECT 1"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid with warnings, got errors: %v", result.Errors)
		}
		if !containsWarning(result.Warnings, "path is ignored for cron") {
			t.Errorf("expected path-ignored warning, got: %v", result.Warnings)
		}
	})
}

func TestValidate_DuplicateStepNames(t *testing.T) {
	cfg := &WorkflowConfig{
		Name:     "test",
		Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
		Steps: []StepConfig{
			{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT 1"},
			{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT 2"},
			{Type: "response", Template: "{}"},
		},
	}
	ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
	result := Validate(cfg, ctx)
	if result.Valid {
		t.Error("expected validation to fail")
	}
	if !containsError(result.Errors, "duplicate step name") {
		t.Errorf("expected duplicate step name error, got: %v", result.Errors)
	}
}

func TestValidate_MultiStepRequiresNames(t *testing.T) {
	cfg := &WorkflowConfig{
		Name:     "test",
		Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
		Steps: []StepConfig{
			{Type: "query", Database: "db", SQL: "SELECT 1"}, // Missing name
			{Type: "response", Template: "{}"},
		},
	}
	ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
	result := Validate(cfg, ctx)
	if result.Valid {
		t.Error("expected validation to fail")
	}
	if !containsError(result.Errors, "name required in multi-step workflow") {
		t.Errorf("expected name-required error, got: %v", result.Errors)
	}
}

func TestValidate_PathParameters(t *testing.T) {
	t.Run("valid path parameter", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{
					Type:   "http",
					Path:   "/api/items/{id}",
					Method: "GET",
					Parameters: []ParamConfig{
						{Name: "id", Type: "int", Required: true},
					},
				},
			},
			Steps: []StepConfig{
				{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT * FROM items WHERE id = @id"},
				{Type: "response", Template: "{}"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
	})

	t.Run("multiple path parameters", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{
					Type:   "http",
					Path:   "/api/users/{user_id}/posts/{post_id}",
					Method: "GET",
					Parameters: []ParamConfig{
						{Name: "user_id", Type: "int", Required: true},
						{Name: "post_id", Type: "int", Required: true},
					},
				},
			},
			Steps: []StepConfig{
				{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT 1"},
				{Type: "response", Template: "{}"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
	})

	t.Run("path parameter without definition", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{
					Type:   "http",
					Path:   "/api/items/{id}",
					Method: "GET",
					// Missing parameter definition for 'id'
				},
			},
			Steps: []StepConfig{{Type: "response", Template: "{}"}},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "path parameter '{id}' must be defined in parameters") {
			t.Errorf("expected path parameter error, got: %v", result.Errors)
		}
	})

	t.Run("path parameter not required", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{
					Type:   "http",
					Path:   "/api/items/{id}",
					Method: "GET",
					Parameters: []ParamConfig{
						{Name: "id", Type: "int", Required: false}, // Path params must be required
					},
				},
			},
			Steps: []StepConfig{{Type: "response", Template: "{}"}},
		}
		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail")
		}
		if !containsError(result.Errors, "path parameter 'id' must be required") {
			t.Errorf("expected path parameter required error, got: %v", result.Errors)
		}
	})

	t.Run("path with query and path params", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test",
			Triggers: []TriggerConfig{
				{
					Type:   "http",
					Path:   "/api/items/{id}",
					Method: "GET",
					Parameters: []ParamConfig{
						{Name: "id", Type: "int", Required: true},          // Path param
						{Name: "include", Type: "string", Required: false}, // Query param (optional)
					},
				},
			},
			Steps: []StepConfig{
				{Name: "fetch", Type: "query", Database: "db", SQL: "SELECT 1"},
				{Type: "response", Template: "{}"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
	})
}

func TestExtractPathParams(t *testing.T) {
	tests := []struct {
		path     string
		expected map[string]bool
	}{
		{
			path:     "/api/items",
			expected: map[string]bool{},
		},
		{
			path:     "/api/items/{id}",
			expected: map[string]bool{"id": true},
		},
		{
			path:     "/api/users/{user_id}/posts/{post_id}",
			expected: map[string]bool{"user_id": true, "post_id": true},
		},
		{
			path:     "/api/{org}/users/{id}/settings",
			expected: map[string]bool{"org": true, "id": true},
		},
		{
			path:     "/{a}/{b}/{c}",
			expected: map[string]bool{"a": true, "b": true, "c": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := ExtractPathParams(tt.path)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d params, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for k := range tt.expected {
				if !result[k] {
					t.Errorf("expected param %q not found in result: %v", k, result)
				}
			}
		})
	}
}

func containsError(errors []string, substr string) bool {
	for _, e := range errors {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

func containsWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestValidate_SQLTemplateInjection(t *testing.T) {
	ctx := &ValidationContext{Databases: map[string]bool{"db": false}}

	tests := []struct {
		name        string
		sql         string
		shouldError bool
	}{
		{
			name:        "parameterized query is valid",
			sql:         "SELECT * FROM users WHERE id = @id",
			shouldError: false,
		},
		{
			name:        "multiple params are valid",
			sql:         "INSERT INTO users (name, email) VALUES (@name, @email)",
			shouldError: false,
		},
		{
			name:        "template interpolation rejected",
			sql:         "SELECT * FROM users WHERE name = '{{.name}}'",
			shouldError: true,
		},
		{
			name:        "template function rejected",
			sql:         "SELECT {{.field}} FROM users",
			shouldError: true,
		},
		{
			name:        "template with pipes rejected",
			sql:         "SELECT * FROM users WHERE status = '{{.status | default \"active\"}}'",
			shouldError: true,
		},
		{
			name:        "complex template rejected",
			sql:         "INSERT INTO tasks (title) VALUES ('{{.task.title}}')",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name: "test",
				Triggers: []TriggerConfig{
					{Type: "http", Path: "/test", Method: "GET"},
				},
				Steps: []StepConfig{
					{Name: "query", Type: "query", Database: "db", SQL: tt.sql},
					{Type: "response", Template: "{}"},
				},
			}
			result := Validate(cfg, ctx)
			if tt.shouldError {
				if result.Valid {
					t.Error("expected validation to fail for template interpolation")
				}
				if !containsError(result.Errors, "template interpolation") {
					t.Errorf("expected template interpolation error, got: %v", result.Errors)
				}
			} else {
				if !result.Valid {
					t.Errorf("expected valid, got errors: %v", result.Errors)
				}
			}
		})
	}
}

func TestContainsTemplateInterpolation(t *testing.T) {
	tests := []struct {
		sql      string
		contains bool
	}{
		{"SELECT * FROM users", false},
		{"SELECT * FROM users WHERE id = @id", false},
		{"SELECT {{.field}} FROM users", true},
		{"INSERT INTO t (x) VALUES ('{{.val}}')", true},
		{"SELECT * FROM {{.table}}", true},
		{"{{ if .cond }}SELECT 1{{ end }}", true},
		{"{single brace}", false},
		{"no template here", false},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			result := containsTemplateInterpolation(tt.sql)
			if result != tt.contains {
				t.Errorf("containsTemplateInterpolation(%q) = %v, want %v", tt.sql, result, tt.contains)
			}
		})
	}
}

// TestValidate_RateLimitPool verifies rate limit validation accepts valid pool references
func TestValidate_RateLimitPool(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test",
		Triggers: []TriggerConfig{{
			Type:   "http",
			Path:   "/test",
			Method: "GET",
			RateLimit: []RateLimitRefConfig{
				{Pool: "default"},
			},
		}},
		Steps: []StepConfig{{Type: "response", Template: "{}"}},
	}

	ctx := &ValidationContext{
		RateLimitPools: map[string]bool{"default": true},
	}

	result := Validate(cfg, ctx)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

// TestValidate_RateLimitInline verifies rate limit validation accepts valid inline config
func TestValidate_RateLimitInline(t *testing.T) {
	cfg := &WorkflowConfig{
		Name: "test",
		Triggers: []TriggerConfig{{
			Type:   "http",
			Path:   "/test",
			Method: "GET",
			RateLimit: []RateLimitRefConfig{
				{RequestsPerSecond: 10, Burst: 20, Key: "{{.ClientIP}}"},
			},
		}},
		Steps: []StepConfig{{Type: "response", Template: "{}"}},
	}

	result := Validate(cfg, nil)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

// TestValidate_RateLimitErrors verifies rate limit validation catches invalid configurations
func TestValidate_RateLimitErrors(t *testing.T) {
	tests := []struct {
		name        string
		rateLimit   RateLimitRefConfig
		ctx         *ValidationContext
		expectError string
	}{
		{
			name:        "both pool and inline",
			rateLimit:   RateLimitRefConfig{Pool: "default", RequestsPerSecond: 10, Burst: 20},
			expectError: "cannot specify both pool and inline",
		},
		{
			name:        "neither pool nor inline",
			rateLimit:   RateLimitRefConfig{},
			expectError: "must specify pool or inline",
		},
		{
			name:        "unknown pool",
			rateLimit:   RateLimitRefConfig{Pool: "nonexistent"},
			ctx:         &ValidationContext{RateLimitPools: map[string]bool{"default": true}},
			expectError: "unknown rate limit pool",
		},
		{
			name:        "inline missing requests_per_second",
			rateLimit:   RateLimitRefConfig{Burst: 20, Key: "{{.ClientIP}}"},
			expectError: "requests_per_second must be positive",
		},
		{
			name:        "inline missing burst",
			rateLimit:   RateLimitRefConfig{RequestsPerSecond: 10, Key: "{{.ClientIP}}"},
			expectError: "burst must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name: "test",
				Triggers: []TriggerConfig{{
					Type:      "http",
					Path:      "/test",
					Method:    "GET",
					RateLimit: []RateLimitRefConfig{tt.rateLimit},
				}},
				Steps: []StepConfig{{Type: "response", Template: "{}"}},
			}

			result := Validate(cfg, tt.ctx)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

// TestValidate_HTTPCallRetry verifies httpcall retry configuration validation
func TestValidate_HTTPCallRetry(t *testing.T) {
	tests := []struct {
		name        string
		retry       *RetryConfig
		expectError string
	}{
		{
			name:        "negative max_attempts",
			retry:       &RetryConfig{MaxAttempts: -1},
			expectError: "max_attempts cannot be negative",
		},
		{
			name:        "negative initial_backoff",
			retry:       &RetryConfig{InitialBackoffSec: -1},
			expectError: "initial_backoff_sec cannot be negative",
		},
		{
			name:        "negative max_backoff",
			retry:       &RetryConfig{MaxBackoffSec: -1},
			expectError: "max_backoff_sec cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps: []StepConfig{
					{Name: "call", Type: "httpcall", URL: "http://example.com", Retry: tt.retry},
					{Type: "response", Template: "{}"},
				},
			}

			result := Validate(cfg, nil)
			if result.Valid {
				t.Error("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
}

// TestValidate_HTTPCallRetryValid verifies valid httpcall retry configuration passes
func TestValidate_HTTPCallRetryValid(t *testing.T) {
	cfg := &WorkflowConfig{
		Name:     "test",
		Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
		Steps: []StepConfig{
			{
				Name:  "call",
				Type:  "httpcall",
				URL:   "http://example.com",
				Retry: &RetryConfig{MaxAttempts: 3, InitialBackoffSec: 1, MaxBackoffSec: 10},
			},
			{Type: "response", Template: "{}"},
		},
	}

	result := Validate(cfg, nil)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}
