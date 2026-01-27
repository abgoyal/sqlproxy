package workflow

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"
	"time"

	"sql-proxy/internal/workflow/step"
)

// mockDBManager implements step.DBManager for testing.
type mockDBManager struct {
	queryFunc func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error)
}

func (m *mockDBManager) ExecuteQuery(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, database, sql, params, opts)
	}
	return &step.QueryResult{Rows: []map[string]any{}}, nil
}

// mockHTTPClient implements step.HTTPClient for testing.
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.doFunc != nil {
		return m.doFunc(req)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func TestNewExecutor(t *testing.T) {
	db := &mockDBManager{}
	httpClient := &mockHTTPClient{}
	logger := &testLogger{}

	exec := NewExecutor(db, httpClient, nil, logger)

	if exec.dbManager != db {
		t.Error("dbManager not set")
	}
	if exec.httpClient != httpClient {
		t.Error("httpClient not set")
	}
	if exec.logger != logger {
		t.Error("logger not set")
	}
}

func TestExecutor_Execute_SimpleQuery(t *testing.T) {
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{
				{"id": 1, "name": "Alice"},
				{"id": 2, "name": "Bob"},
			}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Create a simple workflow with one query step
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "fetch_users", Type: "query", Database: "testdb"},
				SQLTmpl: sqlTmpl,
			},
		},
	}

	trigger := &TriggerData{
		Type:   "http",
		Params: map[string]any{},
	}

	result := exec.Execute(context.Background(), wf, trigger, "req-123", nil, nil)

	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.Error != nil {
		t.Errorf("Error = %v, want nil", result.Error)
	}
	if len(result.Steps) != 1 {
		t.Errorf("len(Steps) = %d, want 1", len(result.Steps))
	}
	stepResult := result.Steps["fetch_users"]
	if stepResult == nil {
		t.Fatal("fetch_users step not found")
	}
	if stepResult.Count != 2 {
		t.Errorf("step.Count = %d, want 2", stepResult.Count)
	}
}

func TestExecutor_Execute_DisabledStep(t *testing.T) {
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			t.Error("Query should not be executed for disabled step")
			return nil, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "disabled_step", Type: "query", Database: "testdb", Disabled: true},
				SQLTmpl: sqlTmpl,
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if len(result.Steps) != 0 {
		t.Errorf("len(Steps) = %d, want 0 (disabled step should be skipped)", len(result.Steps))
	}
}

func TestExecutor_Execute_ConditionalStep(t *testing.T) {
	queryCalled := false
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			queryCalled = true
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Compile a false condition
	condProg, _ := compileCondition("false")
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users"))

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:    &StepConfig{Name: "conditional_step", Type: "query", Database: "testdb", Condition: "false"},
				Condition: condProg,
				SQLTmpl:   sqlTmpl,
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if queryCalled {
		t.Error("Query should not be called when condition is false")
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
}

func TestExecutor_Execute_StepFailure_Abort(t *testing.T) {
	stepOrder := []string{}
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			if strings.Contains(sql, "fail") {
				return nil, errors.New("query failed")
			}
			stepOrder = append(stepOrder, sql)
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "step1", Type: "query", Database: "db", OnError: "abort"},
				SQLTmpl: template.Must(template.New("sql").Parse("step1")),
			},
			{
				Config:  &StepConfig{Name: "step2_fail", Type: "query", Database: "db", OnError: "abort"},
				SQLTmpl: template.Must(template.New("sql").Parse("fail")),
			},
			{
				Config:  &StepConfig{Name: "step3", Type: "query", Database: "db"},
				SQLTmpl: template.Must(template.New("sql").Parse("step3")),
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if result.Success {
		t.Error("Success = true, want false")
	}
	if result.Error == nil {
		t.Error("Error should be set")
	}
	if len(stepOrder) != 1 {
		t.Errorf("step order = %v, want [step1] only", stepOrder)
	}
}

func TestExecutor_Execute_StepFailure_Continue(t *testing.T) {
	stepOrder := []string{}
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			stepOrder = append(stepOrder, sql)
			if strings.Contains(sql, "fail") {
				return nil, errors.New("query failed")
			}
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "step1", Type: "query", Database: "db"},
				SQLTmpl: template.Must(template.New("sql").Parse("step1")),
			},
			{
				Config:  &StepConfig{Name: "step2_fail", Type: "query", Database: "db", OnError: "continue"},
				SQLTmpl: template.Must(template.New("sql").Parse("fail")),
			},
			{
				Config:  &StepConfig{Name: "step3", Type: "query", Database: "db"},
				SQLTmpl: template.Must(template.New("sql").Parse("step3")),
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if !result.Success {
		t.Error("Success = false, want true (on_error: continue)")
	}
	if len(stepOrder) != 3 {
		t.Errorf("step order = %v, want [step1, fail, step3]", stepOrder)
	}
}

func TestExecutor_Execute_ResponseStep(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	tmpl := template.Must(template.New("response").Parse(`{"success": true}`))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:       &StepConfig{Name: "send_response", Type: "response", StatusCode: 200},
				TemplateTmpl: tmpl,
			},
		},
	}

	recorder := httptest.NewRecorder()
	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", recorder, nil)

	if !result.ResponseSent {
		t.Error("ResponseSent = false, want true")
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}

	resp := recorder.Result()
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestExecutor_Execute_HTTPCallStep(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"data": "test"}`)),
				Header:     http.Header{"X-Custom": []string{"value"}},
			}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, client, nil, logger)

	urlTmpl := template.Must(template.New("url").Parse("https://api.example.com/data"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "call_api", Type: "httpcall", HTTPMethod: "GET"},
				URLTmpl: urlTmpl,
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	stepResult := result.Steps["call_api"]
	if stepResult == nil {
		t.Fatal("call_api step not found")
	}
	if stepResult.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", stepResult.StatusCode)
	}
}

func TestExecutor_Execute_ContextCancellation(t *testing.T) {
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	sqlTmpl := template.Must(template.New("sql").Parse("SELECT 1"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{Config: &StepConfig{Name: "step1", Type: "query", Database: "db"}, SQLTmpl: sqlTmpl},
			{Config: &StepConfig{Name: "step2", Type: "query", Database: "db"}, SQLTmpl: sqlTmpl},
		},
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(ctx, wf, trigger, "req-1", nil, nil)

	if result.Success {
		t.Error("Success = true, want false for cancelled context")
	}
	if result.Error != context.Canceled {
		t.Errorf("Error = %v, want context.Canceled", result.Error)
	}
}

func TestExecutor_Execute_WorkflowTimeout(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create workflow with very short timeout
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{
			Name:       "test",
			TimeoutSec: 1, // 1 second timeout
		},
		Steps: []*CompiledStep{}, // No steps
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Should still succeed since there are no steps
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
}

func TestExecutor_Execute_HTTPTriggerWithoutResponse(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Workflow with no response step
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps:  []*CompiledStep{},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Should succeed but warn about no response
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.ResponseSent {
		t.Error("ResponseSent = true, want false")
	}

	// Check that warning was logged
	found := false
	for _, call := range logger.warnCalls {
		if call.msg == "workflow_no_response" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected workflow_no_response warning")
	}
}

func TestExecutor_Execute_UnknownStepType(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config: &StepConfig{Name: "unknown", Type: "custom"}, // Unknown type
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Should fail due to unknown step type
	if result.Success {
		t.Error("Success = true, want false")
	}
	stepResult := result.Steps["unknown"]
	if stepResult == nil || stepResult.Error == nil {
		t.Error("Expected error for unknown step type")
	}
}

func TestExecutor_Execute_BlockStep(t *testing.T) {
	queryCount := 0
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			queryCount++
			return &step.QueryResult{Rows: []map[string]any{{"result": queryCount}}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Create iterate expression that returns array of items
	overExpr, _ := compileExpression("steps.fetch.data")
	innerSqlTmpl := template.Must(template.New("sql").Parse("UPDATE items SET processed = 1"))

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "fetch", Type: "query", Database: "db"},
				SQLTmpl: template.Must(template.New("sql").Parse("SELECT id FROM items")),
			},
			{
				Config: &StepConfig{
					Name:  "process_items",
					Steps: []StepConfig{{Name: "update_item", Type: "query", Database: "db"}},
				},
				Iterate: &CompiledIterate{
					Config:   &IterateConfig{Over: "steps.fetch.data", As: "item", OnError: "continue"},
					OverExpr: overExpr,
				},
				BlockSteps: []*CompiledStep{
					{
						Config:  &StepConfig{Name: "update_item", Type: "query", Database: "db"},
						SQLTmpl: innerSqlTmpl,
					},
				},
			},
		},
	}

	trigger := &TriggerData{Type: "http"}

	// The first query (fetch) returns 2 items
	db.queryFunc = func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
		queryCount++
		if strings.Contains(sql, "SELECT") {
			return &step.QueryResult{Rows: []map[string]any{{"id": 1}, {"id": 2}}}, nil
		}
		return &step.QueryResult{Rows: []map[string]any{}}, nil
	}

	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if !result.Success {
		t.Errorf("Success = false, error = %v", result.Error)
	}
	// 1 fetch + 2 updates
	if queryCount != 3 {
		t.Errorf("queryCount = %d, want 3", queryCount)
	}

	blockResult := result.Steps["process_items"]
	if blockResult == nil {
		t.Fatal("process_items step not found")
	}
	if blockResult.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", blockResult.SuccessCount)
	}
}

func TestExecutor_Execute_BlockStep_IterationError_Abort(t *testing.T) {
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			if strings.Contains(sql, "SELECT") {
				return &step.QueryResult{Rows: []map[string]any{{"id": 1}, {"id": 2}, {"id": 3}}}, nil
			}
			if strings.Contains(sql, "fail") {
				return nil, errors.New("update failed")
			}
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	overExpr, _ := compileExpression("steps.fetch.data")
	innerSqlTmpl := template.Must(template.New("sql").Parse("fail"))

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "fetch", Type: "query", Database: "db"},
				SQLTmpl: template.Must(template.New("sql").Parse("SELECT id FROM items")),
			},
			{
				Config: &StepConfig{
					Name:  "process",
					Steps: []StepConfig{{Name: "fail_step", Type: "query", Database: "db"}},
				},
				Iterate: &CompiledIterate{
					Config:   &IterateConfig{Over: "steps.fetch.data", As: "item", OnError: "abort"},
					OverExpr: overExpr,
				},
				BlockSteps: []*CompiledStep{
					{
						Config:  &StepConfig{Name: "fail_step", Type: "query", Database: "db"},
						SQLTmpl: innerSqlTmpl,
					},
				},
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Block should fail
	blockResult := result.Steps["process"]
	if blockResult == nil {
		t.Fatal("process step not found")
	}
	if blockResult.Success {
		t.Error("Block should fail")
	}
	// Should abort after first failure
	if blockResult.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", blockResult.FailureCount)
	}
}

func TestExecutor_Execute_BlockStep_WithoutIteration(t *testing.T) {
	queryCount := 0
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			queryCount++
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config: &StepConfig{
					Name:  "no_iterate",
					Steps: []StepConfig{{Name: "inner", Type: "query", Database: "db"}},
				},
				BlockSteps: []*CompiledStep{
					{
						Config:  &StepConfig{Name: "inner", Type: "query", Database: "db"},
						SQLTmpl: template.Must(template.New("sql").Parse("SELECT 1")),
					},
				},
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if !result.Success {
		t.Errorf("Success = false, error = %v", result.Error)
	}
	// Should execute once without iteration
	if queryCount != 1 {
		t.Errorf("queryCount = %d, want 1", queryCount)
	}
}

func TestExecutor_Execute_StepNames_Auto(t *testing.T) {
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Type: "query", Database: "db"}, // No name
				SQLTmpl: template.Must(template.New("sql").Parse("SELECT 1")),
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	// Auto-generated name should be step_0
	if _, ok := result.Steps["step_0"]; !ok {
		t.Errorf("Expected step_0 in results, got %v", result.Steps)
	}
}

func TestExecutor_Execute_LoggingCalls(t *testing.T) {
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "step1", Type: "query", Database: "db"},
				SQLTmpl: template.Must(template.New("sql").Parse("SELECT 1")),
			},
		},
	}

	trigger := &TriggerData{Type: "http"}
	exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Check workflow_started was logged
	foundStarted := false
	for _, call := range logger.infoCalls {
		if call.msg == "workflow_started" {
			foundStarted = true
			if call.fields["workflow"] != "test_workflow" {
				t.Errorf("workflow_started workflow field = %v", call.fields["workflow"])
			}
			break
		}
	}
	if !foundStarted {
		t.Error("Expected workflow_started log")
	}

	// Check workflow_completed was logged
	foundCompleted := false
	for _, call := range logger.infoCalls {
		if call.msg == "workflow_completed" {
			foundCompleted = true
			break
		}
	}
	if !foundCompleted {
		t.Error("Expected workflow_completed log")
	}
}

// mockStepCache implements StepCache for testing.
type mockStepCache struct {
	data map[string][]map[string]any
}

func newMockStepCache() *mockStepCache {
	return &mockStepCache{data: make(map[string][]map[string]any)}
}

func (m *mockStepCache) Get(workflow, key string) ([]map[string]any, bool) {
	fullKey := workflow + ":" + key
	data, ok := m.data[fullKey]
	return data, ok
}

func (m *mockStepCache) Set(workflow, key string, data []map[string]any, ttl time.Duration) bool {
	fullKey := workflow + ":" + key
	m.data[fullKey] = data
	return true
}

func TestExecutor_StepCache_Hit(t *testing.T) {
	// Create a mock cache with pre-populated data
	cache := newMockStepCache()
	cachedData := []map[string]any{{"id": 1, "cached": true}}
	cache.Set("test_workflow", "user:42", cachedData, 0)

	queryCount := 0
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			queryCount++
			return &step.QueryResult{Rows: []map[string]any{{"id": 1, "cached": false}}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, cache, logger)

	// Create workflow with a cached query step
	cacheKeyTmpl := template.Must(template.New("cache_key").Parse("user:{{.trigger.params.id}}"))
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users WHERE id = {{.trigger.params.id}}"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:       &StepConfig{Name: "fetch_user", Type: "query", Database: "testdb"},
				SQLTmpl:      sqlTmpl,
				CacheKeyTmpl: cacheKeyTmpl,
			},
		},
	}

	trigger := &TriggerData{Type: "http", Params: map[string]any{"id": 42}}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Should not have executed the query (cache hit)
	if queryCount != 0 {
		t.Errorf("Expected 0 queries (cache hit), got %d", queryCount)
	}

	// Should have result with cached data
	stepResult := result.Steps["fetch_user"]
	if stepResult == nil {
		t.Fatal("Expected fetch_user step result")
	}
	if !stepResult.CacheHit {
		t.Error("Expected CacheHit to be true")
	}
	if len(stepResult.Data) != 1 || stepResult.Data[0]["cached"] != true {
		t.Errorf("Expected cached data, got %v", stepResult.Data)
	}
}

func TestExecutor_StepCache_Miss(t *testing.T) {
	cache := newMockStepCache()

	queryCount := 0
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			queryCount++
			return &step.QueryResult{Rows: []map[string]any{{"id": 1, "from_db": true}}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, cache, logger)

	cacheKeyTmpl := template.Must(template.New("cache_key").Parse("user:{{.trigger.params.id}}"))
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users WHERE id = {{.trigger.params.id}}"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:       &StepConfig{Name: "fetch_user", Type: "query", Database: "testdb", Cache: &StepCacheConfig{Key: "user:{{.trigger.params.id}}", TTLSec: 300}},
				SQLTmpl:      sqlTmpl,
				CacheKeyTmpl: cacheKeyTmpl,
			},
		},
	}

	trigger := &TriggerData{Type: "http", Params: map[string]any{"id": 99}}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Should have executed the query (cache miss)
	if queryCount != 1 {
		t.Errorf("Expected 1 query (cache miss), got %d", queryCount)
	}

	// Should have result from database
	stepResult := result.Steps["fetch_user"]
	if stepResult == nil {
		t.Fatal("Expected fetch_user step result")
	}
	if stepResult.CacheHit {
		t.Error("Expected CacheHit to be false")
	}
	if len(stepResult.Data) != 1 || stepResult.Data[0]["from_db"] != true {
		t.Errorf("Expected db data, got %v", stepResult.Data)
	}

	// Should have cached the result
	cachedData, hit := cache.Get("test_workflow", "user:99")
	if !hit {
		t.Error("Expected result to be cached")
	}
	if len(cachedData) != 1 || cachedData[0]["from_db"] != true {
		t.Errorf("Cached data mismatch: %v", cachedData)
	}
}

func TestExecutor_StepCache_NilCache(t *testing.T) {
	queryCount := 0
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			queryCount++
			return &step.QueryResult{Rows: []map[string]any{{"id": 1}}}, nil
		},
	}
	logger := &testLogger{}
	// Pass nil cache
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	cacheKeyTmpl := template.Must(template.New("cache_key").Parse("user:{{.trigger.params.id}}"))
	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM users WHERE id = {{.trigger.params.id}}"))
	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Steps: []*CompiledStep{
			{
				Config:       &StepConfig{Name: "fetch_user", Type: "query", Database: "testdb"},
				SQLTmpl:      sqlTmpl,
				CacheKeyTmpl: cacheKeyTmpl,
			},
		},
	}

	trigger := &TriggerData{Type: "http", Params: map[string]any{"id": 42}}
	exec.Execute(context.Background(), wf, trigger, "req-1", nil, nil)

	// Should execute query even with cache key template (cache is nil)
	if queryCount != 1 {
		t.Errorf("Expected 1 query with nil cache, got %d", queryCount)
	}
}

// TestExecutor_ConditionalResponse_NegatedAlias tests that negated condition aliases work correctly
func TestExecutor_ConditionalResponse_NegatedAlias(t *testing.T) {
	// DB returns no rows to simulate "not found" case
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			// Return empty result
			return &step.QueryResult{Rows: []map[string]any{}}, nil
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Compile conditions
	foundCond, err := compileCondition("steps.fetch.count > 0")
	if err != nil {
		t.Fatalf("failed to compile found condition: %v", err)
	}
	// Negated condition: !(steps.fetch.count > 0)
	notFoundCond, err := compileCondition("!(steps.fetch.count > 0)")
	if err != nil {
		t.Fatalf("failed to compile not found condition: %v", err)
	}

	sqlTmpl := template.Must(template.New("sql").Parse("SELECT * FROM items WHERE id = @id"))
	responseTmpl := template.Must(template.New("resp").Funcs(TemplateFuncs).Parse(`{"item": "found"}`))
	notFoundTmpl := template.Must(template.New("notfound").Funcs(TemplateFuncs).Parse(`{"error": "not found"}`))

	wf := &CompiledWorkflow{
		Config: &WorkflowConfig{Name: "test_workflow"},
		Conditions: map[string]*CompiledCondition{
			"found": {Source: "steps.fetch.count > 0"},
		},
		Steps: []*CompiledStep{
			{
				Config:  &StepConfig{Name: "fetch", Type: "query", Database: "testdb"},
				SQLTmpl: sqlTmpl,
			},
			{
				Config:       &StepConfig{Name: "success_response", Type: "response", Template: `{"item": "found"}`, Condition: "found"},
				Condition:    foundCond,
				TemplateTmpl: responseTmpl,
			},
			{
				Config:       &StepConfig{Name: "not_found_response", Type: "response", Template: `{"error": "not found"}`, StatusCode: 404, Condition: "!found"},
				Condition:    notFoundCond,
				TemplateTmpl: notFoundTmpl,
			},
		},
	}

	// Create a recorder to capture response
	recorder := httptest.NewRecorder()

	trigger := &TriggerData{Type: "http", Params: map[string]any{"id": 999}}
	result := exec.Execute(context.Background(), wf, trigger, "req-1", recorder, nil)

	// Check that the "not found" response was sent
	if !result.ResponseSent {
		t.Error("Expected ResponseSent to be true")
	}

	// Response should be from the "not found" step
	body := recorder.Body.String()
	if !strings.Contains(body, "not found") {
		t.Errorf("Expected 'not found' in response body, got: %s", body)
	}

	// Check fetch step result
	fetchResult := result.Steps["fetch"]
	if fetchResult == nil {
		t.Fatal("Expected fetch step result")
	}
	if fetchResult.Count != 0 {
		t.Errorf("Expected Count=0 for empty result, got %d", fetchResult.Count)
	}
}

// TestExecutor_ConditionalResponse_FromConfig tests conditional responses compiled from config (like E2E).
func TestExecutor_ConditionalResponse_FromConfig(t *testing.T) {
	// Create workflow config similar to what YAML parsing produces
	wfConfig := &WorkflowConfig{
		Name: "test_conditional",
		Conditions: map[string]string{
			"found": "steps.fetch.count > 0",
		},
		Steps: []StepConfig{
			{
				Name:     "fetch",
				Type:     "query",
				Database: "testdb",
				SQL:      "SELECT * FROM items WHERE id = @id",
			},
			{
				Name:      "success_response",
				Type:      "response",
				Template:  `{"item": "found"}`,
				Condition: "found",
			},
			{
				Name:       "not_found_response",
				Type:       "response",
				StatusCode: 404,
				Template:   `{"error": "not found"}`,
				Condition:  "!found",
			},
		},
	}

	// Compile the workflow
	compiled, err := Compile(wfConfig)
	if err != nil {
		t.Fatalf("failed to compile workflow: %v", err)
	}

	// Verify conditions are compiled
	if len(compiled.Conditions) != 1 {
		t.Fatalf("expected 1 compiled condition, got %d", len(compiled.Conditions))
	}
	if compiled.Conditions["found"] == nil {
		t.Fatal("expected 'found' condition to be compiled")
	}
	t.Logf("Compiled condition 'found' source: %s", compiled.Conditions["found"].Source)

	// Verify step conditions
	for i, cs := range compiled.Steps {
		t.Logf("Step %d (%s): condition=%v, config.Condition=%s",
			i, cs.Config.Name, cs.Condition != nil, cs.Config.Condition)
	}

	// Check that the negated condition step has a compiled condition
	notFoundStep := compiled.Steps[2]
	if notFoundStep.Condition == nil {
		t.Error("expected not_found_response step to have compiled condition")
	}

	// Create mock database that returns empty results
	db := &mockDBManager{
		queryFunc: func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
			return &step.QueryResult{Rows: []map[string]any{}}, nil // Return empty result
		},
	}
	logger := &testLogger{}
	exec := NewExecutor(db, &mockHTTPClient{}, nil, logger)

	// Create a recorder to capture response
	recorder := httptest.NewRecorder()
	trigger := &TriggerData{Type: "http", Params: map[string]any{"id": 999}}
	result := exec.Execute(context.Background(), compiled, trigger, "req-1", recorder, nil)

	// Log what happened
	t.Logf("ResponseSent: %v", result.ResponseSent)
	t.Logf("Response body: %s", recorder.Body.String())
	t.Logf("Response status: %d", recorder.Code)

	// Check fetch step result
	if fetchResult, ok := result.Steps["fetch"]; ok {
		t.Logf("fetch step: count=%d, data=%v", fetchResult.Count, fetchResult.Data)
	}

	// Check that the "not found" response was sent
	if !result.ResponseSent {
		t.Error("Expected ResponseSent to be true")
	}

	// Response should be from the "not found" step with 404
	body := recorder.Body.String()
	if !strings.Contains(body, "not found") {
		t.Errorf("Expected 'not found' in response body, got: %s", body)
	}

	if recorder.Code != 404 {
		t.Errorf("Expected status 404, got %d", recorder.Code)
	}
}

func TestEvaluateStepParams_Integer(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create step with param that evaluates to an integer
	paramTmpl := template.Must(template.New("param").Parse("42"))
	cs := &CompiledStep{
		Config:     &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{"user_id": paramTmpl},
	}

	data := map[string]any{
		"trigger": map[string]any{"params": map[string]any{}},
	}

	err := exec.evaluateStepParams(cs, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params, ok := data["params"].(map[string]any)
	if !ok {
		t.Fatal("params not set in data")
	}

	userID, ok := params["user_id"]
	if !ok {
		t.Fatal("user_id not set in params")
	}

	// Should be parsed as int64
	if userID != int64(42) {
		t.Errorf("user_id = %v (%T), want int64(42)", userID, userID)
	}
}

func TestEvaluateStepParams_String(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create step with param that evaluates to a string (not a valid integer)
	paramTmpl := template.Must(template.New("param").Parse("hello-world"))
	cs := &CompiledStep{
		Config:     &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{"name": paramTmpl},
	}

	data := map[string]any{
		"trigger": map[string]any{"params": map[string]any{}},
	}

	err := exec.evaluateStepParams(cs, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params := data["params"].(map[string]any)
	name := params["name"]

	// Should remain as string
	if name != "hello-world" {
		t.Errorf("name = %v (%T), want \"hello-world\"", name, name)
	}
}

func TestEvaluateStepParams_TemplateError(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create step with param that references a non-existent field
	// Use missingkey=error option to make template error on missing keys
	paramTmpl := template.Must(template.New("param").Option("missingkey=error").Parse("{{.nonexistent.field}}"))
	cs := &CompiledStep{
		Config:     &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{"bad_param": paramTmpl},
	}

	data := map[string]any{
		"trigger": map[string]any{"params": map[string]any{}},
	}

	err := exec.evaluateStepParams(cs, data)
	if err == nil {
		t.Fatal("expected error for template execution failure")
	}

	if !strings.Contains(err.Error(), "bad_param") {
		t.Errorf("error should mention param name, got: %v", err)
	}
}

func TestEvaluateStepParams_MultipleParams(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create step with multiple params
	cs := &CompiledStep{
		Config: &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{
			"int_param":    template.Must(template.New("int").Parse("123")),
			"string_param": template.Must(template.New("string").Parse("test-value")),
			"float_int":    template.Must(template.New("float").Parse("456.0")), // Float with .0 should parse as int
		},
	}

	data := map[string]any{
		"trigger": map[string]any{"params": map[string]any{}},
	}

	err := exec.evaluateStepParams(cs, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params := data["params"].(map[string]any)

	// Check int_param
	if params["int_param"] != int64(123) {
		t.Errorf("int_param = %v (%T), want int64(123)", params["int_param"], params["int_param"])
	}

	// Check string_param
	if params["string_param"] != "test-value" {
		t.Errorf("string_param = %v (%T), want \"test-value\"", params["string_param"], params["string_param"])
	}

	// Check float_int (456.0 should be parsed as int64)
	if params["float_int"] != int64(456) {
		t.Errorf("float_int = %v (%T), want int64(456)", params["float_int"], params["float_int"])
	}
}

func TestEvaluateStepParams_UsesExistingParamsMap(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	paramTmpl := template.Must(template.New("param").Parse("new_value"))
	cs := &CompiledStep{
		Config:     &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{"new_param": paramTmpl},
	}

	// Pre-populate data with existing params
	existingParams := map[string]any{"existing_key": "existing_value"}
	data := map[string]any{
		"params":  existingParams,
		"trigger": map[string]any{"params": map[string]any{}},
	}

	err := exec.evaluateStepParams(cs, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params := data["params"].(map[string]any)

	// Existing key should be preserved
	if params["existing_key"] != "existing_value" {
		t.Errorf("existing_key = %v, want \"existing_value\"", params["existing_key"])
	}

	// New key should be added
	if params["new_param"] != "new_value" {
		t.Errorf("new_param = %v, want \"new_value\"", params["new_param"])
	}
}

func TestEvaluateStepParams_TemplateWithData(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Create step with param that uses trigger data
	paramTmpl := template.Must(template.New("param").Parse("{{.trigger.params.id}}"))
	cs := &CompiledStep{
		Config:     &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{"computed_id": paramTmpl},
	}

	data := map[string]any{
		"trigger": map[string]any{
			"params": map[string]any{"id": "99"},
		},
	}

	err := exec.evaluateStepParams(cs, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params := data["params"].(map[string]any)

	// Template should evaluate using trigger data, result is "99" which parses as int64
	if params["computed_id"] != int64(99) {
		t.Errorf("computed_id = %v (%T), want int64(99)", params["computed_id"], params["computed_id"])
	}
}

func TestEvaluateStepParams_EmptyParams(t *testing.T) {
	logger := &testLogger{}
	exec := NewExecutor(&mockDBManager{}, &mockHTTPClient{}, nil, logger)

	// Step with no param templates
	cs := &CompiledStep{
		Config:     &StepConfig{Name: "test_step"},
		ParamTmpls: map[string]*template.Template{},
	}

	data := map[string]any{
		"trigger": map[string]any{"params": map[string]any{}},
	}

	err := exec.evaluateStepParams(cs, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create empty params map
	params, ok := data["params"].(map[string]any)
	if !ok {
		t.Fatal("params should be set in data")
	}
	if len(params) != 0 {
		t.Errorf("params should be empty, got %v", params)
	}
}

// TestCompileAndEvaluate_ConditionAliases tests that condition aliases and negated aliases are properly compiled and evaluated.
func TestCompileAndEvaluate_ConditionAliases(t *testing.T) {
	wfConfig := &WorkflowConfig{
		Name: "test_conditional",
		Conditions: map[string]string{
			"found": "steps.fetch.count > 0",
		},
		Steps: []StepConfig{
			{
				Name:     "fetch",
				Type:     "query",
				Database: "testdb",
				SQL:      "SELECT * FROM items WHERE id = @id",
			},
			{
				Name:      "success_response",
				Type:      "response",
				Template:  `{"item": "found"}`,
				Condition: "found",
			},
			{
				Name:       "not_found_response",
				Type:       "response",
				StatusCode: 404,
				Template:   `{"error": "not found"}`,
				Condition:  "!found",
			},
		},
	}

	// Compile
	compiled, err := Compile(wfConfig)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Verify conditions map
	t.Logf("Compiled %d named conditions", len(compiled.Conditions))
	for name, cc := range compiled.Conditions {
		t.Logf("  %s: source=%q", name, cc.Source)
	}

	// Verify step conditions
	t.Logf("Compiled %d steps", len(compiled.Steps))
	for i, cs := range compiled.Steps {
		t.Logf("  Step %d: name=%s condition=%v config.Condition=%q",
			i, cs.Config.Name, cs.Condition != nil, cs.Config.Condition)
	}

	// Specific assertions
	if compiled.Conditions["found"] == nil {
		t.Error("expected 'found' condition to be compiled")
	}

	// success_response should have condition
	if compiled.Steps[1].Condition == nil {
		t.Error("expected success_response step to have condition")
	}

	// not_found_response should have condition (the negated alias)
	if compiled.Steps[2].Condition == nil {
		t.Error("expected not_found_response step to have condition (negated alias)")
	}

	// Test evaluation with empty results
	env := map[string]any{
		"steps": map[string]any{
			"fetch": map[string]any{
				"count": 0,
				"data":  []map[string]any{},
			},
		},
	}

	// Evaluate "found" - should be false
	foundResult, err := EvalCondition(compiled.Steps[1].Condition, env)
	if err != nil {
		t.Fatalf("error evaluating found condition: %v", err)
	}
	if foundResult {
		t.Error("expected 'found' condition to be false when count=0")
	}
	t.Logf("found condition result: %v", foundResult)

	// Evaluate "!found" - should be true
	notFoundResult, err := EvalCondition(compiled.Steps[2].Condition, env)
	if err != nil {
		t.Fatalf("error evaluating !found condition: %v", err)
	}
	if !notFoundResult {
		t.Error("expected '!found' condition to be true when count=0")
	}
	t.Logf("!found condition result: %v", notFoundResult)
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"positive integer", "123", 123, false},
		{"negative integer", "-456", -456, false},
		{"zero", "0", 0, false},
		{"with whitespace", "  123  ", 123, false},
		{"float whole number", "123.0", 123, false},
		{"large number", "9223372036854775807", 9223372036854775807, false},
		{"invalid string", "abc", 0, true},
		{"float with decimals", "123.5", 0, true},
		{"empty string", "", 0, true},
		// Boundary tests for int64 range
		{"min int64", "-9223372036854775808", -9223372036854775808, false},
		{"min int64 as float", "-9223372036854775808.0", -9223372036854775808, false},
		// Overflow tests - values outside int64 range
		{"overflow positive", "9223372036854775808", 0, true},  // max int64 + 1
		{"overflow negative", "-9223372036854775809", 0, true}, // min int64 - 1
		{"float overflow positive", "9.3e18", 0, true},         // larger than max int64
		{"float overflow negative", "-9.3e18", 0, true},        // smaller than min int64
		// Float precision test - small whole numbers should work
		{"small float", "1000000.0", 1000000, false},
		{"negative float", "-1000000.0", -1000000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInt64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInt64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseInt64() = %v, want %v", got, tt.want)
			}
		})
	}
}
