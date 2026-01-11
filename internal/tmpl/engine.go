// Package tmpl provides a unified template engine for cache keys, rate limit keys,
// and webhook payloads. All templates use the same context variables and functions.
package tmpl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// Usage indicates what context is available for a template
type Usage int

const (
	// UsagePreQuery is for templates evaluated before query execution (cache keys, rate limits)
	UsagePreQuery Usage = iota
	// UsagePostQuery is for templates evaluated after query execution (webhooks)
	UsagePostQuery
)

// Engine manages compiled templates with consistent context and functions
type Engine struct {
	mu        sync.RWMutex
	templates map[string]*compiledTemplate
	funcs     template.FuncMap
}

type compiledTemplate struct {
	tmpl  *template.Template
	usage Usage
}

// New creates a new template engine with all standard functions
func New() *Engine {
	e := &Engine{
		templates: make(map[string]*compiledTemplate),
	}
	e.funcs = template.FuncMap{
		// Strict access - errors if key missing or empty
		"require": requireFunc,

		// Optional access with explicit fallback
		"getOr": getOrFunc,

		// Check if key exists and is non-empty
		"has": hasFunc,

		// JSON serialization
		"json":       jsonFunc,
		"jsonIndent": jsonIndentFunc,

		// Math
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"mod": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a % b
		},

		// String manipulation
		"upper":     strings.ToUpper,
		"lower":     strings.ToLower,
		"trim":      strings.TrimSpace,
		"replace":   strings.ReplaceAll,
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,

		// Default value (for direct field access, not map access)
		"default": defaultFunc,

		// Coalesce - return first non-empty value
		"coalesce": coalesceFunc,
	}
	return e
}

// requireFunc returns value from map, errors if missing or empty
func requireFunc(m map[string]string, key string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("required key %q: nil map", key)
	}
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("required key %q not found", key)
	}
	if v == "" {
		return "", fmt.Errorf("required key %q is empty", key)
	}
	return v, nil
}

// getOrFunc returns value from map, or fallback if missing/empty
func getOrFunc(m map[string]string, key, fallback string) string {
	if m == nil {
		return fallback
	}
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return fallback
}

// hasFunc checks if key exists and is non-empty
func hasFunc(m map[string]string, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	return ok && v != ""
}

// jsonFunc serializes value to JSON
func jsonFunc(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("[json error: %v]", err)
	}
	return string(b)
}

// jsonIndentFunc serializes value to indented JSON
func jsonIndentFunc(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("[json error: %v]", err)
	}
	return string(b)
}

// defaultFunc returns def if val is empty/zero, otherwise val
func defaultFunc(def, val any) any {
	if val == nil {
		return def
	}
	switch v := val.(type) {
	case string:
		if v == "" {
			return def
		}
	case int:
		if v == 0 {
			return def
		}
	case int64:
		if v == 0 {
			return def
		}
	case float64:
		if v == 0 {
			return def
		}
	case bool:
		// false is a valid value, not "empty"
	}
	return val
}

// coalesceFunc returns first non-empty string
func coalesceFunc(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Register compiles and stores a named template
func (e *Engine) Register(name, tmplStr string, usage Usage) error {
	if tmplStr == "" {
		return fmt.Errorf("template %q: empty template", name)
	}

	t, err := template.New(name).
		Funcs(e.funcs).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template %q: %w", name, err)
	}

	e.mu.Lock()
	e.templates[name] = &compiledTemplate{
		tmpl:  t,
		usage: usage,
	}
	e.mu.Unlock()
	return nil
}

// Execute runs a registered template with the given context
func (e *Engine) Execute(name string, ctx *Context) (string, error) {
	e.mu.RLock()
	ct, ok := e.templates[name]
	e.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("template %q not registered", name)
	}

	data := ctx.toMap(ct.usage)

	var buf bytes.Buffer
	if err := ct.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	result := buf.String()
	if result == "" {
		return "", fmt.Errorf("template %q produced empty result", name)
	}

	return result, nil
}

// ExecuteInline compiles and executes a template string directly (not cached)
func (e *Engine) ExecuteInline(tmplStr string, ctx *Context, usage Usage) (string, error) {
	if tmplStr == "" {
		return "", fmt.Errorf("empty template")
	}

	t, err := template.New("inline").
		Funcs(e.funcs).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return "", err
	}

	data := ctx.toMap(usage)

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	result := buf.String()
	if result == "" {
		return "", fmt.Errorf("template produced empty result")
	}

	return result, nil
}

// Validate checks a template string without executing it
func (e *Engine) Validate(tmplStr string, usage Usage) error {
	if tmplStr == "" {
		return fmt.Errorf("empty template")
	}

	// Parse
	t, err := template.New("validate").
		Funcs(e.funcs).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Check for invalid usage (e.g., accessing Result in pre-query template)
	if usage == UsagePreQuery && strings.Contains(tmplStr, ".Result") {
		return fmt.Errorf("pre-query templates cannot access .Result")
	}

	// Execute with sample data to catch structural errors
	sample := sampleContextMap(usage)
	var buf bytes.Buffer
	if err := t.Execute(&buf, sample); err != nil {
		// Check if error is about missing map key - that's expected for templates
		// that reference headers/query params we don't have in sample
		errStr := err.Error()
		if strings.Contains(errStr, "map has no entry") ||
			strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "is empty") {
			// This is OK for Header/Query access - runtime will validate
			return nil
		}
		return fmt.Errorf("execution error: %w", err)
	}

	return nil
}

// ValidateWithParams validates a template and checks that Param references exist
func (e *Engine) ValidateWithParams(tmplStr string, usage Usage, paramNames []string) error {
	if err := e.Validate(tmplStr, usage); err != nil {
		return err
	}

	// Check that Param references exist in paramNames
	paramRefs := ExtractParamRefs(tmplStr)
	paramSet := make(map[string]bool)
	for _, p := range paramNames {
		paramSet[p] = true
	}

	for _, ref := range paramRefs {
		if !paramSet[ref] {
			return fmt.Errorf("template references .Param.%s but no such parameter defined", ref)
		}
	}

	return nil
}

// sampleContextMap returns sample data for validation
func sampleContextMap(usage Usage) map[string]any {
	m := map[string]any{
		"ClientIP":  "192.168.1.1",
		"Method":    "GET",
		"Path":      "/api/sample",
		"RequestID": "sample-request-id",
		"Timestamp": "2024-01-01T00:00:00Z",
		"Version":   "1.0.0",
		"Header":    map[string]string{},
		"Query":     map[string]string{},
		"Param":     map[string]any{},
	}

	if usage == UsagePostQuery {
		m["Result"] = map[string]any{
			"Query":      "sample_query",
			"Success":    true,
			"Count":      1,
			"Data":       []map[string]any{{"id": 1}},
			"Error":      "",
			"DurationMs": int64(10),
		}
	}

	return m
}
