package workflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"text/template"
	"time"

	"sql-proxy/internal/types"
	"sql-proxy/internal/workflow/step"
)

// TriggerCache provides caching for workflow trigger responses.
type TriggerCache interface {
	// Get retrieves cached response. Returns body, status code, and hit status.
	Get(workflow, key string) (body []byte, statusCode int, hit bool)
	// Set stores response in the cache with the given TTL.
	Set(workflow, key string, body []byte, statusCode int, ttl time.Duration) bool
}

// HTTPHandler handles HTTP requests for a workflow trigger.
type HTTPHandler struct {
	executor          *Executor
	workflow          *CompiledWorkflow
	trigger           *CompiledTrigger
	rateLimiter       RateLimiter
	cache             TriggerCache
	trustProxyHeaders bool
	version           string
	buildTime         string
	variables         map[string]string
}

// RateLimitContext contains all data available for rate limit key evaluation.
type RateLimitContext struct {
	ClientIP string
	Params   map[string]any
	Headers  map[string]string
	Query    map[string]string
	Cookies  map[string]string
}

// RateLimiter checks rate limits for workflow triggers.
type RateLimiter interface {
	// CheckTriggerLimits checks rate limits for a workflow trigger.
	// Returns (allowed, retryAfterSec, error).
	CheckTriggerLimits(limits []*CompiledRateLimit, ctx *RateLimitContext) (bool, int, error)
}

// NewHTTPHandler creates a handler for a workflow HTTP trigger.
func NewHTTPHandler(executor *Executor, wf *CompiledWorkflow, trigger *CompiledTrigger, rateLimiter RateLimiter, cache TriggerCache, trustProxyHeaders bool, version, buildTime string, variables map[string]string) *HTTPHandler {
	return &HTTPHandler{
		executor:          executor,
		workflow:          wf,
		trigger:           trigger,
		rateLimiter:       rateLimiter,
		cache:             cache,
		trustProxyHeaders: trustProxyHeaders,
		version:           version,
		buildTime:         buildTime,
		variables:         variables,
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := getOrGenerateRequestID(r)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	if h.version != "" {
		versionHeader := h.version
		if h.buildTime != "" && h.buildTime != "unknown" {
			versionHeader = fmt.Sprintf("%s (built %s)", h.version, h.buildTime)
		}
		w.Header().Set("X-Server-Version", versionHeader)
	}

	// Check method
	if r.Method != h.trigger.Config.Method {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", requestID)
		return
	}

	// Parse parameters
	params, err := h.parseParameters(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error(), requestID)
		return
	}

	// Parse cookies once for reuse in rate limits, cache key, and trigger data
	cookies := parseCookies(r)

	// Check rate limits
	clientIP := resolveClientIP(r, h.trustProxyHeaders)
	if h.rateLimiter != nil && len(h.trigger.RateLimits) > 0 {
		rlCtx := &RateLimitContext{
			ClientIP: clientIP,
			Params:   params,
			Headers:  flattenHeaders(r.Header),
			Query:    flattenQuery(r.URL.Query()),
			Cookies:  cookies,
		}
		allowed, retryAfterSec, err := h.rateLimiter.CheckTriggerLimits(h.trigger.RateLimits, rlCtx)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "rate limit check failed", requestID)
			return
		}
		if !allowed {
			h.writeRateLimitError(w, retryAfterSec, requestID)
			return
		}
	}

	// Check trigger-level cache
	var cacheKey string
	cacheEnabled := h.cache != nil && h.trigger.CacheKey != nil
	if cacheEnabled {
		var err error
		cacheKey, err = h.evaluateCacheKey(h.trigger.CacheKey, r, params, clientIP, cookies, requestID)
		if err != nil {
			// Log warning and continue without caching
			if h.executor != nil && h.executor.Logger() != nil {
				h.executor.Logger().Warn("trigger_cache_key_error", map[string]any{
					"workflow":   h.workflow.Config.Name,
					"error":      err.Error(),
					"request_id": requestID,
				})
			}
			cacheEnabled = false
		} else {
			// Check cache for hit
			if body, statusCode, hit := h.cache.Get(h.workflow.Config.Name, cacheKey); hit {
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(statusCode)
				w.Write(body)
				return
			}
			w.Header().Set("X-Cache", "MISS")
		}
	}

	// Build trigger data
	triggerData := &TriggerData{
		Type:     "http",
		Params:   params,
		Headers:  r.Header,
		Cookies:  cookies,
		ClientIP: clientIP,
		Method:   r.Method,
		Path:     r.URL.Path,
	}

	// Use response capture if caching is enabled
	var capture *responseCapture
	responseWriter := w
	if cacheEnabled {
		capture = &responseCapture{ResponseWriter: w}
		responseWriter = capture
	}

	// Execute workflow
	result := h.executor.Execute(r.Context(), h.workflow, triggerData, requestID, responseWriter, h.variables)

	// If workflow didn't send a response (no response step executed), send a default response
	if !result.ResponseSent {
		if result.Error != nil {
			h.writeError(responseWriter, http.StatusInternalServerError, "workflow execution failed", requestID)
		} else {
			// Send empty success response
			h.writeSuccess(responseWriter, nil, requestID)
		}
	}

	// Cache the response if caching is enabled and we have a successful response
	if cacheEnabled && capture != nil && capture.statusCode >= 200 && capture.statusCode < 400 {
		ttl := time.Duration(0)
		if h.trigger.Config.Cache != nil && h.trigger.Config.Cache.TTLSec > 0 {
			ttl = time.Duration(h.trigger.Config.Cache.TTLSec) * time.Second
		}
		h.cache.Set(h.workflow.Config.Name, cacheKey, capture.body.Bytes(), capture.statusCode, ttl)
	}
}

func (h *HTTPHandler) evaluateCacheKey(tmpl *template.Template, r *http.Request, params map[string]any, clientIP string, cookies map[string]string, requestID string) (string, error) {
	// Build trigger namespace matching response template context
	trigger := map[string]any{
		"params":    params,
		"client_ip": clientIP,
		"method":    r.Method,
		"path":      r.URL.Path,
		"headers":   flattenHeaders(r.Header),
		"query":     flattenQuery(r.URL.Query()),
		"cookies":   cookies,
	}
	data := map[string]any{
		"trigger":   trigger,
		"RequestID": requestID,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("evaluating trigger cache key: %w", err)
	}
	return buf.String(), nil
}

// flattenHeaders converts http.Header to a simple map, keeping only the first
// value for each header name. This is a deliberate simplification for template
// access in cache keys and rate limit contexts.
//
// Note: HTTP headers can have multiple values (e.g., multiple X-Forwarded-For
// entries). This function only returns the first value. For security-sensitive
// headers like X-Forwarded-For where all values matter for IP spoofing detection,
// use resolveClientIP which properly parses X-Forwarded-For format.
func flattenHeaders(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

// flattenQuery converts url.Values to a simple map, keeping only the first
// value for each query parameter. This is a deliberate simplification for
// template access in cache keys and rate limit contexts.
//
// Note: Query parameters can appear multiple times (e.g., ?id=1&id=2). This
// function only returns the first value. If you need all values for a
// parameter, access r.URL.Query() directly.
func flattenQuery(q map[string][]string) map[string]string {
	m := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

// responseCapture wraps http.ResponseWriter to capture the response for caching.
type responseCapture struct {
	http.ResponseWriter
	body        bytes.Buffer
	statusCode  int
	wroteHeader bool
}

func (rc *responseCapture) WriteHeader(code int) {
	if !rc.wroteHeader {
		rc.statusCode = code
		rc.wroteHeader = true
		rc.ResponseWriter.WriteHeader(code)
	}
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if !rc.wroteHeader {
		rc.statusCode = http.StatusOK
		rc.wroteHeader = true
	}
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}

func (h *HTTPHandler) parseParameters(r *http.Request) (map[string]any, error) {
	params := make(map[string]any)

	// Build parameter type map for validation
	paramTypes := make(map[string]string)
	for _, p := range h.trigger.Config.Parameters {
		paramTypes[p.Name] = strings.ToLower(p.Type)
	}

	// Extract path parameters from trigger config path
	pathParams := ExtractPathParams(h.trigger.Config.Path)

	// Parse JSON body if Content-Type is application/json
	var jsonParams map[string]any
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && r.Body != nil {
		var rawJSON map[string]any
		if err := json.NewDecoder(r.Body).Decode(&rawJSON); err != nil {
			return nil, fmt.Errorf("failed to parse JSON body: %w", err)
		}

		jsonParams = make(map[string]any)
		for k, v := range rawJSON {
			paramType := paramTypes[k]

			switch v.(type) {
			case map[string]any:
				if paramType != "json" {
					return nil, fmt.Errorf("nested objects not supported: parameter '%s' requires type 'json' for object values", k)
				}
			case []any:
				if paramType != "json" && !types.IsArrayType(paramType) {
					return nil, fmt.Errorf("arrays not supported: parameter '%s' requires type 'json' or array type (e.g., 'int[]') for array values", k)
				}
			}
			jsonParams[k] = v
		}
	}

	// Parse query string and form data
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	for _, p := range h.trigger.Config.Parameters {
		var value string

		// Check path parameters first (highest precedence)
		if pathParams[p.Name] {
			value = r.PathValue(p.Name)
		}

		// If not in path, check query string/form
		if value == "" {
			value = r.FormValue(p.Name)
		}

		// If not in query string, check JSON body
		if value == "" && jsonParams != nil {
			if jsonVal, ok := jsonParams[p.Name]; ok {
				converted, err := types.ConvertJSONValue(jsonVal, p.Type)
				if err != nil {
					return nil, fmt.Errorf("invalid value for parameter %s: %w", p.Name, err)
				}
				params[p.Name] = converted
				continue
			}
		}

		if value == "" {
			if p.Required {
				return nil, fmt.Errorf("missing required parameter: %s", p.Name)
			}
			// Use default value (even if it's empty string) for optional params
			value = p.Default
		}

		converted, err := types.ConvertValue(value, p.Type)
		if err != nil {
			return nil, fmt.Errorf("invalid value for parameter %s: %w", p.Name, err)
		}
		params[p.Name] = converted
	}

	return params, nil
}

type httpResponse struct {
	Success   bool   `json:"success"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func (h *HTTPHandler) writeSuccess(w http.ResponseWriter, data any, requestID string) {
	resp := httpResponse{
		Success:   true,
		Data:      data,
		RequestID: requestID,
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPHandler) writeError(w http.ResponseWriter, status int, message string, requestID string) {
	resp := httpResponse{
		Success:   false,
		Error:     message,
		RequestID: requestID,
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func (h *HTTPHandler) writeRateLimitError(w http.ResponseWriter, retryAfterSec int, requestID string) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSec))
	resp := rateLimitResponse{
		Success:       false,
		Error:         "rate limit exceeded",
		RequestID:     requestID,
		RetryAfterSec: retryAfterSec,
	}
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(resp)
}

type rateLimitResponse struct {
	Success       bool   `json:"success"`
	Error         string `json:"error"`
	RequestID     string `json:"request_id,omitempty"`
	RetryAfterSec int    `json:"retry_after_sec"`
}

func getOrGenerateRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return sanitizeHeaderValue(id)
	}
	if id := r.Header.Get("X-Correlation-ID"); id != "" {
		return sanitizeHeaderValue(id)
	}
	return generateRequestID()
}

func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func sanitizeHeaderValue(s string) string {
	const maxLen = 128
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7F {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func resolveClientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		// X-Forwarded-For: client, proxy1, proxy2
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	// Fall back to RemoteAddr - use net.SplitHostPort for proper IPv6 handling
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If SplitHostPort fails, return the address as-is
		return r.RemoteAddr
	}
	return host
}

// parseCookies extracts cookies into a map. For duplicate cookie names,
// the first occurrence wins (RFC 6265 compliant, matches Go's r.Cookie() behavior).
func parseCookies(r *http.Request) map[string]string {
	cookies := make(map[string]string)
	for _, c := range r.Cookies() {
		if _, exists := cookies[c.Name]; !exists {
			cookies[c.Name] = c.Value
		}
	}
	return cookies
}

// DBManagerAdapter adapts db.Manager to step.DBManager interface.
type DBManagerAdapter struct {
	queryFunc func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error)
}

// NewDBManagerAdapter creates a new adapter with a query function.
// The query function should be provided by the caller to avoid import cycles.
func NewDBManagerAdapter(queryFunc func(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error)) *DBManagerAdapter {
	return &DBManagerAdapter{queryFunc: queryFunc}
}

// ExecuteQuery implements step.DBManager.
func (a *DBManagerAdapter) ExecuteQuery(ctx context.Context, database, sql string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
	return a.queryFunc(ctx, database, sql, params, opts)
}

// LoggerAdapter adapts to the workflow.Logger interface.
type LoggerAdapter struct {
	logger LoggerInterface
}

// LoggerInterface defines the interface that logging package implements.
type LoggerInterface interface {
	Debug(msg string, fields map[string]any)
	Info(msg string, fields map[string]any)
	Warn(msg string, fields map[string]any)
	Error(msg string, fields map[string]any)
}

// NewLoggerAdapter creates a new logger adapter.
func NewLoggerAdapter(logger LoggerInterface) *LoggerAdapter {
	return &LoggerAdapter{logger: logger}
}

func (a *LoggerAdapter) Debug(msg string, fields map[string]any) {
	a.logger.Debug(msg, fields)
}

func (a *LoggerAdapter) Info(msg string, fields map[string]any) {
	a.logger.Info(msg, fields)
}

func (a *LoggerAdapter) Warn(msg string, fields map[string]any) {
	a.logger.Warn(msg, fields)
}

func (a *LoggerAdapter) Error(msg string, fields map[string]any) {
	a.logger.Error(msg, fields)
}
