package workflow

import "sql-proxy/internal/types"

// Step type constants
const (
	StepTypeQuery    = "query"
	StepTypeHTTPCall = "httpcall"
	StepTypeResponse = "response"
	StepTypeBlock    = "block"
	StepTypeUnknown  = "unknown"
)

// Trigger type constants
const (
	TriggerTypeHTTP = "http"
	TriggerTypeCron = "cron"
)

// ParamConfig is re-exported from internal/types for workflow parameters
type ParamConfig = types.ParamConfig

// WorkflowConfig defines a complete workflow with triggers and steps.
type WorkflowConfig struct {
	Name       string            `yaml:"name"`
	TimeoutSec int               `yaml:"timeout_sec,omitempty"`
	Conditions map[string]string `yaml:"conditions,omitempty"` // Named condition aliases
	Triggers   []TriggerConfig   `yaml:"triggers"`
	Steps      []StepConfig      `yaml:"steps"`
}

// TriggerConfig defines how a workflow is initiated.
type TriggerConfig struct {
	Type string `yaml:"type"` // "http" | "cron"

	// HTTP trigger fields
	Path       string               `yaml:"path,omitempty"`
	Method     string               `yaml:"method,omitempty"`
	Parameters []ParamConfig        `yaml:"parameters,omitempty"`
	RateLimit  []RateLimitRefConfig `yaml:"rate_limit,omitempty"`
	Cache      *CacheConfig         `yaml:"cache,omitempty"`

	// Cron trigger fields
	Schedule string            `yaml:"schedule,omitempty"`
	Params   map[string]string `yaml:"params,omitempty"`
}

// RateLimitRefConfig references a rate limit pool or defines inline limits.
type RateLimitRefConfig struct {
	Pool              string `yaml:"pool,omitempty"`
	RequestsPerSecond int    `yaml:"requests_per_second,omitempty"`
	Burst             int    `yaml:"burst,omitempty"`
	Key               string `yaml:"key,omitempty"`
}

// CacheConfig defines caching for HTTP triggers.
type CacheConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Key       string `yaml:"key"`
	TTLSec    int    `yaml:"ttl_sec,omitempty"`
	MaxSizeMB int    `yaml:"max_size_mb,omitempty"`
	EvictCron string `yaml:"evict_cron,omitempty"`
}

// StepConfig defines a single step or block in a workflow.
type StepConfig struct {
	// Common fields
	Name      string `yaml:"name,omitempty"`
	Disabled  bool   `yaml:"disabled,omitempty"`
	Condition string `yaml:"condition,omitempty"`
	OnError   string `yaml:"on_error,omitempty"` // "abort" | "continue"

	// Step type (leaf steps only; blocks have steps: instead)
	Type string `yaml:"type,omitempty"` // "query" | "httpcall" | "response"

	// Caching for query and httpcall steps
	Cache *StepCacheConfig `yaml:"cache,omitempty"`

	// Query step fields
	Database         string   `yaml:"database,omitempty"`
	SQL              string   `yaml:"sql,omitempty"`
	Isolation        string   `yaml:"isolation,omitempty"`
	LockTimeoutMs    *int     `yaml:"lock_timeout_ms,omitempty"`
	DeadlockPriority string   `yaml:"deadlock_priority,omitempty"`
	JSONColumns      []string `yaml:"json_columns,omitempty"`

	// HTTPCall step fields
	URL        string            `yaml:"url,omitempty"`
	HTTPMethod string            `yaml:"http_method,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	Body       string            `yaml:"body,omitempty"`
	Parse      string            `yaml:"parse,omitempty"` // "json" | "text" | "form"
	TimeoutSec int               `yaml:"timeout_sec,omitempty"`
	Retry      *RetryConfig      `yaml:"retry,omitempty"`

	// Response step fields
	StatusCode int    `yaml:"status_code,omitempty"`
	Template   string `yaml:"template,omitempty"`

	// Block fields (steps with nested steps)
	Iterate *IterateConfig    `yaml:"iterate,omitempty"`
	Inputs  map[string]string `yaml:"inputs,omitempty"`
	Steps   []StepConfig      `yaml:"steps,omitempty"` // Nested steps create a block
	Outputs map[string]string `yaml:"outputs,omitempty"`
}

// StepCacheConfig defines caching for query and httpcall steps.
// Cache key can reference request params and previous step results.
type StepCacheConfig struct {
	Key    string `yaml:"key"`               // Template for cache key (e.g., "user:{{.Param.id}}" or "query:{{.steps.auth.data.user_id}}")
	TTLSec int    `yaml:"ttl_sec,omitempty"` // TTL in seconds (0 = use server default)
}

// IterateConfig defines iteration over a collection.
type IterateConfig struct {
	Over    string `yaml:"over"`     // Expression like "steps.fetch.data"
	As      string `yaml:"as"`       // Variable name for current item
	OnError string `yaml:"on_error"` // "abort" | "continue" | "skip"
}

// RetryConfig defines retry behavior for httpcall steps.
type RetryConfig struct {
	Enabled           bool `yaml:"enabled"`
	MaxAttempts       int  `yaml:"max_attempts,omitempty"`
	InitialBackoffSec int  `yaml:"initial_backoff_sec,omitempty"`
	MaxBackoffSec     int  `yaml:"max_backoff_sec,omitempty"`
}

// IsBlock returns true if this step is a block (has steps: key in config).
// A nil Steps means no steps: key was present. An empty slice means steps: was present but empty.
func (s *StepConfig) IsBlock() bool {
	return s.Steps != nil
}

// IsQuery returns true if this step is a query step.
func (s *StepConfig) IsQuery() bool {
	return s.Type == "query" || (s.Type == "" && s.SQL != "")
}

// IsHTTPCall returns true if this step is an httpcall step.
func (s *StepConfig) IsHTTPCall() bool {
	return s.Type == "httpcall" || (s.Type == "" && s.URL != "")
}

// IsResponse returns true if this step is a response step.
func (s *StepConfig) IsResponse() bool {
	return s.Type == "response"
}

// StepType returns the resolved step type.
func (s *StepConfig) StepType() string {
	if s.IsBlock() {
		return StepTypeBlock
	}
	if s.Type != "" {
		return s.Type
	}
	if s.SQL != "" {
		return StepTypeQuery
	}
	if s.URL != "" {
		return StepTypeHTTPCall
	}
	if s.Template != "" {
		return StepTypeResponse
	}
	return StepTypeUnknown
}

// Valid step types
var ValidStepTypes = map[string]bool{
	"query":    true,
	"httpcall": true,
	"response": true,
}

// Valid trigger types
var ValidTriggerTypes = map[string]bool{
	"http": true,
	"cron": true,
}

// Valid on_error values
var ValidOnErrorValues = map[string]bool{
	"abort":    true,
	"continue": true,
}

// Valid iterate on_error values
var ValidIterateOnErrorValues = map[string]bool{
	"abort":    true,
	"continue": true,
	"skip":     true,
}

// Valid parse modes for httpcall
var ValidParseModes = map[string]bool{
	"json": true,
	"text": true,
	"form": true,
	"":     true, // Default to json
}

// Valid HTTP methods for httpcall and triggers
var ValidHTTPMethods = map[string]bool{
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"PATCH":   true,
	"DELETE":  true,
	"HEAD":    true,
	"OPTIONS": true,
	"":        true, // Default to GET for httpcall steps
}
