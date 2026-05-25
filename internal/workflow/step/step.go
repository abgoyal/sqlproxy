package step

import (
	"context"
	"net/http"
)

// ExecutionData provides template and context data needed for step execution.
type ExecutionData struct {
	TemplateData   map[string]any
	ExprEnv        map[string]any
	ResponseWriter http.ResponseWriter
}

// DBManager interface for database operations.
type DBManager interface {
	ExecuteQuery(ctx context.Context, database, sql string, params map[string]any, opts QueryOptions) (*QueryResult, error)
}

// QueryResult contains database query execution results.
type QueryResult struct {
	Rows         []map[string]any
	RowsAffected int64
}

// QueryOptions contains options for query execution.
type QueryOptions struct {
	Isolation        string
	LockTimeoutMs    *int
	DeadlockPriority string
	JSONColumns      []string

	// IsWrite and HasReturning are precomputed SQL classification hints.
	// When non-nil, drivers use these instead of re-parsing the SQL at request time.
	IsWrite      *bool
	HasReturning *bool
}

// HTTPClient interface for HTTP operations.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Logger interface for step logging.
type Logger interface {
	Debug(msg string, fields map[string]any)
	Info(msg string, fields map[string]any)
	Warn(msg string, fields map[string]any)
	Error(msg string, fields map[string]any)
}
