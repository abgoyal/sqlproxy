package db

import (
	"database/sql"
	"fmt"
	"time"

	"sql-proxy/internal/sqlutil"
	"sql-proxy/internal/workflow/step"
)

// ParamRegex matches @param style named parameters in SQL queries.
// Exported for use by other packages (e.g., validation).
var ParamRegex = sqlutil.ParamRegex

// QueryResult contains the results of a database query execution.
// For SELECT queries, Rows contains the returned data.
// For INSERT/UPDATE/DELETE, RowsAffected contains the number of affected rows.
type QueryResult = step.QueryResult

// IsWriteQuery returns true if the SQL is a write operation.
var IsWriteQuery = sqlutil.IsWriteQuery

// HasReturningClause returns true if a write query has OUTPUT/RETURNING.
var HasReturningClause = sqlutil.HasReturningClause

// resolveIsWrite returns the precomputed hint if available, otherwise parses the query.
func resolveIsWrite(hints *QueryHints, query string) bool {
	if hints != nil && hints.IsWrite != nil {
		return *hints.IsWrite
	}
	return IsWriteQuery(query)
}

// resolveHasReturning returns the precomputed hint if available, otherwise parses the query.
func resolveHasReturning(hints *QueryHints, query string) bool {
	if hints != nil && hints.HasReturning != nil {
		return *hints.HasReturning
	}
	return HasReturningClause(query)
}

// ScanRows converts sql.Rows to []map[string]any.
// Shared across database drivers.
func ScanRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			switch v := val.(type) {
			case []byte:
				row[col] = string(v)
			case time.Time:
				row[col] = v.Format(time.RFC3339)
			default:
				row[col] = v
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}
