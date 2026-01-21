package server

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron/v3"

	"sql-proxy/internal/cache"
	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/logging"
	"sql-proxy/internal/metrics"
	"sql-proxy/internal/openapi"
	"sql-proxy/internal/publicid"
	"sql-proxy/internal/ratelimit"
	"sql-proxy/internal/tmpl"
	"sql-proxy/internal/workflow"
	"sql-proxy/internal/workflow/step"
)

const (
	// healthCheckInterval is how often to check database connectivity
	healthCheckInterval = 30 * time.Second

	// healthCheckTimeout is the timeout for each health check ping
	healthCheckTimeout = 5 * time.Second

	// healthCheckFailuresBeforeReconnect is how many consecutive failures before attempting reconnect
	healthCheckFailuresBeforeReconnect = 3

	// httpReadTimeout is the timeout for reading the entire request
	httpReadTimeout = 15 * time.Second

	// httpIdleTimeout is how long to keep idle connections open
	httpIdleTimeout = 60 * time.Second

	// writeTimeoutBuffer is added to max query timeout for HTTP write timeout
	writeTimeoutBuffer = 30 * time.Second

	// maxRequestBodySize is the maximum allowed request body size (1MB)
	maxRequestBodySize = 1 << 20
)

// fallbackIDCounter provides unique IDs when crypto/rand fails
var fallbackIDCounter atomic.Uint64

type Server struct {
	httpServer  *http.Server
	debugServer *http.Server // Separate debug server (pprof) if configured on different port
	dbManager   *db.Manager
	cache       *cache.Cache
	rateLimiter *ratelimit.Limiter
	ctxBuilder  *tmpl.ContextBuilder
	config      *config.Config
	createdAt   time.Time

	// Health tracking (all DBs healthy)
	dbHealthy     atomic.Bool
	healthChecker context.CancelFunc

	// Cron job scheduler for workflow triggers
	cron       *cron.Cron
	cronCtx    context.Context    // Context for cron job execution
	cronCancel context.CancelFunc // Cancel function for graceful shutdown

	// Workflow execution (written once during initialization, read-only after)
	workflowExecutor *workflow.Executor
	workflows        []*workflow.CompiledWorkflow
}

// Response types for JSON encoding
type healthResponse struct {
	Status    string            `json:"status"`
	Databases map[string]string `json:"databases"`
	Uptime    string            `json:"uptime"`
}

type logLevelResponse struct {
	Status string `json:"status,omitempty"`
	Level  string `json:"level,omitempty"`
	// For GET request
	CurrentLevel string `json:"current_level,omitempty"`
	Usage        string `json:"usage,omitempty"`
}

type cacheClearResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Endpoint string `json:"endpoint,omitempty"`
}

type dbHealthResponse struct {
	Database string `json:"database"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	ReadOnly bool   `json:"readonly"`
}

type errorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// writeJSON encodes v as JSON to w and logs any encoding errors
func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logging.Error("json_encode_failed", map[string]any{
			"error": err.Error(),
			"type":  fmt.Sprintf("%T", v),
		})
	}
}

func New(cfg *config.Config, interactive bool) (*Server, error) {
	// Initialize logging
	// Interactive: stdout, Service: file
	logFile := ""
	if !interactive {
		logFile = cfg.Logging.FilePath
	}
	if err := logging.Init(cfg.Logging.Level, logFile, cfg.Logging.MaxSizeMB, cfg.Logging.MaxBackups, cfg.Logging.MaxAgeDays); err != nil {
		return nil, fmt.Errorf("failed to initialize logging: %w", err)
	}

	logging.Info("service_starting", map[string]any{
		"version":   cfg.Server.Version,
		"log_level": cfg.Logging.Level,
		"workflows": len(cfg.Workflows),
		"databases": len(cfg.Databases),
	})

	// Connect to all databases
	dbManager, err := db.NewManager(cfg.Databases)
	if err != nil {
		logging.Error("database_connection_failed", map[string]any{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to connect to databases: %w", err)
	}

	for _, dbCfg := range cfg.Databases {
		logFields := map[string]any{
			"name":     dbCfg.Name,
			"type":     dbCfg.Type,
			"readonly": dbCfg.IsReadOnly(),
		}
		if dbCfg.Type == "sqlite" {
			logFields["path"] = dbCfg.Path
		} else {
			logFields["host"] = dbCfg.Host
			logFields["database"] = dbCfg.Database
		}
		logging.Info("database_connected", logFields)
	}

	s := &Server{
		dbManager: dbManager,
		config:    cfg,
		createdAt: time.Now(),
	}
	s.dbHealthy.Store(true)

	// Initialize cache if enabled
	if cfg.Server.Cache != nil && cfg.Server.Cache.Enabled {
		var err error
		s.cache, err = cache.New(*cfg.Server.Cache)
		if err != nil {
			logging.Error("cache_init_failed", map[string]any{
				"error": err.Error(),
			})
			return nil, fmt.Errorf("failed to initialize cache: %w", err)
		}
		logging.Info("cache_initialized", map[string]any{
			"max_size_mb":     cfg.Server.Cache.MaxSizeMB,
			"default_ttl_sec": cfg.Server.Cache.DefaultTTLSec,
		})
	}

	// Initialize template engine and context builder (for rate limiting, webhooks, etc.)
	tmplEngine := tmpl.New()
	s.ctxBuilder = tmpl.NewContextBuilder(cfg.Server.TrustProxyHeaders, cfg.Server.Version)

	// Initialize public ID encoder if configured
	if cfg.PublicIDs != nil && cfg.PublicIDs.SecretKey != "" {
		enc, err := publicid.NewEncoder(cfg.PublicIDs.SecretKey, cfg.PublicIDs.Namespaces)
		if err != nil {
			logging.Error("public_id_encoder_init_failed", map[string]any{
				"error": err.Error(),
			})
			return nil, fmt.Errorf("failed to initialize public ID encoder: %w", err)
		}
		tmplEngine.SetPublicIDEncoder(enc)
		workflow.SetTemplateEncoder(enc)
		logging.Info("public_id_encoder_initialized", map[string]any{
			"namespaces": len(cfg.PublicIDs.Namespaces),
		})
	}

	// Initialize rate limiter if pools are configured
	if len(cfg.RateLimits) > 0 {
		var err error
		s.rateLimiter, err = ratelimit.New(cfg.RateLimits, tmplEngine)
		if err != nil {
			logging.Error("rate_limiter_init_failed", map[string]any{
				"error": err.Error(),
			})
			return nil, fmt.Errorf("failed to initialize rate limiter: %w", err)
		}
		logging.Info("rate_limiter_initialized", map[string]any{
			"pools": len(cfg.RateLimits),
		})
	}

	// Initialize metrics
	if cfg.Metrics.Enabled {
		metrics.Init(s.checkDBHealth, cfg.Server.Version, cfg.Server.BuildTime)
		// Set cache snapshot provider for metrics
		if s.cache != nil {
			metrics.SetCacheSnapshotProvider(func() any {
				return s.cache.GetSnapshot()
			})
		}
		// Set rate limit snapshot provider for metrics
		if s.rateLimiter != nil {
			metrics.SetRateLimitSnapshotProvider(func() any {
				return s.rateLimiter.Snapshot()
			})
		}
		logging.Info("metrics_initialized", nil)
	}

	// Initialize workflows if configured
	if len(cfg.Workflows) > 0 {
		if err := s.initWorkflows(cfg); err != nil {
			return nil, err
		}

		// Add workflow cron triggers to scheduler
		if err := s.addWorkflowCronJobs(); err != nil {
			return nil, err
		}
	}

	// Start background health checker
	healthCtx, healthCancel := context.WithCancel(context.Background())
	s.healthChecker = healthCancel
	go s.runHealthChecker(healthCtx)

	// Setup routes
	mux := http.NewServeMux()
	s.setupRoutes(mux)

	// Calculate write timeout based on max query timeout + buffer
	writeTimeout := time.Duration(cfg.Server.MaxTimeoutSec)*time.Second + writeTimeoutBuffer

	// Middleware chain: recovery -> bodyLimit -> gzip -> routes
	handler := s.recoveryMiddleware(s.bodySizeLimitMiddleware(s.gzipMiddleware(mux)))

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	// Setup debug server (pprof) if enabled
	if cfg.Debug.Enabled {
		debugPort := cfg.Debug.Port

		if debugPort == 0 || debugPort == cfg.Server.Port {
			// Same port as main server - add pprof routes to main mux
			// Note: debug.host is not allowed in this case (caught by config validation)
			mux.HandleFunc("/_/debug/pprof/", pprof.Index)
			mux.HandleFunc("/_/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/_/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/_/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/_/debug/pprof/trace", pprof.Trace)

			logging.Info("debug_endpoints_enabled", map[string]any{
				"host": cfg.Server.Host,
				"port": cfg.Server.Port,
				"path": "/_/debug/pprof/",
			})
		} else {
			// Separate port for debug server - host setting applies
			debugHost := cfg.Debug.Host
			if debugHost == "" {
				debugHost = "localhost" // Default to localhost for security
			}

			debugMux := http.NewServeMux()
			debugMux.HandleFunc("/_/debug/pprof/", pprof.Index)
			debugMux.HandleFunc("/_/debug/pprof/cmdline", pprof.Cmdline)
			debugMux.HandleFunc("/_/debug/pprof/profile", pprof.Profile)
			debugMux.HandleFunc("/_/debug/pprof/symbol", pprof.Symbol)
			debugMux.HandleFunc("/_/debug/pprof/trace", pprof.Trace)

			s.debugServer = &http.Server{
				Addr:         fmt.Sprintf("%s:%d", debugHost, debugPort),
				Handler:      debugMux,
				ReadTimeout:  httpReadTimeout,
				WriteTimeout: 60 * time.Second, // Longer for profiling
				IdleTimeout:  httpIdleTimeout,
			}

			logging.Info("debug_server_configured", map[string]any{
				"host": debugHost,
				"port": debugPort,
				"path": "/_/debug/pprof/",
			})
		}
	}

	return s, nil
}

// runHealthChecker periodically checks database connectivity for all connections
func (s *Server) runHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	// Track consecutive failures per database
	consecutiveFailures := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
			results := s.dbManager.Ping(pingCtx)
			cancel()

			wasHealthy := s.dbHealthy.Load()
			allHealthy := true

			for name, err := range results {
				if err != nil {
					allHealthy = false
					consecutiveFailures[name]++

					logging.Warn("health_check_failed", map[string]any{
						"database":             name,
						"error":                err.Error(),
						"consecutive_failures": consecutiveFailures[name],
					})

					// After consecutive failures, try to reconnect
					if consecutiveFailures[name] >= healthCheckFailuresBeforeReconnect {
						logging.Info("attempting_reconnect", map[string]any{
							"database": name,
						})
						if err := s.dbManager.Reconnect(name); err != nil {
							logging.Error("reconnect_failed", map[string]any{
								"database": name,
								"error":    err.Error(),
							})
						} else {
							logging.Info("reconnect_successful", map[string]any{
								"database": name,
							})
							consecutiveFailures[name] = 0
						}
					}
				} else {
					if consecutiveFailures[name] > 0 {
						logging.Info("health_restored", map[string]any{
							"database":       name,
							"after_failures": consecutiveFailures[name],
						})
					}
					consecutiveFailures[name] = 0
				}
			}

			s.dbHealthy.Store(allHealthy)

			if allHealthy && !wasHealthy {
				logging.Info("all_databases_healthy", nil)
			}
		}
	}
}

// checkDBHealth returns current DB health status (for metrics)
func (s *Server) checkDBHealth() bool {
	return s.dbHealthy.Load()
}

func (s *Server) setupRoutes(mux *http.ServeMux) {
	// Internal endpoints (/_/ prefix is reserved, user queries cannot use it)
	// Health check endpoints
	mux.HandleFunc("/_/health", s.healthHandler)    // Aggregate health
	mux.HandleFunc("/_/health/", s.dbHealthHandler) // Per-database health: /_/health/{dbname}

	// Metrics endpoints
	mux.HandleFunc("/_/metrics.json", s.metricsJSONHandler)  // Human-readable JSON metrics
	mux.HandleFunc("/_/metrics", s.metricsPrometheusHandler) // Prometheus format

	// OpenAPI spec endpoint
	mux.HandleFunc("/_/openapi.json", s.openAPIHandler)

	// Runtime config endpoint
	mux.HandleFunc("/_/config/loglevel", s.logLevelHandler)

	// Cache management endpoint
	mux.HandleFunc("/_/cache/clear", s.cacheClearHandler)

	// Rate limit observability and management endpoints
	mux.HandleFunc("/_/ratelimits", s.rateLimitsHandler)
	mux.HandleFunc("/_/ratelimits/reset", s.rateLimitsResetHandler)

	// List available endpoints
	mux.HandleFunc("/", s.listEndpointsHandler)

	// Create rate limiter adapter for workflows
	var rateLimiterAdapter workflow.RateLimiter
	if s.rateLimiter != nil {
		rateLimiterAdapter = &workflowRateLimiterAdapter{
			limiter:    s.rateLimiter,
			ctxBuilder: s.ctxBuilder,
		}
	}

	// Create trigger cache adapter for workflows
	var triggerCache workflow.TriggerCache
	if s.cache != nil {
		triggerCache = &triggerCacheAdapter{cache: s.cache}
	}

	// Register workflow HTTP triggers
	for _, wf := range s.workflows {
		for _, trigger := range wf.Triggers {
			if trigger.Config.Type != "http" {
				continue
			}

			h := workflow.NewHTTPHandler(
				s.workflowExecutor,
				wf,
				trigger,
				rateLimiterAdapter,
				triggerCache,
				s.config.Server.TrustProxyHeaders,
				s.config.Server.Version,
				s.config.Server.BuildTime,
				s.config.Variables.Values,
			)
			// Register with method prefix for Go 1.22+ routing (e.g., "GET /api/items")
			pattern := trigger.Config.Method + " " + trigger.Config.Path
			mux.Handle(pattern, h)

			logging.Info("workflow_endpoint_registered", map[string]any{
				"workflow": wf.Config.Name,
				"method":   trigger.Config.Method,
				"path":     trigger.Config.Path,
			})
		}
	}
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check each database individually
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()
	dbResults := s.dbManager.Ping(ctx)

	// Build per-database status and count healthy/unhealthy
	databases := make(map[string]string)
	healthyCount := 0
	totalCount := 0
	for name, err := range dbResults {
		totalCount++
		if err != nil {
			databases[name] = "disconnected"
		} else {
			databases[name] = "connected"
			healthyCount++
		}
	}

	// Determine overall status:
	// - "healthy" = all DBs connected
	// - "degraded" = some DBs connected, some disconnected
	// - "unhealthy" = all DBs disconnected
	status := "healthy"
	if healthyCount == 0 && totalCount > 0 {
		status = "unhealthy"
	} else if healthyCount < totalCount {
		status = "degraded"
	}

	// Always return 200 - clients should parse the status field
	// This avoids confusion with proxy/middleware 503s
	writeJSON(w, healthResponse{
		Status:    status,
		Databases: databases,
		Uptime:    time.Since(s.startTime()).String(),
	})
}

// dbHealthHandler handles per-database health checks: /_/health/{dbname}
func (s *Server) dbHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract database name from path: /_/health/{dbname}
	dbName := strings.TrimPrefix(r.URL.Path, "/_/health/")
	if dbName == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, errorResponse{
			Error: "database name required: /_/health/{dbname}",
		})
		return
	}

	// Get the database driver
	driver, err := s.dbManager.Get(dbName)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, errorResponse{
			Error: "database not found: " + dbName,
		})
		return
	}

	// Check connectivity
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	status := "connected"
	if err := driver.Ping(ctx); err != nil {
		status = "disconnected"
	}

	// Always return 200 - clients should parse the status field
	// Only 404 is returned for unknown database names
	writeJSON(w, dbHealthResponse{
		Database: dbName,
		Status:   status,
		Type:     driver.Type(),
		ReadOnly: driver.IsReadOnly(),
	})
}

// metricsJSONHandler returns metrics in human-readable JSON format
func (s *Server) metricsJSONHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	snap := metrics.GetSnapshot()
	if snap == nil {
		writeJSON(w, errorResponse{
			Error: "metrics not enabled",
		})
		return
	}

	writeJSON(w, snap)
}

// metricsPrometheusHandler returns metrics in Prometheus/OpenMetrics format
func (s *Server) metricsPrometheusHandler(w http.ResponseWriter, r *http.Request) {
	registry := metrics.Registry()
	if registry == nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("# metrics not enabled\n"))
		return
	}

	// Use promhttp handler with our custom registry
	// DisableCompression: true because our gzip middleware handles compression
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics:  true,
		DisableCompression: true,
	}).ServeHTTP(w, r)
}

func (s *Server) openAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // Allow Swagger UI from anywhere

	spec := openapi.Spec(s.config)
	writeJSON(w, spec)
}

func (s *Server) logLevelHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		level := r.URL.Query().Get("level")
		if level == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, errorResponse{
				Error: "level parameter required (debug, info, warn, error)",
			})
			return
		}

		logging.SetLevel(level)
		logging.Info("log_level_changed", map[string]any{
			"new_level": level,
		})

		writeJSON(w, logLevelResponse{
			Status: "ok",
			Level:  level,
		})
		return
	}

	writeJSON(w, logLevelResponse{
		CurrentLevel: logging.GetLevel(),
		Usage:        "POST /_/config/loglevel?level=debug|info|warn|error",
	})
}

func (s *Server) cacheClearHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Only allow POST/DELETE methods
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, errorResponse{
			Error: "method not allowed, use POST or DELETE",
		})
		return
	}

	// Check if cache is enabled
	if s.cache == nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, errorResponse{
			Error: "cache not enabled",
		})
		return
	}

	// Check for endpoint parameter (optional)
	endpoint := r.URL.Query().Get("endpoint")

	if endpoint != "" {
		// Clear specific endpoint
		s.cache.Clear(endpoint)
		logging.Info("cache_cleared", map[string]any{
			"endpoint": endpoint,
		})
		writeJSON(w, cacheClearResponse{
			Status:   "ok",
			Message:  "cache cleared for endpoint",
			Endpoint: endpoint,
		})
	} else {
		// Clear all cache
		s.cache.ClearAll()
		logging.Info("cache_cleared_all", nil)
		writeJSON(w, cacheClearResponse{
			Status:  "ok",
			Message: "all cache cleared",
		})
	}
}

// rateLimitsHandler returns rate limit pool status and metrics
func (s *Server) rateLimitsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.rateLimiter == nil {
		writeJSON(w, errorResponse{
			Error: "rate limiting not configured",
		})
		return
	}

	// Get detailed rate limit snapshot
	snapshot := s.rateLimiter.Snapshot()

	// Add pool configuration info
	type poolInfo struct {
		Name              string `json:"name"`
		RequestsPerSecond int    `json:"requests_per_second"`
		Burst             int    `json:"burst"`
		Allowed           int64  `json:"allowed"`
		Denied            int64  `json:"denied"`
		ActiveBuckets     int64  `json:"active_buckets"`
	}

	type rateLimitsResponse struct {
		Enabled      bool        `json:"enabled"`
		TotalAllowed int64       `json:"total_allowed"`
		TotalDenied  int64       `json:"total_denied"`
		Pools        []*poolInfo `json:"pools"`
	}

	resp := rateLimitsResponse{
		Enabled:      true,
		TotalAllowed: snapshot.TotalAllowed,
		TotalDenied:  snapshot.TotalDenied,
		Pools:        make([]*poolInfo, 0),
	}

	// Include pool config and metrics
	for _, name := range s.rateLimiter.PoolNames() {
		pool := s.rateLimiter.GetPool(name)
		if pool == nil {
			continue
		}
		poolMetrics := snapshot.Pools[name]

		resp.Pools = append(resp.Pools, &poolInfo{
			Name:              name,
			RequestsPerSecond: pool.RequestsPerSecond(),
			Burst:             pool.Burst(),
			Allowed:           poolMetrics.Allowed,
			Denied:            poolMetrics.Denied,
			ActiveBuckets:     poolMetrics.ActiveBuckets,
		})
	}

	writeJSON(w, resp)
}

// rateLimitsResetHandler resets rate limit buckets for test isolation
func (s *Server) rateLimitsResetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Only allow POST/DELETE methods
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, errorResponse{
			Error: "method not allowed, use POST or DELETE",
		})
		return
	}

	if s.rateLimiter == nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, errorResponse{
			Error: "rate limiting not configured",
		})
		return
	}

	pool := r.URL.Query().Get("pool")
	key := r.URL.Query().Get("key")

	type resetResponse struct {
		Status         string `json:"status"`
		Message        string `json:"message"`
		Pool           string `json:"pool,omitempty"`
		Key            string `json:"key,omitempty"`
		BucketsCleared int    `json:"buckets_cleared"`
	}

	// Reset specific key in pool
	if key != "" {
		if pool == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, errorResponse{
				Error: "key parameter requires pool parameter",
			})
			return
		}

		cleared, err := s.rateLimiter.ResetKey(pool, key)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, errorResponse{Error: err.Error()})
			return
		}

		count := 0
		if cleared {
			count = 1
		}

		logging.Info("ratelimit_reset_key", map[string]any{
			"pool": pool,
			"key":  key,
		})

		writeJSON(w, resetResponse{
			Status:         "ok",
			Message:        "rate limit key reset",
			Pool:           pool,
			Key:            key,
			BucketsCleared: count,
		})
		return
	}

	// Reset specific pool
	if pool != "" {
		count, err := s.rateLimiter.ResetPool(pool)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, errorResponse{Error: err.Error()})
			return
		}

		logging.Info("ratelimit_reset_pool", map[string]any{
			"pool":            pool,
			"buckets_cleared": count,
		})

		writeJSON(w, resetResponse{
			Status:         "ok",
			Message:        "rate limit pool reset",
			Pool:           pool,
			BucketsCleared: count,
		})
		return
	}

	// Reset all pools
	count := s.rateLimiter.ResetAll()

	logging.Info("ratelimit_reset_all", map[string]any{
		"buckets_cleared": count,
	})

	writeJSON(w, resetResponse{
		Status:         "ok",
		Message:        "all rate limits reset",
		BucketsCleared: count,
	})
}

func (s *Server) startTime() time.Time {
	return s.createdAt
}

func (s *Server) listEndpointsHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	type endpointInfo struct {
		Name       string                 `json:"name"`
		Path       string                 `json:"path"`
		Method     string                 `json:"method"`
		Parameters []workflow.ParamConfig `json:"parameters,omitempty"`
		TimeoutSec int                    `json:"timeout_sec"`
	}

	type scheduledInfo struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
	}

	endpoints := make([]endpointInfo, 0)
	scheduled := make([]scheduledInfo, 0)

	for _, wf := range s.workflows {
		effectiveTimeout := s.config.Server.DefaultTimeoutSec
		if wf.Config.TimeoutSec > 0 {
			effectiveTimeout = wf.Config.TimeoutSec
		}

		for _, trigger := range wf.Triggers {
			if trigger.Config.Type == "http" && trigger.Config.Path != "" {
				endpoints = append(endpoints, endpointInfo{
					Name:       wf.Config.Name,
					Path:       trigger.Config.Path,
					Method:     trigger.Config.Method,
					Parameters: trigger.Config.Parameters,
					TimeoutSec: effectiveTimeout,
				})
			} else if trigger.Config.Type == "cron" {
				scheduled = append(scheduled, scheduledInfo{
					Name:     wf.Config.Name,
					Schedule: trigger.Config.Schedule,
				})
			}
		}
	}

	response := map[string]any{
		"service":             "sql-proxy",
		"version":             s.config.Server.Version,
		"build_time":          s.config.Server.BuildTime,
		"default_timeout_sec": s.config.Server.DefaultTimeoutSec,
		"max_timeout_sec":     s.config.Server.MaxTimeoutSec,
		"databases":           s.dbManager.Names(),
		"db_healthy":          s.dbHealthy.Load(),
		"workflows":           endpoints,
	}

	if len(scheduled) > 0 {
		response["scheduled_workflows"] = scheduled
	}

	writeJSON(w, response)
}

// initWorkflows validates, compiles, and initializes workflows
func (s *Server) initWorkflows(cfg *config.Config) error {
	// Build validation context
	databases := make(map[string]bool)
	for _, dbCfg := range cfg.Databases {
		databases[dbCfg.Name] = dbCfg.IsReadOnly()
	}
	rateLimitPools := make(map[string]bool)
	for _, rl := range cfg.RateLimits {
		rateLimitPools[rl.Name] = true
	}
	validationCtx := &workflow.ValidationContext{
		Databases:      databases,
		RateLimitPools: rateLimitPools,
	}

	// Create DB manager adapter for workflow execution
	dbAdapter := workflow.NewDBManagerAdapter(func(ctx context.Context, database, sqlQuery string, params map[string]any, opts step.QueryOptions) (*step.QueryResult, error) {
		driver, err := s.dbManager.Get(database)
		if err != nil {
			return nil, err
		}

		session := config.SessionConfig{
			Isolation:        opts.Isolation,
			DeadlockPriority: opts.DeadlockPriority,
		}
		if opts.LockTimeoutMs != nil {
			session.LockTimeoutMs = *opts.LockTimeoutMs
		}

		dbResult, err := driver.Query(ctx, session, sqlQuery, params)
		if err != nil {
			return nil, err
		}

		// Parse JSON columns if specified
		if len(opts.JSONColumns) > 0 {
			if err := parseJSONColumns(dbResult.Rows, opts.JSONColumns); err != nil {
				return nil, err
			}
		}

		return &step.QueryResult{
			Rows:         dbResult.Rows,
			RowsAffected: dbResult.RowsAffected,
		}, nil
	})

	// Create logger adapter
	loggerAdapter := &serverLoggerAdapter{}

	// Create workflow executor with cache (cache may be nil if not enabled)
	s.workflowExecutor = workflow.NewExecutor(dbAdapter, http.DefaultClient, s.cache, loggerAdapter)

	// Validate and compile each workflow
	s.workflows = make([]*workflow.CompiledWorkflow, 0, len(cfg.Workflows))
	for _, wfCfg := range cfg.Workflows {
		wfCfgCopy := wfCfg // Copy to avoid closure issues

		// Validate
		result := workflow.Validate(&wfCfgCopy, validationCtx)
		if !result.Valid {
			logging.Error("workflow_validation_failed", map[string]any{
				"workflow": wfCfgCopy.Name,
				"errors":   result.Errors,
			})
			return fmt.Errorf("workflow %q validation failed: %v", wfCfgCopy.Name, result.Errors)
		}

		// Log warnings
		for _, warning := range result.Warnings {
			logging.Warn("workflow_validation_warning", map[string]any{
				"workflow": wfCfgCopy.Name,
				"warning":  warning,
			})
		}

		// Compile
		compiled, err := workflow.Compile(&wfCfgCopy)
		if err != nil {
			logging.Error("workflow_compile_failed", map[string]any{
				"workflow": wfCfgCopy.Name,
				"error":    err.Error(),
			})
			return fmt.Errorf("workflow %q compilation failed: %w", wfCfgCopy.Name, err)
		}

		s.workflows = append(s.workflows, compiled)

		logging.Info("workflow_compiled", map[string]any{
			"workflow": wfCfgCopy.Name,
			"triggers": len(compiled.Triggers),
			"steps":    len(compiled.Steps),
		})
	}

	logging.Info("workflows_initialized", map[string]any{
		"count": len(s.workflows),
	})

	// Check for route clashes across all workflows
	routes := make(map[string]string) // "METHOD /path" -> workflow name
	for _, wf := range s.workflows {
		for _, trigger := range wf.Triggers {
			if trigger.Config.Type != "http" {
				continue
			}
			route := trigger.Config.Method + " " + trigger.Config.Path
			if existingWorkflow, exists := routes[route]; exists {
				return fmt.Errorf("route clash: %s is defined in both %q and %q", route, existingWorkflow, wf.Config.Name)
			}
			routes[route] = wf.Config.Name
		}
	}

	return nil
}

// addWorkflowCronJobs adds cron triggers from workflows to the cron scheduler
func (s *Server) addWorkflowCronJobs() error {
	hasCronTriggers := false
	for _, wf := range s.workflows {
		for _, trigger := range wf.Triggers {
			if trigger.Config.Type == "cron" {
				hasCronTriggers = true
				break
			}
		}
		if hasCronTriggers {
			break
		}
	}

	if !hasCronTriggers {
		return nil
	}

	// Create cron scheduler with cancelable context for graceful shutdown
	s.cronCtx, s.cronCancel = context.WithCancel(context.Background())
	s.cron = cron.New()

	// Add workflow cron jobs
	for _, wf := range s.workflows {
		for _, trigger := range wf.Triggers {
			if trigger.Config.Type != "cron" {
				continue
			}

			// Capture variables for closure
			wfCopy := wf
			triggerCopy := trigger

			_, err := s.cron.AddFunc(trigger.Config.Schedule, func() {
				s.executeWorkflowCron(wfCopy, triggerCopy)
			})
			if err != nil {
				logging.Error("workflow_cron_add_failed", map[string]any{
					"workflow": wf.Config.Name,
					"schedule": trigger.Config.Schedule,
					"error":    err.Error(),
				})
				return fmt.Errorf("failed to add workflow %q cron job: %w", wf.Config.Name, err)
			}

			logging.Info("workflow_cron_job_added", map[string]any{
				"workflow": wf.Config.Name,
				"schedule": trigger.Config.Schedule,
			})
		}
	}

	return nil
}

// executeWorkflowCron executes a workflow for a cron trigger
func (s *Server) executeWorkflowCron(wf *workflow.CompiledWorkflow, trigger *workflow.CompiledTrigger) {
	// Recover from panics to prevent crashing the cron goroutine
	defer func() {
		if r := recover(); r != nil {
			logging.Error("workflow_cron_panic", map[string]any{
				"workflow": wf.Config.Name,
				"panic":    fmt.Sprintf("%v", r),
			})
			metrics.RecordCronPanic(wf.Config.Name)
		}
	}()

	requestID := generateCronRequestID()

	// Build trigger data for cron execution
	triggerData := &workflow.TriggerData{
		Type:         "cron",
		Params:       make(map[string]any),
		CronExpr:     trigger.Config.Schedule,
		ScheduleTime: time.Now(),
	}

	// Add scheduled params if configured
	for k, v := range trigger.Config.Params {
		triggerData.Params[k] = resolveDynamicValue(v)
	}

	logging.Info("workflow_cron_started", map[string]any{
		"workflow":   wf.Config.Name,
		"request_id": requestID,
		"schedule":   trigger.Config.Schedule,
	})

	// Use server's cron context for graceful shutdown support
	// Apply workflow timeout if configured
	ctx := s.cronCtx
	if wf.Config.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(wf.Config.TimeoutSec)*time.Second)
		defer cancel()
	}

	// Execute workflow (no HTTP response writer for cron)
	result := s.workflowExecutor.Execute(ctx, wf, triggerData, requestID, nil, s.config.Variables.Values)

	if result.Error != nil {
		logging.Error("workflow_cron_failed", map[string]any{
			"workflow":    wf.Config.Name,
			"request_id":  requestID,
			"error":       result.Error.Error(),
			"duration_ms": result.DurationMs,
		})
	} else {
		logging.Info("workflow_cron_completed", map[string]any{
			"workflow":    wf.Config.Name,
			"request_id":  requestID,
			"duration_ms": result.DurationMs,
			"success":     result.Success,
		})
	}
}

func generateCronRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Use counter + time for uniqueness when crypto/rand fails
		counter := fallbackIDCounter.Add(1)
		return fmt.Sprintf("cron-%x-%d", time.Now().UnixNano(), counter)
	}
	return fmt.Sprintf("cron-%s", hex.EncodeToString(b))
}

func resolveDynamicValue(value string) any {
	lower := strings.ToLower(value)
	now := time.Now()

	switch lower {
	case "now":
		return now
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "yesterday":
		yesterday := now.AddDate(0, 0, -1)
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location())
	case "tomorrow":
		tomorrow := now.AddDate(0, 0, 1)
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, now.Location())
	default:
		return value
	}
}

// serverLoggerAdapter adapts the logging package to workflow.Logger interface
type serverLoggerAdapter struct{}

func (a *serverLoggerAdapter) Debug(msg string, fields map[string]any) {
	logging.Debug(msg, fields)
}

func (a *serverLoggerAdapter) Info(msg string, fields map[string]any) {
	logging.Info(msg, fields)
}

func (a *serverLoggerAdapter) Warn(msg string, fields map[string]any) {
	logging.Warn(msg, fields)
}

func (a *serverLoggerAdapter) Error(msg string, fields map[string]any) {
	logging.Error(msg, fields)
}

// parseJSONColumns parses specified columns from strings to JSON objects in-place.
func parseJSONColumns(results []map[string]any, columns []string) error {
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

			strVal, ok := val.(string)
			if !ok {
				continue
			}

			if strVal == "" {
				continue
			}

			var parsed any
			if err := json.Unmarshal([]byte(strVal), &parsed); err != nil {
				return fmt.Errorf("column '%s': invalid JSON: %w", col, err)
			}
			row[col] = parsed
		}
	}
	return nil
}

// recoveryMiddleware catches panics and logs them with stack traces
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				logging.Error("panic_recovered", map[string]any{
					"error":  fmt.Sprintf("%v", err),
					"path":   r.URL.Path,
					"method": r.Method,
					"stack":  string(stack),
				})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, errorResponse{
					Success: false,
					Error:   "internal server error",
				})
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// bodySizeLimitMiddleware limits the size of request bodies to prevent memory exhaustion
func (s *Server) bodySizeLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		}
		next.ServeHTTP(w, r)
	})
}

// gzipResponseWriter wraps http.ResponseWriter with gzip compression
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (grw *gzipResponseWriter) Write(b []byte) (int, error) {
	return grw.Writer.Write(b)
}

// gzip writer pool to reduce allocations
var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// gzipMiddleware compresses responses for clients that accept gzip
func (s *Server) gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Get a gzip writer from pool
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			// Close flushes the buffer; only return to pool if successful
			if err := gz.Close(); err != nil {
				logging.Debug("gzip_close_error", map[string]any{
					"error": err.Error(),
					"path":  r.URL.Path,
				})
				return // Don't put broken writer back in pool
			}
			gzipWriterPool.Put(gz)
		}()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length") // Length changes with compression

		grw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(grw, r)
	})
}

// Start begins listening for HTTP requests and starts the cron scheduler
func (s *Server) Start() error {
	// Start cron scheduler if configured
	if s.cron != nil {
		s.cron.Start()
		logging.Info("cron_scheduler_started", map[string]any{
			"jobs": len(s.cron.Entries()),
		})
	}

	// Start debug server if configured on separate port
	if s.debugServer != nil {
		go func() {
			logging.Info("debug_server_starting", map[string]any{
				"addr": s.debugServer.Addr,
			})
			if err := s.debugServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logging.Error("debug_server_error", map[string]any{
					"error": err.Error(),
				})
			}
		}()
	}

	logging.Info("server_starting", map[string]any{
		"addr": s.httpServer.Addr,
	})
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	logging.Info("server_shutting_down", nil)

	// Cancel cron job context to stop in-flight cron executions
	if s.cronCancel != nil {
		s.cronCancel()
	}

	// Stop cron scheduler
	if s.cron != nil {
		cronCtx := s.cron.Stop()
		<-cronCtx.Done()
		logging.Info("cron_scheduler_stopped", nil)
	}

	// Stop health checker
	if s.healthChecker != nil {
		s.healthChecker()
	}

	// Shutdown debug server if running
	if s.debugServer != nil {
		if err := s.debugServer.Shutdown(ctx); err != nil {
			logging.Error("debug_server_shutdown_error", map[string]any{
				"error": err.Error(),
			})
		}
	}

	// Shutdown HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		logging.Error("http_shutdown_error", map[string]any{
			"error": err.Error(),
		})
		return err
	}

	// Close cache (stops cron jobs)
	if s.cache != nil {
		s.cache.Close()
		logging.Info("cache_closed", nil)
	}

	// Close database connections
	if err := s.dbManager.Close(); err != nil {
		logging.Error("database_close_error", map[string]any{
			"error": err.Error(),
		})
		return err
	}

	// Close logging last
	logging.Info("server_stopped", nil)
	logging.Close()

	return nil
}

// workflowRateLimiterAdapter implements workflow.RateLimiter using ratelimit.Limiter.
type workflowRateLimiterAdapter struct {
	limiter    *ratelimit.Limiter
	ctxBuilder *tmpl.ContextBuilder
}

// CheckTriggerLimits implements workflow.RateLimiter.
func (a *workflowRateLimiterAdapter) CheckTriggerLimits(limits []*workflow.CompiledRateLimit, rlCtx *workflow.RateLimitContext) (bool, int, error) {
	if a.limiter == nil || len(limits) == 0 {
		return true, 0, nil
	}

	// Convert workflow rate limit configs to ratelimit.Limiter format
	rateLimitConfigs := make([]config.RateLimitConfig, 0, len(limits))
	for _, rl := range limits {
		cfg := config.RateLimitConfig{
			Pool:              rl.Config.Pool,
			RequestsPerSecond: rl.Config.RequestsPerSecond,
			Burst:             rl.Config.Burst,
			Key:               rl.Config.Key,
		}
		rateLimitConfigs = append(rateLimitConfigs, cfg)
	}

	// Build tmpl.Context for key template evaluation
	ctx := a.ctxBuilder.BuildForRateLimit(&tmpl.RateLimitData{
		ClientIP: rlCtx.ClientIP,
		Params:   rlCtx.Params,
		Headers:  rlCtx.Headers,
		Query:    rlCtx.Query,
		Cookies:  rlCtx.Cookies,
	})

	allowed, retryAfter, err := a.limiter.Allow(rateLimitConfigs, ctx)
	if err != nil {
		return false, 0, err
	}

	retryAfterSec := int(retryAfter.Seconds())
	if retryAfterSec < 1 && !allowed {
		retryAfterSec = 1
	}

	return allowed, retryAfterSec, nil
}

// triggerCacheAdapter implements workflow.TriggerCache using cache.Cache.
// It stores response body and status code in the cache using a special format.
type triggerCacheAdapter struct {
	cache *cache.Cache
}

// Get retrieves a cached trigger response.
func (a *triggerCacheAdapter) Get(workflowName, key string) ([]byte, int, bool) {
	if a.cache == nil {
		return nil, 0, false
	}

	data, hit := a.cache.Get(workflowName, key)
	if !hit || len(data) == 0 {
		return nil, 0, false
	}

	// Extract response from stored format
	entry := data[0]
	bodyStr, ok := entry["__body__"].(string)
	if !ok {
		return nil, 0, false
	}
	statusFloat, ok := entry["__status__"].(float64)
	if !ok {
		return nil, 0, false
	}

	return []byte(bodyStr), int(statusFloat), true
}

// Set stores a trigger response in the cache.
func (a *triggerCacheAdapter) Set(workflowName, key string, body []byte, statusCode int, ttl time.Duration) bool {
	if a.cache == nil {
		return false
	}

	// Store in the format expected by cache.Cache ([]map[string]any)
	data := []map[string]any{
		{"__body__": string(body), "__status__": float64(statusCode)},
	}

	return a.cache.Set(workflowName, key, data, ttl)
}
