package step

import (
	"context"
	"net/http"
)

// Step is the interface that all step types implement.
type Step interface {
	// Execute runs the step and returns a result.
	Execute(ctx context.Context, data ExecutionData) (*Result, error)

	// Type returns the step type name.
	Type() string
}

// ExecutionData provides data needed for step execution.
type ExecutionData struct {
	// TemplateData is the data map for template execution.
	TemplateData map[string]any

	// ExprEnv is the environment for expression evaluation.
	ExprEnv map[string]any

	// DBManager provides database access for query steps.
	DBManager DBManager

	// HTTPClient provides HTTP client for httpcall steps.
	HTTPClient HTTPClient

	// ResponseWriter is set for response steps to write HTTP response.
	ResponseWriter http.ResponseWriter

	// Logger for step execution logging.
	Logger Logger
}

// Result contains the outcome of step execution.
type Result struct {
	Success    bool
	Error      error
	DurationMs int64

	// Query results
	Data         []map[string]any
	Count        int
	RowsAffected int64 // For INSERT/UPDATE/DELETE operations

	// HTTPCall results
	StatusCode   int
	Headers      http.Header
	ResponseBody string
}

// DBManager interface for database operations.
type DBManager interface {
	// ExecuteQuery runs a query and returns results.
	// Returns rows for SELECT, rows affected for writes.
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
