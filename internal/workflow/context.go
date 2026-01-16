package workflow

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Context holds the execution state for a workflow run.
type Context struct {
	Workflow  *CompiledWorkflow
	Trigger   *TriggerData
	Steps     map[string]*StepResult
	RequestID string
	StartTime time.Time
	Logger    Logger

	mu  sync.RWMutex
	ctx context.Context
}

// Logger interface for workflow execution logging
type Logger interface {
	Debug(msg string, fields map[string]any)
	Info(msg string, fields map[string]any)
	Warn(msg string, fields map[string]any)
	Error(msg string, fields map[string]any)
}

// TriggerData contains input data from the trigger.
type TriggerData struct {
	Type string // "http" | "cron"

	// HTTP trigger data
	Params   map[string]any // Query/body parameters
	Headers  http.Header
	ClientIP string
	Method   string
	Path     string

	// Cron trigger data
	ScheduleTime time.Time
	CronExpr     string
}

// StepResult contains the result of executing a step.
type StepResult struct {
	Name       string
	Type       string // "query" | "httpcall" | "response" | "block"
	Success    bool
	Error      error
	StartTime  time.Time // Currently unused - reserved for future per-step timing
	DurationMs int64
	CacheHit   bool // True if result came from cache

	// Query results
	Data         []map[string]any
	Count        int
	RowsAffected int64 // For INSERT/UPDATE/DELETE operations

	// HTTPCall results
	StatusCode   int
	Headers      http.Header
	ResponseBody string

	// Block results
	Iterations   []*IterationResult
	SuccessCount int
	FailureCount int
	SkippedCount int // Currently unused - reserved for conditional skip tracking
}

// IterationResult contains the result of a single iteration in a block.
type IterationResult struct {
	Index   int
	Item    any
	Success bool
	Error   error
	Steps   map[string]*StepResult
}

// NewContext creates a new workflow execution context.
func NewContext(ctx context.Context, wf *CompiledWorkflow, trigger *TriggerData, requestID string, logger Logger) *Context {
	return &Context{
		Workflow:  wf,
		Trigger:   trigger,
		Steps:     make(map[string]*StepResult),
		RequestID: requestID,
		StartTime: time.Now(),
		Logger:    logger,
		ctx:       ctx,
	}
}

// Context returns the underlying context.Context for cancellation.
func (c *Context) Context() context.Context {
	return c.ctx
}

// SetStepResult records the result of a step execution.
func (c *Context) SetStepResult(name string, result *StepResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Steps[name] = result
}

// GetStepResult retrieves a step result by name.
func (c *Context) GetStepResult(name string) *StepResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Steps[name]
}

// BuildExprEnv builds the environment map for expr evaluation.
// This includes steps results, trigger data, and workflow metadata.
func (c *Context) BuildExprEnv() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	env := make(map[string]any)

	// Add step results
	steps := make(map[string]any)
	for name, result := range c.Steps {
		steps[name] = stepResultToMap(result)
	}
	env["steps"] = steps

	// Add trigger data
	trigger := make(map[string]any)
	trigger["type"] = c.Trigger.Type
	trigger["params"] = c.Trigger.Params
	if c.Trigger.Type == "http" {
		trigger["headers"] = headerToMap(c.Trigger.Headers)
		trigger["client_ip"] = c.Trigger.ClientIP
		trigger["method"] = c.Trigger.Method
		trigger["path"] = c.Trigger.Path
	} else {
		trigger["schedule_time"] = c.Trigger.ScheduleTime
		trigger["cron"] = c.Trigger.CronExpr
	}
	env["trigger"] = trigger

	// Add workflow metadata
	workflow := make(map[string]any)
	workflow["name"] = c.Workflow.Config.Name
	workflow["start_time"] = c.StartTime
	workflow["request_id"] = c.RequestID
	env["workflow"] = workflow

	// Add Param as shortcut for trigger.params (for cache key templates)
	env["Param"] = c.Trigger.Params

	return env
}

// BuildTemplateData builds the data map for Go text/template execution.
// Uses the same structure as BuildExprEnv for consistency.
func (c *Context) BuildTemplateData() map[string]any {
	return c.BuildExprEnv()
}

func stepResultToMap(r *StepResult) map[string]any {
	m := map[string]any{
		"name":        r.Name,
		"type":        r.Type,
		"success":     r.Success,
		"duration_ms": r.DurationMs,
		"cache_hit":   r.CacheHit,
	}

	if r.Error != nil {
		m["error"] = r.Error.Error()
	}

	// Query data - always set count for query steps (even if data is nil/empty)
	if r.Type == "query" {
		if r.Data != nil {
			m["data"] = r.Data
		} else {
			m["data"] = []map[string]any{} // Ensure data is never nil
		}
		m["count"] = r.Count
		m["rows_affected"] = r.RowsAffected
	}

	// HTTPCall data
	if r.Type == "httpcall" {
		m["status_code"] = r.StatusCode
		m["headers"] = headerToMap(r.Headers)
		m["body"] = r.ResponseBody
		// Expose parsed data when available (from parse: json or parse: form)
		// Always set data and count for consistency with query steps
		if r.Data != nil {
			m["data"] = r.Data
		} else {
			m["data"] = []map[string]any{} // Ensure data is never nil
		}
		m["count"] = r.Count
	}

	// Block data
	if r.Type == "block" {
		m["success_count"] = r.SuccessCount
		m["failure_count"] = r.FailureCount
		m["skipped_count"] = r.SkippedCount

		if r.Iterations != nil {
			iterations := make([]map[string]any, len(r.Iterations))
			for i, iter := range r.Iterations {
				iterations[i] = map[string]any{
					"index":   iter.Index,
					"item":    iter.Item,
					"success": iter.Success,
				}
				if iter.Error != nil {
					iterations[i]["error"] = iter.Error.Error()
				}
			}
			m["iterations"] = iterations
		} else {
			m["iterations"] = []map[string]any{}
		}
	}

	return m
}

func headerToMap(h http.Header) map[string]any {
	m := make(map[string]any)
	for k, v := range h {
		if len(v) == 1 {
			m[k] = v[0]
		} else {
			m[k] = v
		}
	}
	return m
}

// BlockContext holds execution state for a block iteration.
type BlockContext struct {
	Parent       *Context
	BlockName    string
	Steps        map[string]*StepResult
	CurrentItem  any
	CurrentIndex int
	TotalCount   int

	mu sync.RWMutex
}

// NewBlockContext creates a context for block iteration.
func NewBlockContext(parent *Context, blockName string, item any, index, total int) *BlockContext {
	return &BlockContext{
		Parent:       parent,
		BlockName:    blockName,
		Steps:        make(map[string]*StepResult),
		CurrentItem:  item,
		CurrentIndex: index,
		TotalCount:   total,
	}
}

// SetStepResult records a step result within the block.
func (b *BlockContext) SetStepResult(name string, result *StepResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Steps[name] = result
}

// GetStepResult retrieves a step result from this block or parent.
func (b *BlockContext) GetStepResult(name string) *StepResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if result, ok := b.Steps[name]; ok {
		return result
	}
	return nil
}

// BuildExprEnv builds the environment for block-level expr evaluation.
func (b *BlockContext) BuildExprEnv(iterateAs string) map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()

	env := make(map[string]any)

	// Add block step results
	steps := make(map[string]any)
	for name, result := range b.Steps {
		steps[name] = stepResultToMap(result)
	}
	env["steps"] = steps

	// Add parent step results under "parent"
	parent := b.Parent.BuildExprEnv()
	env["parent"] = parent

	// Add current item
	if iterateAs != "" {
		env[iterateAs] = b.CurrentItem
	}
	env["_index"] = b.CurrentIndex
	env["_count"] = b.TotalCount

	// Forward trigger and workflow from parent
	env["trigger"] = parent["trigger"]
	env["workflow"] = parent["workflow"]

	return env
}

// BuildTemplateData builds template data for block-level templates.
func (b *BlockContext) BuildTemplateData(iterateAs string) map[string]any {
	return b.BuildExprEnv(iterateAs)
}
