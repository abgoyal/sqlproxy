package tmpl

import (
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Context is the unified data available to all templates.
// All request data is accessed via the Trigger namespace for consistency
// with workflow templates: {{.trigger.client_ip}}, {{.trigger.params.X}}, etc.
type Context struct {
	// Unified trigger namespace (matches workflow context structure)
	Trigger *TriggerContext

	// Metadata (not request-specific)
	RequestID string
	Timestamp string
	Version   string

	// Query result (only for UsagePostQuery)
	Result *Result
}

// TriggerContext provides unified access to request data.
// Field names use snake_case to match workflow context conventions.
type TriggerContext struct {
	Params   map[string]any    `json:"params"`
	ClientIP string            `json:"client_ip"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Headers  map[string]string `json:"headers"`
	Query    map[string]string `json:"query"`
	Cookies  map[string]string `json:"cookies"`
}

// Result contains query execution results for post-query templates
type Result struct {
	Query      string
	Success    bool
	Count      int
	Data       []map[string]any
	Error      string
	DurationMs int64
}

// ContextBuilder creates Context from HTTP requests
type ContextBuilder struct {
	trustProxyHeaders bool
	version           string
}

// NewContextBuilder creates a builder with configuration
func NewContextBuilder(trustProxy bool, version string) *ContextBuilder {
	return &ContextBuilder{
		trustProxyHeaders: trustProxy,
		version:           version,
	}
}

// Build creates a Context from an HTTP request and parsed parameters
func (b *ContextBuilder) Build(r *http.Request, params map[string]any) *Context {
	if params == nil {
		params = make(map[string]any)
	}

	headers := make(map[string]string)
	for name := range r.Header {
		headers[name] = r.Header.Get(name)
	}

	query := make(map[string]string)
	for name := range r.URL.Query() {
		query[name] = r.URL.Query().Get(name)
	}

	cookies := make(map[string]string)
	for _, c := range r.Cookies() {
		cookies[c.Name] = c.Value
	}

	return &Context{
		Trigger: &TriggerContext{
			Params:   params,
			ClientIP: b.resolveClientIP(r),
			Method:   r.Method,
			Path:     r.URL.Path,
			Headers:  headers,
			Query:    query,
			Cookies:  cookies,
		},
		RequestID: b.getRequestID(r),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   b.version,
	}
}

// RateLimitData contains pre-parsed request data for rate limit evaluation.
type RateLimitData struct {
	ClientIP string
	Params   map[string]any
	Headers  map[string]string
	Query    map[string]string
	Cookies  map[string]string
}

// BuildForRateLimit creates a Context for rate limit key evaluation.
func (b *ContextBuilder) BuildForRateLimit(data *RateLimitData) *Context {
	params := data.Params
	if params == nil {
		params = make(map[string]any)
	}
	headers := data.Headers
	if headers == nil {
		headers = make(map[string]string)
	}
	query := data.Query
	if query == nil {
		query = make(map[string]string)
	}
	cookies := data.Cookies
	if cookies == nil {
		cookies = make(map[string]string)
	}

	return &Context{
		Trigger: &TriggerContext{
			Params:   params,
			ClientIP: data.ClientIP,
			Headers:  headers,
			Query:    query,
			Cookies:  cookies,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   b.version,
	}
}

// WithResult adds query result to context (for post-query templates)
func (c *Context) WithResult(r *Result) *Context {
	c.Result = r
	return c
}

// toMap converts Context to map for template execution.
// All request data is under the "trigger" namespace for consistency with workflow templates.
func (c *Context) toMap(usage Usage) map[string]any {
	trigger := map[string]any{}
	if c.Trigger != nil {
		trigger = map[string]any{
			"params":    c.Trigger.Params,
			"client_ip": c.Trigger.ClientIP,
			"method":    c.Trigger.Method,
			"path":      c.Trigger.Path,
			"headers":   c.Trigger.Headers,
			"query":     c.Trigger.Query,
			"cookies":   c.Trigger.Cookies,
		}
	}

	m := map[string]any{
		"trigger":   trigger,
		"RequestID": c.RequestID,
		"Timestamp": c.Timestamp,
		"Version":   c.Version,
	}

	if usage == UsagePostQuery && c.Result != nil {
		m["Result"] = map[string]any{
			"Query":      c.Result.Query,
			"Success":    c.Result.Success,
			"Count":      c.Result.Count,
			"Data":       c.Result.Data,
			"Error":      c.Result.Error,
			"DurationMs": c.Result.DurationMs,
		}
	}

	return m
}

func (b *ContextBuilder) resolveClientIP(r *http.Request) string {
	if b.trustProxyHeaders {
		// X-Forwarded-For: client, proxy1, proxy2
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.Index(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}

		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (b *ContextBuilder) getRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	if id := r.Header.Get("X-Correlation-ID"); id != "" {
		return id
	}
	return "" // Handler generates if empty
}

// ExtractParamRefs extracts {{.trigger.params.xyz}} references from a template string
func ExtractParamRefs(tmpl string) []string {
	re := regexp.MustCompile(`\{\{[^}]*\.trigger\.params\.([a-zA-Z_][a-zA-Z0-9_]*)[^}]*\}\}`)
	matches := re.FindAllStringSubmatch(tmpl, -1)

	refs := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			refs = append(refs, m[1])
			seen[m[1]] = true
		}
	}
	return refs
}

// ExtractHeaderRefs extracts {{.trigger.headers.xyz}} or {{require .trigger.headers "xyz"}} references
func ExtractHeaderRefs(tmpl string) []string {
	// Match both .trigger.headers.Name and .trigger.headers "Name" patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\.trigger\.headers\.([A-Za-z0-9-]+)`),
		regexp.MustCompile(`\.trigger\.headers\s+"([^"]+)"`),
	}

	refs := make([]string, 0)
	seen := make(map[string]bool)

	for _, re := range patterns {
		matches := re.FindAllStringSubmatch(tmpl, -1)
		for _, m := range matches {
			if len(m) > 1 && !seen[m[1]] {
				refs = append(refs, m[1])
				seen[m[1]] = true
			}
		}
	}
	return refs
}

// ExtractQueryRefs extracts {{.trigger.query.xyz}} or {{require .trigger.query "xyz"}} references
func ExtractQueryRefs(tmpl string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\.trigger\.query\.([a-zA-Z_][a-zA-Z0-9_]*)`),
		regexp.MustCompile(`\.trigger\.query\s+"([^"]+)"`),
	}

	refs := make([]string, 0)
	seen := make(map[string]bool)

	for _, re := range patterns {
		matches := re.FindAllStringSubmatch(tmpl, -1)
		for _, m := range matches {
			if len(m) > 1 && !seen[m[1]] {
				refs = append(refs, m[1])
				seen[m[1]] = true
			}
		}
	}
	return refs
}
