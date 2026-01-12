package server

import (
	"compress/gzip"
	"context"
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

	"sql-proxy/internal/cache"
	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/handler"
	"sql-proxy/internal/logging"
	"sql-proxy/internal/metrics"
	"sql-proxy/internal/openapi"
	"sql-proxy/internal/ratelimit"
	"sql-proxy/internal/scheduler"
	"sql-proxy/internal/tmpl"
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

	// Scheduled query execution
	scheduler *scheduler.Scheduler
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
		"queries":   len(cfg.Queries),
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

	// Initialize scheduler if there are scheduled queries
	if scheduler.HasScheduledQueries(cfg.Queries) {
		s.scheduler = scheduler.New(dbManager, cfg.Queries, cfg.Server)
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
	mux.HandleFunc("/_/health", s.healthHandler)       // Aggregate health
	mux.HandleFunc("/_/health/", s.dbHealthHandler)    // Per-database health: /_/health/{dbname}

	// Metrics endpoints
	mux.HandleFunc("/_/metrics.json", s.metricsJSONHandler)  // Human-readable JSON metrics
	mux.HandleFunc("/_/metrics", s.metricsPrometheusHandler) // Prometheus format

	// OpenAPI spec endpoint
	mux.HandleFunc("/_/openapi.json", s.openAPIHandler)

	// Runtime config endpoint
	mux.HandleFunc("/_/config/loglevel", s.logLevelHandler)

	// Cache management endpoint
	mux.HandleFunc("/_/cache/clear", s.cacheClearHandler)

	// Rate limit observability endpoint
	mux.HandleFunc("/_/ratelimits", s.rateLimitsHandler)

	// List available endpoints
	mux.HandleFunc("/", s.listEndpointsHandler)

	// Register query endpoints (only for queries with HTTP paths)
	for _, q := range s.config.Queries {
		if q.Path == "" {
			continue // Schedule-only query, no HTTP endpoint
		}

		// Register cache endpoint if caching is enabled for this query
		if s.cache != nil && q.Cache != nil && q.Cache.Enabled {
			if err := s.cache.RegisterEndpoint(q.Path, q.Cache); err != nil {
				logging.Warn("cache_endpoint_registration_failed", map[string]any{
					"path":  q.Path,
					"error": err.Error(),
				})
			}
		}

		h := handler.New(s.dbManager, s.cache, s.rateLimiter, s.ctxBuilder, q, s.config.Server)
		mux.Handle(q.Path, h)

		logFields := map[string]any{
			"method":      q.Method,
			"path":        q.Path,
			"name":        q.Name,
			"database":    q.Database,
			"description": q.Description,
			"scheduled":   q.Schedule != nil,
		}
		if q.Cache != nil && q.Cache.Enabled {
			logFields["cached"] = true
			logFields["cache_key"] = q.Cache.Key
			if q.Cache.TTLSec > 0 {
				logFields["cache_ttl_sec"] = q.Cache.TTLSec
			}
		}
		logging.Info("endpoint_registered", logFields)
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
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
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
		Name        string               `json:"name"`
		Path        string               `json:"path"`
		Method      string               `json:"method"`
		Database    string               `json:"database"`
		Description string               `json:"description"`
		Parameters  []config.ParamConfig `json:"parameters,omitempty"`
		TimeoutSec  int                  `json:"timeout_sec"`
		TimeoutNote string               `json:"timeout_note"`
		Schedule    string               `json:"schedule,omitempty"`
	}

	type scheduledInfo struct {
		Name        string `json:"name"`
		Database    string `json:"database"`
		Description string `json:"description"`
		Cron        string `json:"cron"`
		LogResults  bool   `json:"log_results"`
	}

	endpoints := make([]endpointInfo, 0)
	scheduled := make([]scheduledInfo, 0)

	for _, q := range s.config.Queries {
		// Add to HTTP endpoints list if it has a path
		if q.Path != "" {
			effectiveTimeout := s.config.Server.DefaultTimeoutSec
			if q.TimeoutSec > 0 {
				effectiveTimeout = q.TimeoutSec
			}
			ep := endpointInfo{
				Name:        q.Name,
				Path:        q.Path,
				Method:      q.Method,
				Database:    q.Database,
				Description: q.Description,
				Parameters:  q.Parameters,
				TimeoutSec:  effectiveTimeout,
				TimeoutNote: fmt.Sprintf("Override with _timeout param (max %d)", s.config.Server.MaxTimeoutSec),
			}
			if q.Schedule != nil {
				ep.Schedule = q.Schedule.Cron
			}
			endpoints = append(endpoints, ep)
		}

		// Add to scheduled list if it has a schedule
		if q.Schedule != nil {
			scheduled = append(scheduled, scheduledInfo{
				Name:        q.Name,
				Database:    q.Database,
				Description: q.Description,
				Cron:        q.Schedule.Cron,
				LogResults:  q.Schedule.LogResults,
			})
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
		"endpoints":           endpoints,
	}

	if len(scheduled) > 0 {
		response["scheduled_queries"] = scheduled
	}

	writeJSON(w, response)
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

// Start begins listening for HTTP requests and starts the scheduler
func (s *Server) Start() error {
	// Start scheduler if configured
	if s.scheduler != nil {
		s.scheduler.Start()
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

	// Stop scheduler
	if s.scheduler != nil {
		s.scheduler.Stop()
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
