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
		return id
	}
	if id := r.Header.Get("X-Correlation-ID"); id != "" {
		return id
	}
	return generateRequestID()
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
				w.Header().Set("X-Cache-Key", cacheKey)
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
			w.Header().Set("X-Cache-Key", cacheKey)
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

	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	for _, p := range h.queryConfig.Parameters {
		value := r.FormValue(p.Name)

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

func convertValue(value, typeName string) (any, error) {
	switch strings.ToLower(typeName) {
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
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string, requestID string) {
	resp := Response{
		Success:   false,
		Error:     message,
		RequestID: requestID,
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
