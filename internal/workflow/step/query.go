package step

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"text/template"
	"time"
)

// QueryStep executes a SQL query.
type QueryStep struct {
	Name             string
	Database         string
	SQLTemplate      *template.Template
	Isolation        string
	LockTimeoutMs    *int
	DeadlockPriority string
	JSONColumns      []string
}

// NewQueryStep creates a query step from configuration.
func NewQueryStep(name, database string, sqlTmpl *template.Template, isolation string, lockTimeoutMs *int, deadlockPriority string, jsonColumns []string) *QueryStep {
	return &QueryStep{
		Name:             name,
		Database:         database,
		SQLTemplate:      sqlTmpl,
		Isolation:        isolation,
		LockTimeoutMs:    lockTimeoutMs,
		DeadlockPriority: deadlockPriority,
		JSONColumns:      jsonColumns,
	}
}

func (s *QueryStep) Type() string {
	return "query"
}

func (s *QueryStep) Execute(ctx context.Context, data ExecutionData) (*Result, error) {
	start := time.Now()
	result := &Result{}

	// Render SQL template
	var sqlBuf bytes.Buffer
	if err := s.SQLTemplate.Execute(&sqlBuf, data.TemplateData); err != nil {
		result.Error = fmt.Errorf("sql template error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	sql := sqlBuf.String()

	// Extract parameters from template data
	params := extractSQLParams(sql, data.TemplateData)

	// Execute query
	opts := QueryOptions{
		Isolation:        s.Isolation,
		LockTimeoutMs:    s.LockTimeoutMs,
		DeadlockPriority: s.DeadlockPriority,
		JSONColumns:      s.JSONColumns,
	}

	qr, err := data.DBManager.ExecuteQuery(ctx, s.Database, sql, params, opts)
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

	if data.Logger != nil {
		data.Logger.Debug("query_step_executed", map[string]any{
			"step":        s.Name,
			"database":    s.Database,
			"row_count":   result.Count,
			"duration_ms": result.DurationMs,
		})
	}

	return result, nil
}

// sqlParamRegex matches @param in SQL
var sqlParamRegex = regexp.MustCompile(`@([a-zA-Z_][a-zA-Z0-9_]*)`)

// extractSQLParams extracts parameter values from template data for SQL execution.
// It looks for @param references in SQL and extracts corresponding values.
// Search order (deterministic):
// 1. trigger.params (HTTP parameters)
// 2. Flattened iteration variable fields (for block iteration over objects) - sorted alphabetically
// 3. Direct data keys (for simple iteration values)
func extractSQLParams(sql string, data map[string]any) map[string]any {
	params := make(map[string]any)

	matches := sqlParamRegex.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		paramName := match[1]

		// Look for param in trigger.params first
		if trigger, ok := data["trigger"].(map[string]any); ok {
			if trigParams, ok := trigger["params"].(map[string]any); ok {
				if val, ok := trigParams[paramName]; ok {
					params[paramName] = val
					continue
				}
			}
		}

		// Check all top-level map values for the param (supports iteration over objects)
		// This allows @title to find data["task"]["title"] when iterating with as: "task"
		// Use sorted keys for deterministic behavior
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

		// Check if it's directly in data (for simple iteration values)
		if val, ok := data[paramName]; ok {
			params[paramName] = val
		}
	}

	return params
}
