package workflow

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/expr-lang/expr"
)

func TestNewContext(t *testing.T) {
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{
			Name: "test_workflow",
		},
	}
	trigger := &TriggerData{
		Type:   "http",
		Params: map[string]any{"id": 123},
	}
	logger := &testLogger{}

	ctx := NewContext(context.Background(), wf, trigger, "req-123", logger, nil)

	if ctx.Workflow != wf {
		t.Error("Workflow not set correctly")
	}
	if ctx.Trigger != trigger {
		t.Error("Trigger not set correctly")
	}
	if ctx.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want %q", ctx.RequestID, "req-123")
	}
	if ctx.Logger != logger {
		t.Error("Logger not set correctly")
	}
	if ctx.Steps == nil {
		t.Error("Steps map not initialized")
	}
}

func TestContext_Context(t *testing.T) {
	bgCtx := context.Background()
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
	trigger := &TriggerData{Type: "http"}

	ctx := NewContext(bgCtx, wf, trigger, "req-1", &testLogger{}, nil)

	if ctx.Context() != bgCtx {
		t.Error("Context() should return the underlying context")
	}
}

func TestContext_SetGetStepResult(t *testing.T) {
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
	trigger := &TriggerData{Type: "http"}
	ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

	result := &StepResult{
		Name:    "fetch_users",
		Type:    "query",
		Success: true,
		Count:   5,
	}

	ctx.SetStepResult("fetch_users", result)

	got := ctx.GetStepResult("fetch_users")
	if got != result {
		t.Error("GetStepResult did not return the set result")
	}

	notFound := ctx.GetStepResult("nonexistent")
	if notFound != nil {
		t.Error("GetStepResult should return nil for nonexistent step")
	}
}

func TestContext_BuildExprEnv_HTTPTrigger(t *testing.T) {
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test_workflow"}}
	trigger := &TriggerData{
		Type:     "http",
		Params:   map[string]any{"status": "active"},
		Headers:  http.Header{"Authorization": []string{"Bearer token123"}},
		Cookies:  map[string]string{"session": "abc123", "user": "john"},
		ClientIP: "192.168.1.1",
		Method:   "POST",
		Path:     "/api/users",
	}
	ctx := NewContext(context.Background(), wf, trigger, "req-123", &testLogger{}, nil)

	// Add a step result
	ctx.SetStepResult("step1", &StepResult{
		Name:    "step1",
		Type:    "query",
		Success: true,
		Data:    []map[string]any{{"id": 1, "name": "Alice"}},
		Count:   1,
	})

	env := ctx.BuildExprEnv()

	// Check steps
	steps, ok := env["steps"].(map[string]any)
	if !ok {
		t.Fatal("steps not found in env")
	}
	step1, ok := steps["step1"].(map[string]any)
	if !ok {
		t.Fatal("step1 not found in steps")
	}
	if step1["success"] != true {
		t.Errorf("step1.success = %v, want true", step1["success"])
	}
	if step1["count"] != 1 {
		t.Errorf("step1.count = %v, want 1", step1["count"])
	}

	// Check trigger
	triggerEnv, ok := env["trigger"].(map[string]any)
	if !ok {
		t.Fatal("trigger not found in env")
	}
	if triggerEnv["type"] != "http" {
		t.Errorf("trigger.type = %v, want http", triggerEnv["type"])
	}
	if triggerEnv["method"] != "POST" {
		t.Errorf("trigger.method = %v, want POST", triggerEnv["method"])
	}
	if triggerEnv["client_ip"] != "192.168.1.1" {
		t.Errorf("trigger.client_ip = %v, want 192.168.1.1", triggerEnv["client_ip"])
	}

	// Check cookies
	cookies, ok := triggerEnv["cookies"].(map[string]string)
	if !ok {
		t.Fatal("trigger.cookies not found or wrong type")
	}
	if cookies["session"] != "abc123" {
		t.Errorf("trigger.cookies.session = %v, want abc123", cookies["session"])
	}
	if cookies["user"] != "john" {
		t.Errorf("trigger.cookies.user = %v, want john", cookies["user"])
	}

	// Check workflow
	workflow, ok := env["workflow"].(map[string]any)
	if !ok {
		t.Fatal("workflow not found in env")
	}
	if workflow["name"] != "test_workflow" {
		t.Errorf("workflow.name = %v, want test_workflow", workflow["name"])
	}
	if workflow["request_id"] != "req-123" {
		t.Errorf("workflow.request_id = %v, want req-123", workflow["request_id"])
	}
}

func TestContext_BuildExprEnv_CronTrigger(t *testing.T) {
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "scheduled_workflow"}}
	schedTime := time.Now()
	trigger := &TriggerData{
		Type:         "cron",
		ScheduleTime: schedTime,
		CronExpr:     "0 8 * * *",
	}
	ctx := NewContext(context.Background(), wf, trigger, "cron-123", &testLogger{}, nil)

	env := ctx.BuildExprEnv()

	triggerEnv, ok := env["trigger"].(map[string]any)
	if !ok {
		t.Fatal("expected trigger to be map[string]any")
	}
	if triggerEnv["type"] != "cron" {
		t.Errorf("trigger.type = %v, want cron", triggerEnv["type"])
	}
	if triggerEnv["schedule_time"] != schedTime {
		t.Errorf("trigger.schedule_time mismatch")
	}
	if triggerEnv["cron"] != "0 8 * * *" {
		t.Errorf("trigger.cron = %v, want '0 8 * * *'", triggerEnv["cron"])
	}
}

func TestContext_BuildExprEnv_WithVariables(t *testing.T) {
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test_workflow"}}
	trigger := &TriggerData{
		Type:   "http",
		Params: map[string]any{"id": 123},
	}
	variables := map[string]string{
		"api_key":     "secret-123",
		"db_host":     "localhost",
		"max_retries": "3",
	}
	ctx := NewContext(context.Background(), wf, trigger, "req-123", &testLogger{}, variables)

	env := ctx.BuildExprEnv()

	// Check vars is present and correct
	vars, ok := env["vars"].(map[string]string)
	if !ok {
		t.Fatal("vars is not a map[string]string")
	}
	if vars["api_key"] != "secret-123" {
		t.Errorf("vars.api_key = %v, want secret-123", vars["api_key"])
	}
	if vars["db_host"] != "localhost" {
		t.Errorf("vars.db_host = %v, want localhost", vars["db_host"])
	}
	if vars["max_retries"] != "3" {
		t.Errorf("vars.max_retries = %v, want 3", vars["max_retries"])
	}
}

func TestContext_BuildExprEnv_NilVariables(t *testing.T) {
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test_workflow"}}
	trigger := &TriggerData{
		Type: "http",
	}
	ctx := NewContext(context.Background(), wf, trigger, "req-123", &testLogger{}, nil)

	env := ctx.BuildExprEnv()

	// vars should be an empty map, not nil
	vars, ok := env["vars"].(map[string]string)
	if !ok {
		t.Fatal("vars is not a map[string]string")
	}
	if len(vars) != 0 {
		t.Errorf("expected empty vars map, got %v", vars)
	}
}

func TestContext_BuildExprEnv_VarsInExpr(t *testing.T) {
	t.Run("access vars.VARIABLE_NAME in expr", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{Type: "http"}
		variables := map[string]string{
			"API_KEY": "secret-abc-123",
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, variables)

		env := ctx.BuildExprEnv()

		// Evaluate expr: vars.API_KEY == "secret-abc-123"
		program, err := expr.Compile(`vars.API_KEY == "secret-abc-123"`, expr.AsBool())
		if err != nil {
			t.Fatalf("failed to compile expr: %v", err)
		}
		result, err := expr.Run(program, env)
		if err != nil {
			t.Fatalf("failed to evaluate expr: %v", err)
		}
		if result != true {
			t.Errorf("vars.API_KEY comparison failed, got %v", result)
		}

		// Evaluate expr to get actual value: vars.API_KEY
		valueProgram, err := expr.Compile(`vars.API_KEY`)
		if err != nil {
			t.Fatalf("failed to compile value expr: %v", err)
		}
		value, err := expr.Run(valueProgram, env)
		if err != nil {
			t.Fatalf("failed to evaluate value expr: %v", err)
		}
		if value != "secret-abc-123" {
			t.Errorf("vars.API_KEY = %q, want %q", value, "secret-abc-123")
		}
	})

	t.Run("variable not present returns empty string", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{Type: "http"}
		variables := map[string]string{
			"EXISTING_VAR": "value",
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, variables)

		env := ctx.BuildExprEnv()

		// Accessing non-existent key in expr returns empty string (zero value for string)
		program, err := expr.Compile(`vars.NON_EXISTENT == ""`, expr.AsBool())
		if err != nil {
			t.Fatalf("failed to compile expr: %v", err)
		}
		result, err := expr.Run(program, env)
		if err != nil {
			t.Fatalf("failed to evaluate expr: %v", err)
		}
		if result != true {
			t.Errorf("non-existent var should return empty string, got %v", result)
		}
	})

	t.Run("multiple variables accessible in same expr", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{Type: "http"}
		variables := map[string]string{
			"DB_HOST":     "localhost",
			"DB_PORT":     "5432",
			"DB_USER":     "admin",
			"ENVIRONMENT": "production",
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, variables)

		env := ctx.BuildExprEnv()

		// Test accessing multiple variables in a single expression
		program, err := expr.Compile(
			`vars.DB_HOST == "localhost" && vars.DB_PORT == "5432" && vars.ENVIRONMENT == "production"`,
			expr.AsBool(),
		)
		if err != nil {
			t.Fatalf("failed to compile expr: %v", err)
		}
		result, err := expr.Run(program, env)
		if err != nil {
			t.Fatalf("failed to evaluate expr: %v", err)
		}
		if result != true {
			t.Errorf("multiple vars comparison failed, got %v", result)
		}

		// Test string concatenation with variables
		concatProgram, err := expr.Compile(`vars.DB_HOST + ":" + vars.DB_PORT`)
		if err != nil {
			t.Fatalf("failed to compile concat expr: %v", err)
		}
		concatResult, err := expr.Run(concatProgram, env)
		if err != nil {
			t.Fatalf("failed to evaluate concat expr: %v", err)
		}
		if concatResult != "localhost:5432" {
			t.Errorf("concat result = %q, want %q", concatResult, "localhost:5432")
		}
	})
}

func TestContext_BuildTemplateData(t *testing.T) {
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
	trigger := &TriggerData{Type: "http"}
	ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

	// BuildTemplateData should return the same as BuildExprEnv
	env := ctx.BuildExprEnv()
	data := ctx.BuildTemplateData()

	if len(env) != len(data) {
		t.Errorf("BuildTemplateData length %d != BuildExprEnv length %d", len(data), len(env))
	}
}

func TestStepResultToMap(t *testing.T) {
	t.Run("query result with rows", func(t *testing.T) {
		r := &StepResult{
			Name:       "fetch",
			Type:       "query",
			Success:    true,
			DurationMs: 42,
			Data:       []map[string]any{{"id": 1}},
			Count:      1,
		}
		m := stepResultToMap(r)

		if m["name"] != "fetch" {
			t.Errorf("name = %v, want fetch", m["name"])
		}
		if m["type"] != "query" {
			t.Errorf("type = %v, want query", m["type"])
		}
		if m["success"] != true {
			t.Errorf("success = %v, want true", m["success"])
		}
		if m["count"] != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
		// rows_affected should be 0 for SELECT queries
		if m["rows_affected"] != int64(0) {
			t.Errorf("rows_affected = %v, want 0", m["rows_affected"])
		}

		// Convenience shortcuts for single row
		row, ok := m["row"].(map[string]any)
		if !ok {
			t.Fatalf("row should be map[string]any, got %T", m["row"])
		}
		if row["id"] != 1 {
			t.Errorf("row.id = %v, want 1", row["id"])
		}
		if m["found"] != true {
			t.Errorf("found = %v, want true", m["found"])
		}
		if m["empty"] != false {
			t.Errorf("empty = %v, want false", m["empty"])
		}
		if m["one"] != true {
			t.Errorf("one = %v, want true", m["one"])
		}
		if m["many"] != false {
			t.Errorf("many = %v, want false", m["many"])
		}
	})

	t.Run("query result with multiple rows", func(t *testing.T) {
		r := &StepResult{
			Name:       "fetch_all",
			Type:       "query",
			Success:    true,
			DurationMs: 100,
			Data:       []map[string]any{{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}, {"id": 3, "name": "Charlie"}},
			Count:      3,
		}
		m := stepResultToMap(r)

		// Convenience shortcuts for multiple rows
		row, ok := m["row"].(map[string]any)
		if !ok {
			t.Fatalf("row should be map[string]any, got %T", m["row"])
		}
		if row["id"] != 1 {
			t.Errorf("row.id = %v, want 1 (first element)", row["id"])
		}
		if row["name"] != "Alice" {
			t.Errorf("row.name = %v, want Alice", row["name"])
		}
		if m["found"] != true {
			t.Errorf("found = %v, want true", m["found"])
		}
		if m["empty"] != false {
			t.Errorf("empty = %v, want false", m["empty"])
		}
		if m["one"] != false {
			t.Errorf("one = %v, want false", m["one"])
		}
		if m["many"] != true {
			t.Errorf("many = %v, want true", m["many"])
		}
	})

	t.Run("query result with rows_affected", func(t *testing.T) {
		// This tests INSERT/UPDATE/DELETE operations that return rows_affected
		r := &StepResult{
			Name:         "insert",
			Type:         "query",
			Success:      true,
			DurationMs:   5,
			Data:         nil,
			Count:        0,
			RowsAffected: 3,
		}
		m := stepResultToMap(r)

		if m["name"] != "insert" {
			t.Errorf("name = %v, want insert", m["name"])
		}
		if m["type"] != "query" {
			t.Errorf("type = %v, want query", m["type"])
		}
		if m["success"] != true {
			t.Errorf("success = %v, want true", m["success"])
		}
		// count should be 0 for INSERT (no rows returned)
		if m["count"] != 0 {
			t.Errorf("count = %v, want 0", m["count"])
		}
		// rows_affected should be 3 (3 rows inserted)
		if m["rows_affected"] != int64(3) {
			t.Errorf("rows_affected = %v, want 3", m["rows_affected"])
		}
		// data should be empty slice, not nil
		data, ok := m["data"].([]map[string]any)
		if !ok {
			t.Fatalf("data should be []map[string]any, got %T", m["data"])
		}
		if len(data) != 0 {
			t.Errorf("len(data) = %d, want 0", len(data))
		}

		// Convenience shortcuts for empty result
		if m["row"] != nil {
			t.Errorf("row = %v, want nil", m["row"])
		}
		if m["found"] != false {
			t.Errorf("found = %v, want false", m["found"])
		}
		if m["empty"] != true {
			t.Errorf("empty = %v, want true", m["empty"])
		}
		if m["one"] != false {
			t.Errorf("one = %v, want false", m["one"])
		}
		if m["many"] != false {
			t.Errorf("many = %v, want false", m["many"])
		}
	})

	t.Run("httpcall result without parsed data", func(t *testing.T) {
		r := &StepResult{
			Name:         "api_call",
			Type:         "httpcall",
			Success:      true,
			StatusCode:   200,
			Headers:      http.Header{"Content-Type": []string{"application/json"}},
			ResponseBody: `{"ok": true}`,
		}
		m := stepResultToMap(r)

		if m["status_code"] != 200 {
			t.Errorf("status_code = %v, want 200", m["status_code"])
		}
		if m["body"] != `{"ok": true}` {
			t.Errorf("body = %v, want {\"ok\": true}", m["body"])
		}
		// Data and count are always present for consistency with query steps
		data, ok := m["data"].([]map[string]any)
		if !ok {
			t.Errorf("data should be []map[string]any, got %T", m["data"])
		} else if len(data) != 0 {
			t.Errorf("data should be empty slice when Data is nil, got %v", data)
		}
		if m["count"] != 0 {
			t.Errorf("count should be 0 when Data is nil, got %v", m["count"])
		}

		// Convenience shortcuts for empty result
		if m["row"] != nil {
			t.Errorf("row = %v, want nil", m["row"])
		}
		if m["found"] != false {
			t.Errorf("found = %v, want false", m["found"])
		}
		if m["empty"] != true {
			t.Errorf("empty = %v, want true", m["empty"])
		}
		if m["one"] != false {
			t.Errorf("one = %v, want false", m["one"])
		}
		if m["many"] != false {
			t.Errorf("many = %v, want false", m["many"])
		}
	})

	t.Run("httpcall result with parsed JSON data", func(t *testing.T) {
		// This tests the case when parse: json is used and Data/Count are populated
		r := &StepResult{
			Name:         "api_call",
			Type:         "httpcall",
			Success:      true,
			StatusCode:   200,
			Headers:      http.Header{"Content-Type": []string{"application/json"}},
			ResponseBody: `[{"id": 1}, {"id": 2}]`,
			Data:         []map[string]any{{"id": 1}, {"id": 2}},
			Count:        2,
		}
		m := stepResultToMap(r)

		if m["status_code"] != 200 {
			t.Errorf("status_code = %v, want 200", m["status_code"])
		}
		if m["body"] != `[{"id": 1}, {"id": 2}]` {
			t.Errorf("body = %v, want [...]", m["body"])
		}
		// When Data is populated, data and count MUST be exposed
		data, ok := m["data"].([]map[string]any)
		if !ok {
			t.Fatalf("data should be []map[string]any, got %T", m["data"])
		}
		if len(data) != 2 {
			t.Errorf("len(data) = %d, want 2", len(data))
		}
		if m["count"] != 2 {
			t.Errorf("count = %v, want 2", m["count"])
		}

		// Convenience shortcuts for multiple rows
		row, ok := m["row"].(map[string]any)
		if !ok {
			t.Fatalf("row should be map[string]any, got %T", m["row"])
		}
		if row["id"] != 1 {
			t.Errorf("row.id = %v, want 1 (first element)", row["id"])
		}
		if m["found"] != true {
			t.Errorf("found = %v, want true", m["found"])
		}
		if m["empty"] != false {
			t.Errorf("empty = %v, want false", m["empty"])
		}
		if m["one"] != false {
			t.Errorf("one = %v, want false", m["one"])
		}
		if m["many"] != true {
			t.Errorf("many = %v, want true", m["many"])
		}
	})

	t.Run("block result", func(t *testing.T) {
		r := &StepResult{
			Name:         "process_items",
			Type:         "block",
			Success:      true,
			SuccessCount: 5,
			FailureCount: 1,
			SkippedCount: 0,
			Iterations: []*IterationResult{
				{Index: 0, Item: map[string]any{"id": 1}, Success: true},
				{Index: 1, Item: map[string]any{"id": 2}, Success: false, Error: testError{msg: "failed"}},
			},
		}
		m := stepResultToMap(r)

		if m["success_count"] != 5 {
			t.Errorf("success_count = %v, want 5", m["success_count"])
		}
		if m["failure_count"] != 1 {
			t.Errorf("failure_count = %v, want 1", m["failure_count"])
		}
		iterations := m["iterations"].([]map[string]any)
		if len(iterations) != 2 {
			t.Errorf("len(iterations) = %d, want 2", len(iterations))
		}
		if iterations[1]["error"] != "failed" {
			t.Errorf("iterations[1].error = %v, want failed", iterations[1]["error"])
		}
	})

	t.Run("result with error", func(t *testing.T) {
		r := &StepResult{
			Name:    "failed_step",
			Type:    "query",
			Success: false,
			Error:   testError{msg: "database error"},
		}
		m := stepResultToMap(r)

		if m["error"] != "database error" {
			t.Errorf("error = %v, want 'database error'", m["error"])
		}
	})
}

func TestHeaderToMap(t *testing.T) {
	t.Run("single value headers", func(t *testing.T) {
		h := http.Header{
			"Content-Type":  []string{"application/json"},
			"Authorization": []string{"Bearer token"},
		}
		m := headerToMap(h)

		if m["Content-Type"] != "application/json" {
			t.Errorf("Content-Type = %v, want application/json", m["Content-Type"])
		}
		if m["Authorization"] != "Bearer token" {
			t.Errorf("Authorization = %v, want Bearer token", m["Authorization"])
		}
	})

	t.Run("multi value headers", func(t *testing.T) {
		h := http.Header{
			"Accept": []string{"text/html", "application/json"},
		}
		m := headerToMap(h)

		accept, ok := m["Accept"].([]string)
		if !ok {
			t.Fatalf("Accept should be []string, got %T", m["Accept"])
		}
		if len(accept) != 2 {
			t.Errorf("len(Accept) = %d, want 2", len(accept))
		}
	})
}

func TestBlockContext(t *testing.T) {
	parentWf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "parent_workflow"}}
	trigger := &TriggerData{
		Type:   "http",
		Params: map[string]any{"filter": "active"},
	}
	parentCtx := NewContext(context.Background(), parentWf, trigger, "req-1", &testLogger{}, nil)

	// Add parent step result
	parentCtx.SetStepResult("fetch_items", &StepResult{
		Name:    "fetch_items",
		Type:    "query",
		Success: true,
		Data:    []map[string]any{{"id": 1}, {"id": 2}},
		Count:   2,
	})

	// Create block context
	item := map[string]any{"id": 1, "name": "Item 1"}
	blockCtx := NewBlockContext(parentCtx, "process_each", item, 0, 2)

	if blockCtx.Parent != parentCtx {
		t.Error("Parent not set correctly")
	}
	if blockCtx.BlockName != "process_each" {
		t.Errorf("BlockName = %q, want %q", blockCtx.BlockName, "process_each")
	}
	if blockCtx.CurrentIndex != 0 {
		t.Errorf("CurrentIndex = %d, want 0", blockCtx.CurrentIndex)
	}
	if blockCtx.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", blockCtx.TotalCount)
	}
}

func TestBlockContext_SetGetStepResult(t *testing.T) {
	parentWf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "parent"}}
	trigger := &TriggerData{Type: "http"}
	parentCtx := NewContext(context.Background(), parentWf, trigger, "req-1", &testLogger{}, nil)

	blockCtx := NewBlockContext(parentCtx, "block1", nil, 0, 1)

	result := &StepResult{
		Name:    "block_step",
		Type:    "query",
		Success: true,
	}

	blockCtx.SetStepResult("block_step", result)

	got := blockCtx.GetStepResult("block_step")
	if got != result {
		t.Error("GetStepResult did not return the set result")
	}

	// Getting a parent step from block context should return nil
	// (GetStepResult only looks in current block)
	notFound := blockCtx.GetStepResult("nonexistent")
	if notFound != nil {
		t.Error("GetStepResult should return nil for nonexistent step")
	}
}

func TestBlockContext_BuildExprEnv(t *testing.T) {
	parentWf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "parent_workflow"}}
	trigger := &TriggerData{
		Type:   "http",
		Params: map[string]any{"filter": "active"},
	}
	parentCtx := NewContext(context.Background(), parentWf, trigger, "req-1", &testLogger{}, nil)
	parentCtx.SetStepResult("fetch", &StepResult{
		Name:    "fetch",
		Type:    "query",
		Success: true,
		Count:   5,
	})

	item := map[string]any{"id": 42, "name": "Test Item"}
	blockCtx := NewBlockContext(parentCtx, "process", item, 1, 3)

	// Add a block step result
	blockCtx.SetStepResult("inner_query", &StepResult{
		Name:    "inner_query",
		Type:    "query",
		Success: true,
		Count:   1,
	})

	env := blockCtx.BuildExprEnv("item")

	// Check current item is set
	envItem, ok := env["item"].(map[string]any)
	if !ok {
		t.Error("item not set in env or wrong type")
	} else {
		if envItem["id"] != 42 {
			t.Errorf("item.id = %v, want 42", envItem["id"])
		}
	}

	// Check index and count
	if env["_index"] != 1 {
		t.Errorf("_index = %v, want 1", env["_index"])
	}
	if env["_count"] != 3 {
		t.Errorf("_count = %v, want 3", env["_count"])
	}

	// Check block steps
	steps := env["steps"].(map[string]any)
	if _, ok := steps["inner_query"]; !ok {
		t.Error("inner_query not found in steps")
	}

	// Check parent access
	parent := env["parent"].(map[string]any)
	parentSteps := parent["steps"].(map[string]any)
	if _, ok := parentSteps["fetch"]; !ok {
		t.Error("fetch not found in parent.steps")
	}

	// Check trigger forwarded
	if env["trigger"] == nil {
		t.Error("trigger should be forwarded from parent")
	}

	// Check workflow forwarded
	if env["workflow"] == nil {
		t.Error("workflow should be forwarded from parent")
	}
}

func TestBlockContext_BuildTemplateData(t *testing.T) {
	parentWf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "parent"}}
	trigger := &TriggerData{Type: "http"}
	parentCtx := NewContext(context.Background(), parentWf, trigger, "req-1", &testLogger{}, nil)

	blockCtx := NewBlockContext(parentCtx, "block", nil, 0, 1)

	env := blockCtx.BuildExprEnv("item")
	data := blockCtx.BuildTemplateData("item")

	if len(env) != len(data) {
		t.Errorf("BuildTemplateData length %d != BuildExprEnv length %d", len(data), len(env))
	}
}

// testLogger implements Logger for testing
type testLogger struct {
	debugCalls []logCall
	infoCalls  []logCall
	warnCalls  []logCall
	errorCalls []logCall
}

type logCall struct {
	msg    string
	fields map[string]any
}

func (l *testLogger) Debug(msg string, fields map[string]any) {
	l.debugCalls = append(l.debugCalls, logCall{msg, fields})
}

func (l *testLogger) Info(msg string, fields map[string]any) {
	l.infoCalls = append(l.infoCalls, logCall{msg, fields})
}

func (l *testLogger) Warn(msg string, fields map[string]any) {
	l.warnCalls = append(l.warnCalls, logCall{msg, fields})
}

func (l *testLogger) Error(msg string, fields map[string]any) {
	l.errorCalls = append(l.errorCalls, logCall{msg, fields})
}

// testError implements error for testing
type testError struct {
	msg string
}

func (e testError) Error() string {
	return e.msg
}

func TestContext_BuildExprEnv_ContainsExprFuncs(t *testing.T) {
	// exprFuncs like isValidPublicID should be available in the expression environment
	wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
	trigger := &TriggerData{Type: "http"}
	ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

	env := ctx.BuildExprEnv()

	// Check that isValidPublicID function is present
	fn, ok := env["isValidPublicID"]
	if !ok {
		t.Error("isValidPublicID function should be in BuildExprEnv result")
	}
	if fn == nil {
		t.Error("isValidPublicID function should not be nil")
	}
}

func TestContext_BuildExprEnv_CookieAccess(t *testing.T) {
	t.Run("access single cookie by name", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type:    "http",
			Cookies: map[string]string{"session_id": "abc123"},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		triggerEnv, ok := env["trigger"].(map[string]any)
		if !ok {
			t.Fatal("expected trigger to be map[string]any")
		}
		cookies, ok := triggerEnv["cookies"].(map[string]string)
		if !ok {
			t.Fatal("trigger.cookies not found or wrong type")
		}
		if cookies["session_id"] != "abc123" {
			t.Errorf("trigger.cookies.session_id = %q, want %q", cookies["session_id"], "abc123")
		}
	})

	t.Run("cookie not present returns empty string", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type:    "http",
			Cookies: map[string]string{"existing": "value"},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		triggerEnv, ok := env["trigger"].(map[string]any)
		if !ok {
			t.Fatal("expected trigger to be map[string]any")
		}
		cookies, ok := triggerEnv["cookies"].(map[string]string)
		if !ok {
			t.Fatal("expected cookies to be map[string]string")
		}
		if cookies["nonexistent"] != "" {
			t.Errorf("trigger.cookies.nonexistent = %q, want empty string", cookies["nonexistent"])
		}
	})

	t.Run("multiple cookies", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type: "http",
			Cookies: map[string]string{
				"session":  "sess_abc123",
				"user_id":  "user_456",
				"theme":    "dark",
				"language": "en-US",
			},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		triggerEnv, ok := env["trigger"].(map[string]any)
		if !ok {
			t.Fatal("expected trigger to be map[string]any")
		}
		cookies, ok := triggerEnv["cookies"].(map[string]string)
		if !ok {
			t.Fatal("expected cookies to be map[string]string")
		}

		if cookies["session"] != "sess_abc123" {
			t.Errorf("trigger.cookies.session = %q, want %q", cookies["session"], "sess_abc123")
		}
		if cookies["user_id"] != "user_456" {
			t.Errorf("trigger.cookies.user_id = %q, want %q", cookies["user_id"], "user_456")
		}
		if cookies["theme"] != "dark" {
			t.Errorf("trigger.cookies.theme = %q, want %q", cookies["theme"], "dark")
		}
		if cookies["language"] != "en-US" {
			t.Errorf("trigger.cookies.language = %q, want %q", cookies["language"], "en-US")
		}
	})

	t.Run("nil cookies map", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type:    "http",
			Cookies: nil,
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		triggerEnv, ok := env["trigger"].(map[string]any)
		if !ok {
			t.Fatal("expected trigger to be map[string]any")
		}
		// When Cookies is nil, accessing cookies via expr still works
		// (nil map allows reads, returning zero value)
		cookies, ok := triggerEnv["cookies"].(map[string]string)
		if !ok {
			t.Fatal("expected cookies to be map[string]string")
		}
		if cookies["any_cookie"] != "" {
			t.Errorf("accessing cookie in nil map should return empty string, got %q", cookies["any_cookie"])
		}
	})

	t.Run("empty cookies map", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type:    "http",
			Cookies: map[string]string{},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		triggerEnv, ok := env["trigger"].(map[string]any)
		if !ok {
			t.Fatal("expected trigger to be map[string]any")
		}
		cookies, ok := triggerEnv["cookies"].(map[string]string)
		if !ok {
			t.Fatal("trigger.cookies should be map[string]string")
		}
		if len(cookies) != 0 {
			t.Errorf("len(trigger.cookies) = %d, want 0", len(cookies))
		}
	})
}

func TestContext_BuildExprEnv_CookiesInExpr(t *testing.T) {
	t.Run("access trigger.cookies.cookiename in expr", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type:    "http",
			Cookies: map[string]string{"session": "sess_abc123"},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		// Evaluate expr: trigger.cookies.session == "sess_abc123"
		program, err := expr.Compile(`trigger.cookies.session == "sess_abc123"`, expr.AsBool())
		if err != nil {
			t.Fatalf("failed to compile expr: %v", err)
		}
		result, err := expr.Run(program, env)
		if err != nil {
			t.Fatalf("failed to evaluate expr: %v", err)
		}
		if result != true {
			t.Errorf("trigger.cookies.session comparison failed, got %v", result)
		}

		// Evaluate expr to get actual value: trigger.cookies.session
		valueProgram, err := expr.Compile(`trigger.cookies.session`)
		if err != nil {
			t.Fatalf("failed to compile value expr: %v", err)
		}
		value, err := expr.Run(valueProgram, env)
		if err != nil {
			t.Fatalf("failed to evaluate value expr: %v", err)
		}
		if value != "sess_abc123" {
			t.Errorf("trigger.cookies.session = %q, want %q", value, "sess_abc123")
		}
	})

	t.Run("cookie not present returns empty string in expr", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type:    "http",
			Cookies: map[string]string{"existing": "value"},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		// Accessing non-existent cookie in expr returns empty string (zero value for string)
		program, err := expr.Compile(`trigger.cookies.nonexistent == ""`, expr.AsBool())
		if err != nil {
			t.Fatalf("failed to compile expr: %v", err)
		}
		result, err := expr.Run(program, env)
		if err != nil {
			t.Fatalf("failed to evaluate expr: %v", err)
		}
		if result != true {
			t.Errorf("non-existent cookie should return empty string, got %v", result)
		}
	})

	t.Run("multiple cookies accessible in same expr", func(t *testing.T) {
		wf := &CompiledWorkflow{Config: &WorkflowConfig{Name: "test"}}
		trigger := &TriggerData{
			Type: "http",
			Cookies: map[string]string{
				"session":  "sess_abc123",
				"user_id":  "user_456",
				"theme":    "dark",
				"language": "en-US",
			},
		}
		ctx := NewContext(context.Background(), wf, trigger, "req-1", &testLogger{}, nil)

		env := ctx.BuildExprEnv()

		// Test accessing multiple cookies in a single expression
		program, err := expr.Compile(
			`trigger.cookies.session == "sess_abc123" && trigger.cookies.theme == "dark"`,
			expr.AsBool(),
		)
		if err != nil {
			t.Fatalf("failed to compile expr: %v", err)
		}
		result, err := expr.Run(program, env)
		if err != nil {
			t.Fatalf("failed to evaluate expr: %v", err)
		}
		if result != true {
			t.Errorf("multiple cookies comparison failed, got %v", result)
		}

		// Test string concatenation with cookies
		concatProgram, err := expr.Compile(`trigger.cookies.user_id + "@" + trigger.cookies.language`)
		if err != nil {
			t.Fatalf("failed to compile concat expr: %v", err)
		}
		concatResult, err := expr.Run(concatProgram, env)
		if err != nil {
			t.Fatalf("failed to evaluate concat expr: %v", err)
		}
		if concatResult != "user_456@en-US" {
			t.Errorf("concat result = %q, want %q", concatResult, "user_456@en-US")
		}
	})
}
