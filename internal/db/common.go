package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ParamRegex matches @param style named parameters in SQL queries.
// Exported for use by other packages (e.g., validation).
var ParamRegex = regexp.MustCompile(`@(\w+)`)

// QueryResult contains the results of a database query execution.
// For SELECT queries, Rows contains the returned data.
// For INSERT/UPDATE/DELETE, RowsAffected contains the number of affected rows.
type QueryResult struct {
	Rows         []map[string]any
	RowsAffected int64
}

// IsWriteQuery returns true if the SQL appears to be a write operation.
// This is used to determine whether to use QueryContext or ExecContext.
func IsWriteQuery(sql string) bool {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return false
	}

	upper := strings.ToUpper(trimmed)

	// Handle CTEs: WITH ... AS (...) INSERT/UPDATE/DELETE
	// Skip past the WITH clause to find the actual operation
	if strings.HasPrefix(upper, "WITH ") {
		// Find the main query after all CTEs
		// CTEs can be nested, so look for write keywords anywhere after WITH
		// Write operations in CTEs themselves would also make this a write query
		for _, keyword := range []string{"INSERT ", "UPDATE ", "DELETE ", "CREATE ", "DROP ", "ALTER ", "TRUNCATE ", "MERGE "} {
			if strings.Contains(upper, keyword) {
				return true
			}
		}
		return false
	}

	// Get first word (uppercase for comparison)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	firstWord := strings.ToUpper(fields[0])

	switch firstWord {
	case "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "TRUNCATE", "MERGE":
		return true
	default:
		return false
	}
}

// ScanRows converts sql.Rows to []map[string]any.
// This is shared between SQLite and SQL Server drivers.
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
