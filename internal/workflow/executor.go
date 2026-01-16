package workflow

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"text/template"
	"time"

	"sql-proxy/internal/workflow/step"
)

// StepCache provides caching for workflow step results.
type StepCache interface {
	// Get retrieves cached step data. Returns data and hit status.
	Get(workflow, key string) ([]map[string]any, bool)
	// Set stores step data in the cache with the given TTL.
	Set(workflow, key string, data []map[string]any, ttl time.Duration) bool
}

// Executor runs compiled workflows.
type Executor struct {
	dbManager  step.DBManager
	httpClient step.HTTPClient
	cache      StepCache
	logger     Logger
}

// NewExecutor creates a workflow executor.
func NewExecutor(dbManager step.DBManager, httpClient step.HTTPClient, cache StepCache, logger Logger) *Executor {
	return &Executor{
		dbManager:  dbManager,
		httpClient: httpClient,
		cache:      cache,
		logger:     logger,
	}
}

// Logger returns the executor's logger.
func (e *Executor) Logger() Logger {
	return e.logger
}

// ExecuteResult contains the result of workflow execution.
type ExecuteResult struct {
	Success      bool
	Error        error
	ResponseSent bool
	DurationMs   int64
	Steps        map[string]*StepResult
}

// Execute runs a workflow with the given trigger data.
func (e *Executor) Execute(ctx context.Context, wf *CompiledWorkflow, trigger *TriggerData, requestID string, w http.ResponseWriter) *ExecuteResult {
	start := time.Now()
	result := &ExecuteResult{
		Steps: make(map[string]*StepResult),
	}

	// Apply workflow timeout if set (must be done before creating wfCtx)
	if wf.Config.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(wf.Config.TimeoutSec)*time.Second)
		defer cancel()
	}

	// Create workflow context with the (potentially timeout-wrapped) context
	wfCtx := NewContext(ctx, wf, trigger, requestID, e.logger)

	e.logger.Info("workflow_started", map[string]any{
		"workflow":   wf.Config.Name,
		"request_id": requestID,
		"trigger":    trigger.Type,
	})

	// Execute steps
	for i, compiledStep := range wf.Steps {
		if compiledStep.Config.Disabled {
			continue
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		default:
		}

		// Evaluate condition if present
		if compiledStep.Condition != nil {
			env := wfCtx.BuildExprEnv()
			shouldRun, err := EvalCondition(compiledStep.Condition, env)
			if err != nil {
				e.logger.Warn("step_condition_error", map[string]any{
					"workflow":   wf.Config.Name,
					"step":       compiledStep.Config.Name,
					"step_index": i,
					"error":      err.Error(),
				})
				// Treat condition error as false
				continue
			}
			if !shouldRun {
				e.logger.Debug("step_skipped_condition", map[string]any{
					"workflow":   wf.Config.Name,
					"step":       compiledStep.Config.Name,
					"step_index": i,
				})
				continue
			}
		}

		// Execute the step
		stepResult, err := e.executeStep(ctx, compiledStep, wfCtx, w)
		if err != nil {
			result.Error = err
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		}

		// Record result
		stepName := compiledStep.Config.Name
		if stepName == "" {
			stepName = fmt.Sprintf("step_%d", i)
		}
		stepResult.Name = stepName
		stepResult.Type = compiledStep.Config.StepType()
		wfCtx.SetStepResult(stepName, stepResult)
		result.Steps[stepName] = stepResult

		// Check if response was sent
		if compiledStep.Config.IsResponse() && stepResult.Success {
			result.ResponseSent = true
		}

		// Handle step failure
		if !stepResult.Success {
			onError := compiledStep.Config.OnError
			if onError == "" {
				onError = "abort" // Default
			}

			errMsg := "unknown error"
			if stepResult.Error != nil {
				errMsg = stepResult.Error.Error()
			}

			if onError == "abort" {
				result.Error = stepResult.Error
				result.DurationMs = time.Since(start).Milliseconds()
				e.logger.Error("workflow_step_failed", map[string]any{
					"workflow": wf.Config.Name,
					"step":     stepName,
					"error":    errMsg,
					"on_error": onError,
				})
				return result
			}
			// on_error: continue - log and proceed
			e.logger.Warn("workflow_step_failed_continue", map[string]any{
				"workflow": wf.Config.Name,
				"step":     stepName,
				"error":    errMsg,
			})
		}
	}

	result.Success = true
	result.DurationMs = time.Since(start).Milliseconds()

	// Warn if HTTP trigger but no response sent
	if trigger.Type == "http" && !result.ResponseSent {
		e.logger.Warn("workflow_no_response", map[string]any{
			"workflow":   wf.Config.Name,
			"request_id": requestID,
		})
	}

	e.logger.Info("workflow_completed", map[string]any{
		"workflow":      wf.Config.Name,
		"request_id":    requestID,
		"duration_ms":   result.DurationMs,
		"response_sent": result.ResponseSent,
	})

	return result
}

func (e *Executor) executeStep(ctx context.Context, cs *CompiledStep, wfCtx *Context, w http.ResponseWriter) (*StepResult, error) {
	stepType := cs.Config.StepType()

	// Build execution data
	execData := step.ExecutionData{
		TemplateData:   wfCtx.BuildTemplateData(),
		ExprEnv:        wfCtx.BuildExprEnv(),
		DBManager:      e.dbManager,
		HTTPClient:     e.httpClient,
		ResponseWriter: w,
		Logger:         e.logger,
	}

	// Check cache for cacheable steps (query, httpcall)
	if (stepType == "query" || stepType == "httpcall") && cs.CacheKeyTmpl != nil && e.cache != nil {
		cacheKey, err := e.evaluateCacheKey(cs.CacheKeyTmpl, execData.TemplateData)
		if err != nil {
			e.logger.Warn("step_cache_key_error", map[string]any{
				"workflow": wfCtx.Workflow.Config.Name,
				"step":     cs.Config.Name,
				"error":    err.Error(),
			})
			// Continue without caching on key evaluation error
		} else {
			// Check cache
			workflowName := wfCtx.Workflow.Config.Name
			if data, hit := e.cache.Get(workflowName, cacheKey); hit {
				e.logger.Debug("step_cache_hit", map[string]any{
					"workflow":  workflowName,
					"step":      cs.Config.Name,
					"cache_key": cacheKey,
				})
				return &StepResult{
					Success:  true,
					CacheHit: true,
					Data:     data,
					Count:    len(data),
				}, nil
			}

			// Cache miss - execute and cache result
			result, err := e.executeStepByType(ctx, stepType, cs, execData, wfCtx, w)
			if err != nil {
				return nil, err
			}

			// Cache successful results with data
			if result.Success && result.Data != nil {
				ttl := time.Duration(0)
				if cs.Config.Cache != nil && cs.Config.Cache.TTLSec > 0 {
					ttl = time.Duration(cs.Config.Cache.TTLSec) * time.Second
				}
				e.cache.Set(workflowName, cacheKey, result.Data, ttl)
				e.logger.Debug("step_cache_set", map[string]any{
					"workflow":  workflowName,
					"step":      cs.Config.Name,
					"cache_key": cacheKey,
					"ttl_sec":   ttl.Seconds(),
				})
			}

			return result, nil
		}
	}

	return e.executeStepByType(ctx, stepType, cs, execData, wfCtx, w)
}

func (e *Executor) executeStepByType(ctx context.Context, stepType string, cs *CompiledStep, execData step.ExecutionData, wfCtx *Context, w http.ResponseWriter) (*StepResult, error) {
	switch stepType {
	case "query":
		return e.executeQueryStep(ctx, cs, execData)
	case "httpcall":
		return e.executeHTTPCallStep(ctx, cs, execData)
	case "response":
		return e.executeResponseStep(ctx, cs, execData)
	case "block":
		return e.executeBlockStep(ctx, cs, wfCtx, w)
	default:
		return &StepResult{Error: fmt.Errorf("unknown step type: %s", stepType)}, nil
	}
}

func (e *Executor) evaluateCacheKey(tmpl *template.Template, data map[string]any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("evaluating cache key: %w", err)
	}
	return buf.String(), nil
}

func (e *Executor) executeQueryStep(ctx context.Context, cs *CompiledStep, execData step.ExecutionData) (*StepResult, error) {
	qs := step.NewQueryStep(
		cs.Config.Name,
		cs.Config.Database,
		cs.SQLTmpl,
		cs.Config.Isolation,
		cs.Config.LockTimeoutMs,
		cs.Config.DeadlockPriority,
		cs.Config.JSONColumns,
	)

	result, err := qs.Execute(ctx, execData)
	if err != nil {
		return nil, err
	}

	return &StepResult{
		Success:      result.Success,
		Error:        result.Error,
		DurationMs:   result.DurationMs,
		Data:         result.Data,
		Count:        result.Count,
		RowsAffected: result.RowsAffected,
	}, nil
}

func (e *Executor) executeHTTPCallStep(ctx context.Context, cs *CompiledStep, execData step.ExecutionData) (*StepResult, error) {
	var retry *step.RetryConfig
	if cs.Config.Retry != nil {
		retry = &step.RetryConfig{
			Enabled:           cs.Config.Retry.Enabled,
			MaxAttempts:       cs.Config.Retry.MaxAttempts,
			InitialBackoffSec: cs.Config.Retry.InitialBackoffSec,
			MaxBackoffSec:     cs.Config.Retry.MaxBackoffSec,
		}
	}

	hs := step.NewHTTPCallStep(
		cs.Config.Name,
		cs.URLTmpl,
		cs.Config.HTTPMethod,
		cs.HeaderTmpls,
		cs.BodyTmpl,
		cs.Config.Parse,
		cs.Config.TimeoutSec,
		retry,
	)

	result, err := hs.Execute(ctx, execData)
	if err != nil {
		return nil, err
	}

	return &StepResult{
		Success:      result.Success,
		Error:        result.Error,
		DurationMs:   result.DurationMs,
		Data:         result.Data,
		Count:        result.Count,
		StatusCode:   result.StatusCode,
		Headers:      result.Headers,
		ResponseBody: result.ResponseBody,
	}, nil
}

func (e *Executor) executeResponseStep(ctx context.Context, cs *CompiledStep, execData step.ExecutionData) (*StepResult, error) {
	rs := step.NewResponseStep(
		cs.Config.Name,
		cs.Config.StatusCode,
		cs.TemplateTmpl,
		cs.HeaderTmpls,
		"application/json",
	)

	result, err := rs.Execute(ctx, execData)
	if err != nil {
		return nil, err
	}

	return &StepResult{
		Success:      result.Success,
		Error:        result.Error,
		DurationMs:   result.DurationMs,
		StatusCode:   result.StatusCode,
		ResponseBody: result.ResponseBody,
	}, nil
}

func (e *Executor) executeBlockStep(ctx context.Context, cs *CompiledStep, wfCtx *Context, w http.ResponseWriter) (*StepResult, error) {
	result := &StepResult{
		Type:       "block",
		Iterations: make([]*IterationResult, 0),
	}
	start := time.Now()

	// Get collection to iterate over
	var items []any
	if cs.Iterate != nil && cs.Iterate.OverExpr != nil {
		env := wfCtx.BuildExprEnv()
		val, err := EvalExpression(cs.Iterate.OverExpr, env)
		if err != nil {
			result.Error = fmt.Errorf("iterate.over expression error: %w", err)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}

		// Convert to slice
		switch v := val.(type) {
		case []any:
			items = v
		case []map[string]any:
			items = make([]any, len(v))
			for i, m := range v {
				items[i] = m
			}
		default:
			result.Error = fmt.Errorf("iterate.over must return array, got %T", val)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}
	} else {
		// No iteration - execute block steps once
		items = []any{nil}
	}

	iterateAs := ""
	onError := "abort"
	if cs.Iterate != nil && cs.Iterate.Config != nil {
		iterateAs = cs.Iterate.Config.As
		if cs.Iterate.Config.OnError != "" {
			onError = cs.Iterate.Config.OnError
		}
	}

	// Execute for each item
	for i, item := range items {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		default:
		}

		iterResult := &IterationResult{
			Index:   i,
			Item:    item,
			Steps:   make(map[string]*StepResult),
			Success: true,
		}

		// Create block context
		blockCtx := NewBlockContext(wfCtx, cs.Config.Name, item, i, len(items))

		// Execute nested steps
		for j, nestedStep := range cs.BlockSteps {
			if nestedStep.Config.Disabled {
				continue
			}

			// Evaluate condition
			if nestedStep.Condition != nil {
				env := blockCtx.BuildExprEnv(iterateAs)
				shouldRun, err := EvalCondition(nestedStep.Condition, env)
				if err != nil {
					e.logger.Warn("block_step_condition_error", map[string]any{
						"block":     cs.Config.Name,
						"step":      nestedStep.Config.Name,
						"iteration": i,
						"error":     err.Error(),
					})
					continue
				}
				if !shouldRun {
					continue
				}
			}

			// Build execution data for block step
			execData := step.ExecutionData{
				TemplateData:   blockCtx.BuildTemplateData(iterateAs),
				ExprEnv:        blockCtx.BuildExprEnv(iterateAs),
				DBManager:      e.dbManager,
				HTTPClient:     e.httpClient,
				ResponseWriter: w,
				Logger:         e.logger,
			}

			var stepResult *StepResult
			var err error

			switch nestedStep.Config.StepType() {
			case "query":
				stepResult, err = e.executeQueryStep(ctx, nestedStep, execData)
			case "httpcall":
				stepResult, err = e.executeHTTPCallStep(ctx, nestedStep, execData)
			default:
				err = fmt.Errorf("unsupported step type in block: %s", nestedStep.Config.StepType())
			}

			if err != nil {
				result.Error = err
				result.DurationMs = time.Since(start).Milliseconds()
				return result, nil
			}

			stepName := nestedStep.Config.Name
			if stepName == "" {
				stepName = fmt.Sprintf("step_%d", j)
			}
			stepResult.Name = stepName
			stepResult.Type = nestedStep.Config.StepType()

			blockCtx.SetStepResult(stepName, stepResult)
			iterResult.Steps[stepName] = stepResult

			if !stepResult.Success {
				iterResult.Success = false
				iterResult.Error = stepResult.Error

				stepOnError := nestedStep.Config.OnError
				if stepOnError == "" {
					stepOnError = "abort"
				}

				if stepOnError == "abort" {
					break // Stop this iteration's steps
				}
			}
		}

		result.Iterations = append(result.Iterations, iterResult)

		if iterResult.Success {
			result.SuccessCount++
		} else {
			result.FailureCount++

			if onError == "abort" {
				result.Error = iterResult.Error
				break // Stop all iterations
			}
			// continue or skip - proceed to next iteration
		}
	}

	result.Success = result.FailureCount == 0
	result.DurationMs = time.Since(start).Milliseconds()

	e.logger.Debug("block_step_completed", map[string]any{
		"block":         cs.Config.Name,
		"total":         len(items),
		"success_count": result.SuccessCount,
		"failure_count": result.FailureCount,
		"duration_ms":   result.DurationMs,
	})

	return result, nil
}
