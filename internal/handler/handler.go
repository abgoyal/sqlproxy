package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sql-proxy/internal/cache"
	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/logging"
	"sql-proxy/internal/metrics"
)

type Handler struct {
	dbManager         *db.Manager
	cache             *cache.Cache
	dbName            string // Which connection to use
	queryConfig       config.QueryConfig
	defaultTimeoutSec int
	maxTimeoutSec     int
	defaultCacheTTL   time.Duration
	version           string
	buildTime         string
}

type Response struct {
	Success    bool   `json:"success"`
	Data       any    `json:"data,omitempty"`
	Error      string `json:"error,omitempty"`
	Count      int    `json:"count,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}

func New(dbManager *db.Manager, c *cache.Cache, queryCfg config.QueryConfig, serverCfg config.ServerConfig) *Handler {
	defaultCacheTTL := 300 * time.Second
	if serverCfg.Cache != nil && serverCfg.Cache.DefaultTTLSec > 0 {
		defaultCacheTTL = time.Duration(serverCfg.Cache.DefaultTTLSec) * time.Second
	}

	return &Handler{
		dbManager:         dbManager,
		cache:             c,
		dbName:            queryCfg.Database,
		queryConfig:       queryCfg,
		defaultTimeoutSec: serverCfg.DefaultTimeoutSec,
		maxTimeoutSec:     serverCfg.MaxTimeoutSec,
		defaultCacheTTL:   defaultCacheTTL,
		version:           serverCfg.Version,
		buildTime:         serverCfg.BuildTime,
	}
}

// generateRequestID creates a short unique ID for request tracing
func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails (extremely rare)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// getOrGenerateRequestID uses caller's request ID if provided, otherwise generates one
func getOrGenerateRequestID(r *http.Request) string {
	// Check for caller-provided request ID (for end-to-end tracing)
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return sanitizeHeaderValue(id)
	}
	if id := r.Header.Get("X-Correlation-ID"); id != "" {
		return sanitizeHeaderValue(id)
	}
	return generateRequestID()
}

// sanitizeHeaderValue removes control characters and limits length to prevent
// log injection and header injection attacks
func sanitizeHeaderValue(s string) string {
	const maxLen = 128
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	// Remove control characters (0x00-0x1F, 0x7F) including newlines
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7F {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Start timing and get/generate request ID
	startTime := time.Now()
	requestID := getOrGenerateRequestID(r)

	// Initialize metrics record
	m := metrics.RequestMetrics{
		Endpoint:   h.queryConfig.Path,
		QueryName:  h.queryConfig.Name,
		StatusCode: http.StatusOK,
	}

	// Common log fields for wide events
	logFields := map[string]any{
		"request_id":  requestID,
		"endpoint":    h.queryConfig.Path,
		"query_name":  h.queryConfig.Name,
		"database":    h.dbName,
		"method":      r.Method,
		"remote_addr": r.RemoteAddr,
	}

	// Log request received
	logging.Debug("request_received", logFields)

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
	if r.Method != h.queryConfig.Method {
		m.StatusCode = http.StatusMethodNotAllowed
		m.Error = "method not allowed"
		m.TotalDuration = time.Since(startTime)
		h.finishRequest(w, m, logFields, http.StatusMethodNotAllowed, "method not allowed", requestID, 0)
		return
	}

	// Parse parameters
	parseStart := time.Now()
	params, err := h.parseParameters(r)
	parseDuration := time.Since(parseStart)

	if err != nil {
		m.StatusCode = http.StatusBadRequest
		m.Error = err.Error()
		m.TotalDuration = time.Since(startTime)
		logFields["parse_duration_ms"] = parseDuration.Milliseconds()
		logFields["error"] = err.Error()
		h.finishRequest(w, m, logFields, http.StatusBadRequest, err.Error(), requestID, 0)
		return
	}

	logFields["param_count"] = len(params)
	logFields["parse_duration_ms"] = parseDuration.Milliseconds()

	logging.Debug("params_parsed", logFields)

	// Resolve timeout
	timeoutSec := h.resolveTimeout(r)
	logFields["timeout_sec"] = timeoutSec

	// Check cache (if enabled and not bypassed)
	cacheEnabled := h.cache != nil && h.queryConfig.Cache != nil && h.queryConfig.Cache.Enabled
	noCache := r.URL.Query().Get("_nocache") == "1"
	var cacheKey string

	if cacheEnabled && !noCache {
		var keyErr error
		cacheKey, keyErr = cache.BuildKey(h.queryConfig.Cache.Key, params)
		if keyErr == nil {
			if cached, hit := h.cache.Get(h.queryConfig.Path, cacheKey); hit {
				m.TotalDuration = time.Since(startTime)
				m.RowCount = len(cached)
				m.CacheHit = true
				logFields["cache_hit"] = true
				logFields["cache_key"] = cacheKey
				logFields["row_count"] = len(cached)
				logFields["total_duration_ms"] = m.TotalDuration.Milliseconds()

				metrics.Record(m)
				logging.Info("request_completed", logFields)

				// Add cache headers
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("X-Cache-Key", sanitizeHeaderValue(cacheKey))
				if ttlRemaining := h.cache.GetTTLRemaining(h.queryConfig.Path, cacheKey); ttlRemaining > 0 {
					w.Header().Set("X-Cache-TTL", fmt.Sprintf("%d", int(ttlRemaining.Seconds())))
				}

				h.writeSuccess(w, cached, timeoutSec, requestID)
				return
			}
		}
		logFields["cache_hit"] = false
	}

	// Build query params
	queryParams := h.buildQueryParams(params)

	query := h.queryConfig.SQL

	// Get database connection
	database, err := h.dbManager.Get(h.dbName)
	if err != nil {
		m.StatusCode = http.StatusInternalServerError
		m.Error = err.Error()
		m.TotalDuration = time.Since(startTime)
		logFields["error"] = err.Error()
		h.finishRequest(w, m, logFields, http.StatusInternalServerError, "database connection unavailable", requestID, timeoutSec)
		return
	}

	// Resolve session config (query overrides > connection defaults > implicit defaults)
	sessionCfg := config.ResolveSessionConfig(database.Config(), h.queryConfig)
	logFields["isolation"] = sessionCfg.Isolation

	// Execute query
	logging.Debug("query_starting", logFields)
	queryStart := time.Now()

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	results, err := database.Query(ctx, sessionCfg, query, queryParams)
	queryDuration := time.Since(queryStart)
	m.QueryDuration = queryDuration

	logFields["query_duration_ms"] = queryDuration.Milliseconds()

	if err != nil {
		m.TotalDuration = time.Since(startTime)
		logFields["error"] = err.Error()

		if ctx.Err() == context.DeadlineExceeded {
			m.StatusCode = http.StatusGatewayTimeout
			m.Error = fmt.Sprintf("query timed out after %d seconds", timeoutSec)
			logging.Warn("query_timeout", logFields)
			h.finishRequest(w, m, logFields, http.StatusGatewayTimeout, m.Error, requestID, timeoutSec)
			return
		}

		m.StatusCode = http.StatusInternalServerError
		m.Error = "query execution failed"
		logging.Error("query_failed", logFields)
		h.finishRequest(w, m, logFields, http.StatusInternalServerError, "query execution failed", requestID, timeoutSec)
		return
	}

	// Parse JSON columns if configured
	if len(h.queryConfig.JSONColumns) > 0 {
		if err := parseJSONColumns(results, h.queryConfig.JSONColumns); err != nil {
			m.TotalDuration = time.Since(startTime)
			m.StatusCode = http.StatusInternalServerError
			m.Error = "failed to parse JSON columns"
			logFields["error"] = err.Error()
			logging.Error("json_column_parse_failed", logFields)
			h.finishRequest(w, m, logFields, http.StatusInternalServerError, "failed to parse JSON columns", requestID, timeoutSec)
			return
		}
	}

	// Store in cache if enabled
	if cacheEnabled && !noCache && cacheKey != "" {
		ttl := time.Duration(h.queryConfig.Cache.TTLSec) * time.Second
		if ttl == 0 {
			ttl = h.defaultCacheTTL
		}
		h.cache.Set(h.queryConfig.Path, cacheKey, results, ttl)
		logFields["cache_key"] = cacheKey
		logFields["cache_ttl_sec"] = int(ttl.Seconds())
	}

	m.RowCount = len(results)
	m.TotalDuration = time.Since(startTime)

	logFields["row_count"] = len(results)
	logFields["total_duration_ms"] = m.TotalDuration.Milliseconds()

	// Warn on slow queries (>80% of timeout)
	if queryDuration > time.Duration(timeoutSec*800)*time.Millisecond {
		logging.Warn("slow_query", logFields)
	}

	// Record metrics and send response
	metrics.Record(m)

	logging.Info("request_completed", logFields)

	// Add cache headers
	if cacheEnabled {
		if noCache {
			w.Header().Set("X-Cache", "BYPASS")
		} else if cacheKey != "" {
			w.Header().Set("X-Cache", "MISS")
			w.Header().Set("X-Cache-Key", sanitizeHeaderValue(cacheKey))
		}
	}

	h.writeSuccess(w, results, timeoutSec, requestID)
}

func (h *Handler) finishRequest(w http.ResponseWriter, m metrics.RequestMetrics, logFields map[string]any, status int, errMsg string, requestID string, timeoutSec int) {
	metrics.Record(m)

	logFields["status_code"] = status
	logFields["total_duration_ms"] = m.TotalDuration.Milliseconds()

	if status >= 500 {
		logging.Error("request_failed", logFields)
	} else if status >= 400 {
		logging.Warn("request_rejected", logFields)
	}

	h.writeError(w, status, errMsg, requestID)
}

func (h *Handler) resolveTimeout(r *http.Request) int {
	timeoutSec := h.defaultTimeoutSec

	if h.queryConfig.TimeoutSec > 0 {
		timeoutSec = h.queryConfig.TimeoutSec
	}

	if timeoutParam := r.URL.Query().Get("_timeout"); timeoutParam != "" {
		if parsed, err := strconv.Atoi(timeoutParam); err == nil && parsed > 0 {
			timeoutSec = parsed
		}
	}

	if timeoutSec > h.maxTimeoutSec {
		timeoutSec = h.maxTimeoutSec
	}

	if timeoutSec < 1 {
		timeoutSec = 1
	}

	return timeoutSec
}

func (h *Handler) parseParameters(r *http.Request) (map[string]any, error) {
	params := make(map[string]any)

	// Build a map of parameter names to their types for validation
	paramTypes := make(map[string]string)
	for _, p := range h.queryConfig.Parameters {
		paramTypes[p.Name] = strings.ToLower(p.Type)
	}

	// Parse JSON body if Content-Type is application/json
	var jsonParams map[string]any
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && r.Body != nil {
		var rawJSON map[string]any
		if err := json.NewDecoder(r.Body).Decode(&rawJSON); err != nil {
			return nil, fmt.Errorf("failed to parse JSON body: %w", err)
		}

		// Validate structure based on parameter types
		jsonParams = make(map[string]any)
		for k, v := range rawJSON {
			paramType := paramTypes[k]

			// Allow nested objects/arrays only for json and array types
			switch v.(type) {
			case map[string]any:
				if paramType != "json" {
					return nil, fmt.Errorf("nested objects not supported: parameter '%s' requires type 'json' for object values", k)
				}
			case []any:
				if paramType != "json" && !config.IsArrayType(paramType) {
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

	for _, p := range h.queryConfig.Parameters {
		// Check query string/form first (takes precedence)
		value := r.FormValue(p.Name)

		// If not in query string, check JSON body
		if value == "" && jsonParams != nil {
			if jsonVal, ok := jsonParams[p.Name]; ok {
				// Convert JSON value to appropriate type
				converted, err := convertJSONValue(jsonVal, p.Type)
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
			if p.Default != "" {
				value = p.Default
			} else {
				continue
			}
		}

		converted, err := convertValue(value, p.Type)
		if err != nil {
			return nil, fmt.Errorf("invalid value for parameter %s: %w", p.Name, err)
		}
		params[p.Name] = converted
	}

	return params, nil
}

// convertJSONValue converts a JSON-decoded value to the appropriate type.
// JSON numbers are float64, strings are strings, bools are bools.
// For json and array types, values are serialized to JSON strings.
func convertJSONValue(v any, typeName string) (any, error) {
	lowerType := strings.ToLower(typeName)

	// Handle json type - serialize any value to JSON string
	if lowerType == "json" {
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize JSON: %w", err)
		}
		return string(jsonBytes), nil
	}

	// Handle array types - validate elements and serialize to JSON array string
	if config.IsArrayType(lowerType) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected array, got %T", v)
		}
		baseType := config.ArrayBaseType(lowerType)
		validated, err := validateArrayElements(arr, baseType)
		if err != nil {
			return nil, err
		}
		jsonBytes, err := json.Marshal(validated)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize array: %w", err)
		}
		return string(jsonBytes), nil
	}

	switch lowerType {
	case "int", "integer":
		switch val := v.(type) {
		case float64:
			return int(val), nil
		case string:
			return strconv.Atoi(val)
		default:
			return nil, fmt.Errorf("expected integer, got %T", v)
		}
	case "float", "double":
		switch val := v.(type) {
		case float64:
			return val, nil
		case string:
			return strconv.ParseFloat(val, 64)
		default:
			return nil, fmt.Errorf("expected number, got %T", v)
		}
	case "bool", "boolean":
		switch val := v.(type) {
		case bool:
			return val, nil
		case string:
			return strconv.ParseBool(val)
		default:
			return nil, fmt.Errorf("expected boolean, got %T", v)
		}
	case "datetime", "date":
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string for datetime, got %T", v)
		}
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, strVal); err == nil {
				return t, nil
			}
		}
		return nil, fmt.Errorf("invalid datetime format")
	default: // string
		switch val := v.(type) {
		case string:
			return val, nil
		case float64:
			// JSON numbers for string params - convert to string
			return strconv.FormatFloat(val, 'f', -1, 64), nil
		case bool:
			return strconv.FormatBool(val), nil
		case nil:
			return "", nil
		default:
			return fmt.Sprintf("%v", v), nil
		}
	}
}

// validateArrayElements validates each element in an array matches the expected base type
func validateArrayElements(arr []any, baseType string) ([]any, error) {
	result := make([]any, len(arr))
	for i, elem := range arr {
		switch baseType {
		case "int", "integer":
			switch val := elem.(type) {
			case float64:
				result[i] = int(val)
			default:
				return nil, fmt.Errorf("array element %d: expected integer, got %T", i, elem)
			}
		case "float", "double":
			switch val := elem.(type) {
			case float64:
				result[i] = val
			default:
				return nil, fmt.Errorf("array element %d: expected number, got %T", i, elem)
			}
		case "bool", "boolean":
			switch val := elem.(type) {
			case bool:
				result[i] = val
			default:
				return nil, fmt.Errorf("array element %d: expected boolean, got %T", i, elem)
			}
		case "string":
			switch val := elem.(type) {
			case string:
				result[i] = val
			default:
				return nil, fmt.Errorf("array element %d: expected string, got %T", i, elem)
			}
		default:
			result[i] = elem
		}
	}
	return result, nil
}

func convertValue(value, typeName string) (any, error) {
	lowerType := strings.ToLower(typeName)

	// Handle json type - validate it's valid JSON and pass through as string
	if lowerType == "json" {
		// Validate it's valid JSON by parsing it
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		// Re-serialize for consistent formatting
		jsonBytes, err := json.Marshal(parsed)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize JSON: %w", err)
		}
		return string(jsonBytes), nil
	}

	// Handle array types - parse JSON array string, validate elements
	if config.IsArrayType(lowerType) {
		var arr []any
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return nil, fmt.Errorf("expected JSON array: %w", err)
		}
		baseType := config.ArrayBaseType(lowerType)
		validated, err := validateArrayElements(arr, baseType)
		if err != nil {
			return nil, err
		}
		// Re-serialize for consistent formatting
		jsonBytes, err := json.Marshal(validated)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize array: %w", err)
		}
		return string(jsonBytes), nil
	}

	switch lowerType {
	case "int", "integer":
		return strconv.Atoi(value)
	case "bool", "boolean":
		return strconv.ParseBool(value)
	case "datetime", "date":
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, value); err == nil {
				return t, nil
			}
		}
		return nil, fmt.Errorf("invalid datetime format")
	case "float", "double":
		return strconv.ParseFloat(value, 64)
	default:
		return value, nil
	}
}

// buildQueryParams builds the parameter map for the query.
// The driver handles parameter translation to its native syntax.
func (h *Handler) buildQueryParams(params map[string]any) map[string]any {
	// The params map from parseParameters already has all provided values.
	// For optional params not provided, the driver will pass nil.
	// We just return what was parsed.
	return params
}

func (h *Handler) writeSuccess(w http.ResponseWriter, data []map[string]any, timeoutSec int, requestID string) {
	resp := Response{
		Success:    true,
		Data:       data,
		Count:      len(data),
		TimeoutSec: timeoutSec,
		RequestID:  requestID,
	}
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logging.Error("response_encode_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string, requestID string) {
	resp := Response{
		Success:   false,
		Error:     message,
		RequestID: requestID,
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logging.Error("response_encode_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
	}
}

// parseJSONColumns parses specified columns from strings to JSON objects in-place.
// If a column value is a string containing valid JSON, it's replaced with the parsed value.
// If the column doesn't exist or isn't a string, it's silently skipped.
// Returns an error only if JSON parsing fails for a string value.
func parseJSONColumns(results []map[string]any, columns []string) error {
	// Build a set for O(1) lookup
	colSet := make(map[string]struct{}, len(columns))
	for _, col := range columns {
		colSet[col] = struct{}{}
	}

	for _, row := range results {
		for col := range colSet {
			val, exists := row[col]
			if !exists {
				continue
			}

			// Only parse string values
			strVal, ok := val.(string)
			if !ok {
				continue
			}

			// Skip empty strings
			if strVal == "" {
				continue
			}

			// Try to parse as JSON
			var parsed any
			if err := json.Unmarshal([]byte(strVal), &parsed); err != nil {
				return fmt.Errorf("column '%s': invalid JSON: %w", col, err)
			}
			row[col] = parsed
		}
	}
	return nil
}
