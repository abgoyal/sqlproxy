package tmpl

import (
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Context is the unified data available to all templates
type Context struct {
	// Request metadata (always available)
	ClientIP  string
	Method    string
	Path      string
	RequestID string
	Timestamp string
	Version   string

	// Request data as maps (for strict missing key handling)
	Header map[string]string // Flattened headers
	Query  map[string]string // Flattened query params
	Param  map[string]any    // Typed parameters from handler

	// Query result (only for UsagePostQuery)
	Result *Result
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
	ctx := &Context{
		ClientIP:  b.resolveClientIP(r),
		Method:    r.Method,
		Path:      r.URL.Path,
		RequestID: b.getRequestID(r),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   b.version,
		Header:    make(map[string]string),
		Query:     make(map[string]string),
		Param:     params,
	}

	if ctx.Param == nil {
		ctx.Param = make(map[string]any)
	}

	// Flatten headers (use canonical names)
	for name := range r.Header {
		ctx.Header[name] = r.Header.Get(name)
	}

	// Flatten query params
	for name := range r.URL.Query() {
		ctx.Query[name] = r.URL.Query().Get(name)
	}

	return ctx
}

// WithResult adds query result to context (for post-query templates)
func (c *Context) WithResult(r *Result) *Context {
	c.Result = r
	return c
}

// toMap converts Context to map for template execution
func (c *Context) toMap(usage Usage) map[string]any {
	m := map[string]any{
		"ClientIP":  c.ClientIP,
		"Method":    c.Method,
		"Path":      c.Path,
		"RequestID": c.RequestID,
		"Timestamp": c.Timestamp,
		"Version":   c.Version,
		"Header":    c.Header,
		"Query":     c.Query,
		"Param":     c.Param,
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

// ExtractParamRefs extracts {{.Param.xyz}} references from a template string
func ExtractParamRefs(tmpl string) []string {
	re := regexp.MustCompile(`\{\{[^}]*\.Param\.([a-zA-Z_][a-zA-Z0-9_]*)[^}]*\}\}`)
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

// ExtractHeaderRefs extracts {{.Header.xyz}} or {{require .Header "xyz"}} references
func ExtractHeaderRefs(tmpl string) []string {
	// Match both .Header.Name and .Header "Name" patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\.Header\.([A-Za-z0-9-]+)`),
		regexp.MustCompile(`\.Header\s+"([^"]+)"`),
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

// ExtractQueryRefs extracts {{.Query.xyz}} or {{require .Query "xyz"}} references
func ExtractQueryRefs(tmpl string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\.Query\.([a-zA-Z_][a-zA-Z0-9_]*)`),
		regexp.MustCompile(`\.Query\s+"([^"]+)"`),
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
