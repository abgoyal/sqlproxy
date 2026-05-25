package workflow

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"sql-proxy/internal/workflow/step"
)

func (e *Executor) executeQueryStep(ctx context.Context, cs *CompiledStep, execData step.ExecutionData) (*StepResult, error) {
	start := time.Now()
	result := &StepResult{}

	var sqlBuf bytes.Buffer
	if err := cs.SQLTmpl.Execute(&sqlBuf, execData.TemplateData); err != nil {
		result.Error = fmt.Errorf("sql template error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	sql := sqlBuf.String()

	params := extractSQLParams(sql, execData.TemplateData)

	opts := step.QueryOptions{
		Isolation:        cs.Config.Isolation,
		LockTimeoutMs:    cs.Config.LockTimeoutMs,
		DeadlockPriority: cs.Config.DeadlockPriority,
		JSONColumns:      cs.Config.JSONColumns,
		IsWrite:          &cs.IsWrite,
		HasReturning:     &cs.HasReturning,
	}

	qr, err := e.dbManager.ExecuteQuery(ctx, cs.Config.Database, sql, params, opts)
	if err != nil {
		result.Error = err
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	result.Success = true
	result.Data = qr.Rows
	result.Count = len(qr.Rows)
	result.RowsAffected = qr.RowsAffected
	result.DurationMs = time.Since(start).Milliseconds()

	e.logger.Debug("query_step_executed", map[string]any{
		"step":        cs.Config.Name,
		"database":    cs.Config.Database,
		"row_count":   result.Count,
		"duration_ms": result.DurationMs,
	})

	return result, nil
}

var sqlParamRegex = regexp.MustCompile(`@([a-zA-Z_][a-zA-Z0-9_]*)`)

// extractSQLParams extracts parameter values from template data for SQL execution.
// Search order (deterministic):
// 1. params (step-level computed params)
// 2. trigger.params (HTTP parameters)
// 3. Flattened iteration variable fields (for block iteration over objects) - sorted alphabetically
// 4. Direct data keys (for simple iteration values)
func extractSQLParams(sql string, data map[string]any) map[string]any {
	params := make(map[string]any)

	matches := sqlParamRegex.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		paramName := match[1]

		if stepParams, ok := data["params"].(map[string]any); ok {
			if val, ok := stepParams[paramName]; ok {
				params[paramName] = val
				continue
			}
		}

		if trigger, ok := data["trigger"].(map[string]any); ok {
			if trigParams, ok := trigger["params"].(map[string]any); ok {
				if val, ok := trigParams[paramName]; ok {
					params[paramName] = val
					continue
				}
			}
		}

		found := false
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := data[k]
			if m, ok := v.(map[string]any); ok {
				if val, ok := m[paramName]; ok {
					params[paramName] = val
					found = true
					break
				}
			}
		}
		if found {
			continue
		}

		if val, ok := data[paramName]; ok {
			params[paramName] = val
		}
	}

	return params
}
