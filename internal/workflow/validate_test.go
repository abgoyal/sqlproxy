package workflow

import (
	"strings"
	"testing"

	"github.com/robfig/cron/v3"
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

// TestValidateCronExpr verifies the accepted schedule dialect: five fields plus descriptors
func TestValidateCronExpr(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		expectError string // empty means the expression must be accepted
	}{
		// Five-field expressions
		{"daily at 8", "0 8 * * *", ""},
		{"every five minutes", "*/5 * * * *", ""},
		{"mondays", "0 8 * * 1", ""},

		// Named descriptors
		{"hourly", "@hourly", ""},
		{"daily", "@daily", ""},
		{"midnight", "@midnight", ""},
		{"weekly", "@weekly", ""},
		{"monthly", "@monthly", ""},
		{"yearly", "@yearly", ""},
		{"annually", "@annually", ""},

		// @every with intervals the scheduler runs exactly as written
		{"every hour", "@every 1h", ""},
		{"every 30 minutes", "@every 30m", ""},
		{"every 90 seconds", "@every 90s", ""},
		{"compound duration", "@every 1h30m", ""},

		// @every intervals the scheduler would silently alter
		{"sub-second interval", "@every 100ms", "below the 1s minimum"},
		{"zero interval", "@every 0s", "below the 1s minimum"},
		{"negative interval", "@every -1h", "below the 1s minimum"},
		{"fractional seconds", "@every 1500ms", "whole number of seconds"},

		// Timezone prefixes, which the parser consumes before the schedule
		{"tz with descriptor", "TZ=UTC @daily", ""},
		{"tz with five fields", "TZ=UTC 0 8 * * *", ""},
		{"cron_tz with five fields", "CRON_TZ=America/New_York 0 8 * * *", ""},
		{"tz with valid every", "TZ=UTC @every 1h", ""},
		{"tz does not bypass every minimum", "TZ=UTC @every 100ms", "below the 1s minimum"},
		{"cron_tz does not bypass whole seconds", "CRON_TZ=America/New_York @every 1500ms", "whole number of seconds"},
		{"tz without schedule", "TZ=UTC", "must be followed by a schedule"},
		{"cron_tz without schedule", "CRON_TZ=UTC", "must be followed by a schedule"},
		{"unknown timezone", "TZ=Bad/Zone 0 8 * * *", "unknown time zone"},

		// Rejected forms
		{"six fields", "0 0 8 * * *", "expected exactly 5 fields"},
		{"seconds column", "*/30 * * * * *", "expected exactly 5 fields"},
		{"unknown descriptor", "@reboot", "unrecognized descriptor"},
		{"uppercase descriptor", "@DAILY", "unrecognized descriptor"},
		{"gibberish", "invalid", "expected exactly 5 fields"},
		{"empty", "", "empty spec string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCronExpr(tt.expr)
			if tt.expectError == "" {
				if err != nil {
					t.Errorf("validateCronExpr(%q) = %v, want accepted", tt.expr, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateCronExpr(%q) = nil, want error containing %q", tt.expr, tt.expectError)
			}
			if !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("validateCronExpr(%q) = %q, want error containing %q", tt.expr, err, tt.expectError)
			}
		})
	}
}

// TestValidateCronExpr_MatchesScheduler verifies validation never accepts a schedule the scheduler cannot run
func TestValidateCronExpr_MatchesScheduler(t *testing.T) {
	// Expressions the validator rejects that the scheduler would otherwise accept.
	// Each is a deliberate rule; anything else showing up here is accidental
	// strictness that would reject a config the service could actually run.
	intentionallyStricter := map[string]string{
		"@every 100ms":        "clamped up to 1s by cron.Every",
		"@every 1500ms":       "truncated to 1s by cron.Every",
		"@every -1h":          "clamped up to 1s by cron.Every",
		"@every 0s":           "clamped up to 1s by cron.Every",
		"TZ=UTC @every 100ms": "clamped up to 1s by cron.Every",
	}

	corpus := []string{
		// Five-field, including range, list and step forms
		"0 8 * * *", "*/5 * * * *", "0 0 1 1 *", "0 9 * * 1-5", "*/30 9-17 * * 1-5",
		"0 0 1,15 * *", "5 4 * * sun", "0 8 * * *  ", "  0 8 * * *",
		// Descriptors
		"@hourly", "@daily", "@midnight", "@weekly", "@monthly", "@yearly", "@annually",
		"@every 1s", "@every 1h", "@every 90s", "@every 1h30m", "@every 24h",
		"@every 100ms", "@every 1500ms", "@every -1h", "@every 0s", "@every abc", "@every",
		// Timezone prefixes, including the forms that make the upstream parser panic
		"TZ=UTC 0 8 * * *", "TZ=America/New_York @daily", "CRON_TZ=UTC @every 1h",
		"TZ=UTC", "CRON_TZ=UTC", "TZ= 0 8 * * *", "TZ=Bad/Zone 0 8 * * *",
		"TZ=UTC @every 100ms", "TZ=UTC TZ=UTC @daily", "TZ=UTC\t@daily",
		" TZ=UTC @daily", "tz=UTC @daily",
		// Malformed
		"@reboot", "@DAILY", "@Every 1h", "", " ", "0 0 8 * * *", "*/30 * * * * *",
		"invalid", "* * * * * * *",
	}

	for _, expr := range corpus {
		t.Run(expr, func(t *testing.T) {
			validatorErr := validateCronExpr(expr)

			var schedulerErr error
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				_, schedulerErr = cron.New().AddFunc(expr, func() {})
			}()

			if validatorErr == nil {
				if panicked != nil {
					t.Errorf("validation accepted %q but the scheduler panics: %v", expr, panicked)
				}
				if schedulerErr != nil {
					t.Errorf("validation accepted %q but the scheduler rejects it: %v", expr, schedulerErr)
				}
				return
			}

			// Rejected by validation. That is only expected when the scheduler also
			// refuses it, or when it is one of the documented stricter rules.
			if schedulerErr == nil && panicked == nil {
				if _, ok := intentionallyStricter[expr]; !ok {
					t.Errorf("validation rejected %q (%v) but the scheduler accepts it; "+
						"if this strictness is intended, add it to intentionallyStricter",
						expr, validatorErr)
				}
			}
		})
	}
}

// TestValidate_EvictCronSchedule verifies cache evict_cron accepts the same dialect as triggers
func TestValidate_EvictCronSchedule(t *testing.T) {
	tests := []struct {
		name        string
		evictCron   string
		expectError string // empty means the config must validate
	}{
		{"five fields", "0 3 * * *", ""},
		{"descriptor", "@daily", ""},
		{"every", "@every 30m", ""},
		{"timezone prefix", "TZ=UTC @daily", ""},
		{"sub-second every", "@every 100ms", "invalid evict_cron"},
		{"timezone without schedule", "TZ=UTC", "invalid evict_cron"},
		{"gibberish", "not-a-schedule", "invalid evict_cron"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name: "test",
				Triggers: []TriggerConfig{{
					Type: "http", Path: "/test", Method: "GET",
					Cache: &CacheConfig{Enabled: true, Key: "k", EvictCron: tt.evictCron},
				}},
				Steps: []StepConfig{
					{Name: "run", Type: "query", Database: "db", SQL: "SELECT 1"},
					{Type: "response", Template: "{}"},
				},
			}
			ctx := &ValidationContext{Databases: map[string]bool{"db": false}}
			result := Validate(cfg, ctx)

			if tt.expectError == "" {
				if !result.Valid {
					t.Errorf("expected valid, got errors: %v", result.Errors)
				}
				return
			}
			if result.Valid {
				t.Fatal("expected validation to fail")
			}
			if !containsError(result.Errors, tt.expectError) {
				t.Errorf("expected error containing %q, got: %v", tt.expectError, result.Errors)
			}
		})
	}
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

// TestValidate_ReadOnlyWriteDetection verifies the read-only check is literal-aware in both directions
func TestValidate_ReadOnlyWriteDetection(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantValid bool
	}{
		// Must be REJECTED: these can mutate a read-only database
		{"insert", "INSERT INTO t VALUES (1)", false},
		{"merge", "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.x = s.x", false},
		{"exec procedure", "EXEC sp_rebuild_index", false},
		{"execute procedure", "EXECUTE sp_rebuild_index", false},
		{"call procedure", "CALL rebuild_index()", false},
		{"cte with insert", "WITH c AS (SELECT 1) INSERT INTO t SELECT * FROM c", false},
		{"leading semicolon cte", ";WITH c AS (SELECT 1) INSERT INTO t SELECT * FROM c", false},
		{"cte inside batch", "SELECT 1; WITH c AS (SELECT 1) INSERT INTO t SELECT * FROM c", false},
		{"write hidden after leading read", "SELECT 1; DROP TABLE users", false},
		{"write inside if block", "IF EXISTS (SELECT 1 FROM t) BEGIN INSERT INTO t VALUES (1) END", false},
		{"write inside while block", "WHILE (1=1) BEGIN DELETE FROM t END", false},
		{"procedure call inside if", "IF (1=1) EXEC sp_rebuild", false},

		// Must be ACCEPTED: a write keyword appearing in a literal or comment is not a write.
		// Rejecting these would refuse to start the server on a valid config.
		{"delete inside string literal", "SELECT * FROM notes WHERE body = 'DELETE ME'", true},
		{"create inside line comment", "SELECT id FROM t -- CREATE a report", true},
		{"update inside block comment", "SELECT /* UPDATE users SET x = 1 */ id FROM t", true},
		{"exec inside string literal", "SELECT * FROM audit WHERE cmd = 'EXEC sp_evil'", true},
		{"write keyword as column name", "SELECT executed_at, created_at FROM jobs", true},
		{"semicolon inside string literal", "SELECT * FROM t WHERE s = 'a; DROP TABLE users'", true},
		{"quoted identifier", "SELECT [insert] FROM t", true},
		{"read only cte", "WITH c AS (SELECT 1) SELECT * FROM c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &WorkflowConfig{
				Name:     "test",
				Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
				Steps: []StepConfig{
					{Name: "q", Type: "query", Database: "db", SQL: tt.sql},
					{Type: "response", Template: "{}"},
				},
			}
			ctx := &ValidationContext{Databases: map[string]bool{"db": true}} // true = read-only
			result := Validate(cfg, ctx)

			if tt.wantValid && !result.Valid {
				t.Errorf("expected valid, got errors: %v", result.Errors)
			}
			if !tt.wantValid {
				if result.Valid {
					t.Error("expected validation to fail")
				} else if !containsError(result.Errors, "write operation but database 'db' is read-only") {
					t.Errorf("expected read-only error, got: %v", result.Errors)
				}
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

	t.Run("all response steps conditional warning", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{Name: "q", Type: "query", Database: "db", SQL: "SELECT 1"},
				{Type: "response", Template: `{"found": true}`, Condition: "steps.q.count > 0"},
				{Type: "response", Template: `{"found": false}`, Condition: "steps.q.count == 0"},
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid with warnings, got errors: %v", result.Errors)
		}
		if !containsWarning(result.Warnings, "all response steps have conditions") {
			t.Errorf("expected conditional-response warning, got: %v", result.Warnings)
		}
	})

	t.Run("no warning with unconditional fallback response", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name:     "test",
			Triggers: []TriggerConfig{{Type: "http", Path: "/test", Method: "GET"}},
			Steps: []StepConfig{
				{Name: "q", Type: "query", Database: "db", SQL: "SELECT 1"},
				{Type: "response", Template: `{"found": true}`, Condition: "steps.q.count > 0"},
				{Type: "response", Template: `{"found": false}`}, // unconditional fallback
			},
		}
		ctx := &ValidationContext{Databases: map[string]bool{"db": true}}
		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid, got errors: %v", result.Errors)
		}
		if containsWarning(result.Warnings, "all response steps have conditions") {
			t.Errorf("should not warn when unconditional fallback exists, got: %v", result.Warnings)
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
				{RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
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

// TestValidate_DivisionSafety tests that unsafe divisions are caught during validation
func TestValidate_DivisionSafety(t *testing.T) {
	t.Run("rejects_dynamic_divisor_in_condition", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "step1",
					Condition: "total / divisor > 10", // Dynamic divisor - should fail
					Type:      "response",
					Template:  "{}",
				},
			},
		}

		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail for dynamic divisor")
		}
		found := false
		for _, err := range result.Errors {
			if strings.Contains(err, "divOr") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error mentioning divOr, got: %v", result.Errors)
		}
	})

	t.Run("accepts_safe_division_in_condition", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "step1",
					Condition: "total / 2 > 10", // Literal divisor - should pass
					Type:      "response",
					Template:  "{}",
				},
			},
		}

		result := Validate(cfg, nil)
		if !result.Valid {
			t.Errorf("expected valid for literal divisor, got errors: %v", result.Errors)
		}
	})

	t.Run("accepts_divOr_in_condition", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "step1",
					Condition: "divOr(total, divisor, 0) > 10", // divOr is safe
					Type:      "response",
					Template:  "{}",
				},
			},
		}

		result := Validate(cfg, nil)
		if !result.Valid {
			t.Errorf("expected valid for divOr, got errors: %v", result.Errors)
		}
	})

	t.Run("rejects_division_by_zero", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "step1",
					Condition: "total / 0 > 10", // Division by zero - should fail
					Type:      "response",
					Template:  "{}",
				},
			},
		}

		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail for division by zero")
		}
		found := false
		for _, err := range result.Errors {
			if strings.Contains(err, "division by zero") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'division by zero' error, got: %v", result.Errors)
		}
	})
}

// TestValidate_StepReferences tests that step references are validated
func TestValidate_StepReferences(t *testing.T) {
	t.Run("valid_step_reference", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:     "fetch",
					Type:     "query",
					Database: "primary",
					SQL:      "SELECT * FROM items",
				},
				{
					Name:      "respond",
					Type:      "response",
					Condition: "steps.fetch.count > 0", // Valid: fetch is before respond
					Template:  "{}",
				},
			},
		}

		ctx := &ValidationContext{
			Databases: map[string]bool{"primary": false},
		}

		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid workflow, got errors: %v", result.Errors)
		}
	})

	t.Run("unknown_step_reference", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "respond",
					Type:      "response",
					Condition: "steps.nonexistent.count > 0", // Invalid: step doesn't exist
					Template:  "{}",
				},
			},
		}

		result := Validate(cfg, nil)
		if result.Valid {
			t.Error("expected validation to fail for unknown step reference")
		}
		found := false
		for _, err := range result.Errors {
			if strings.Contains(err, "unknown step 'nonexistent'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'unknown step' error, got: %v", result.Errors)
		}
	})

	t.Run("forward_step_reference", func(t *testing.T) {
		// Forward references appear as "unknown step" because the referenced step
		// hasn't been added to stepNames yet during validation
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "first",
					Type:      "response",
					Condition: "steps.second.count > 0", // Invalid: second comes after first
					Template:  "{}",
				},
				{
					Name:     "second",
					Type:     "query",
					Database: "primary",
					SQL:      "SELECT * FROM items",
				},
			},
		}

		ctx := &ValidationContext{
			Databases: map[string]bool{"primary": false},
		}

		result := Validate(cfg, ctx)
		if result.Valid {
			t.Error("expected validation to fail for forward reference")
		}
		found := false
		for _, err := range result.Errors {
			// Forward refs appear as "unknown step" since step isn't registered yet
			if strings.Contains(err, "unknown step 'second'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'unknown step' error for forward reference, got: %v", result.Errors)
		}
	})

	t.Run("step_reference_via_alias", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Conditions: map[string]string{
				"has_data": "steps.fetch.count > 0", // Alias references step 'fetch'
			},
			Steps: []StepConfig{
				{
					Name:      "respond",
					Type:      "response",
					Condition: "has_data", // Uses alias - but 'fetch' doesn't exist yet
					Template:  "{}",
				},
				{
					Name:     "fetch",
					Type:     "query",
					Database: "primary",
					SQL:      "SELECT * FROM items",
				},
			},
		}

		ctx := &ValidationContext{
			Databases: map[string]bool{"primary": false},
		}

		result := Validate(cfg, ctx)
		if result.Valid {
			t.Error("expected validation to fail for alias referencing forward step")
		}
		found := false
		for _, err := range result.Errors {
			if strings.Contains(err, "forward reference") || strings.Contains(err, "unknown step") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected step reference error, got: %v", result.Errors)
		}
	})

	t.Run("self_reference", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:      "fetch",
					Type:      "query",
					Database:  "primary",
					SQL:       "SELECT * FROM items",
					Condition: "steps.fetch.count > 0", // Invalid: references itself
				},
			},
		}

		ctx := &ValidationContext{
			Databases: map[string]bool{"primary": false},
		}

		result := Validate(cfg, ctx)
		if result.Valid {
			t.Error("expected validation to fail for self-reference")
		}
		found := false
		for _, err := range result.Errors {
			if strings.Contains(err, "cannot reference itself") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'cannot reference itself' error, got: %v", result.Errors)
		}
	})

	t.Run("multiple_step_references", func(t *testing.T) {
		cfg := &WorkflowConfig{
			Name: "test_workflow",
			Triggers: []TriggerConfig{
				{Type: "http", Method: "GET", Path: "/test"},
			},
			Steps: []StepConfig{
				{
					Name:     "auth",
					Type:     "query",
					Database: "primary",
					SQL:      "SELECT * FROM users",
				},
				{
					Name:     "fetch",
					Type:     "query",
					Database: "primary",
					SQL:      "SELECT * FROM items",
				},
				{
					Name:      "respond",
					Type:      "response",
					Condition: "steps.auth.count > 0 && steps.fetch.count > 0", // Both valid
					Template:  "{}",
				},
			},
		}

		ctx := &ValidationContext{
			Databases: map[string]bool{"primary": false},
		}

		result := Validate(cfg, ctx)
		if !result.Valid {
			t.Errorf("expected valid workflow, got errors: %v", result.Errors)
		}
	})
}
