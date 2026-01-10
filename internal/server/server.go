package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sql-proxy/internal/cache"
	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/handler"
	"sql-proxy/internal/logging"
	"sql-proxy/internal/metrics"
	"sql-proxy/internal/openapi"
	"sql-proxy/internal/scheduler"
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
)

type Server struct {
	httpServer *http.Server
	dbManager  *db.Manager
	cache      *cache.Cache
	config     *config.Config
	createdAt  time.Time

	// Health tracking (all DBs healthy)
	dbHealthy     atomic.Bool
	healthChecker context.CancelFunc

	// Scheduled query execution
	scheduler *scheduler.Scheduler
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
		"version":   "1.0.0",
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

	// Initialize metrics
	if cfg.Metrics.Enabled {
		metrics.Init(s.checkDBHealth)
		// Set cache snapshot provider for metrics
		if s.cache != nil {
			metrics.SetCacheSnapshotProvider(func() any {
				return s.cache.GetSnapshot()
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

	// Middleware chain: recovery -> gzip -> routes
	handler := s.recoveryMiddleware(s.gzipMiddleware(mux))

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  httpIdleTimeout,
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
	// Health check endpoint
	mux.HandleFunc("/health", s.healthHandler)

	// Metrics endpoint (returns current snapshot)
	mux.HandleFunc("/metrics", s.metricsHandler)

	// OpenAPI spec endpoint
	mux.HandleFunc("/openapi.json", s.openAPIHandler)

	// Runtime config endpoint
	mux.HandleFunc("/config/loglevel", s.logLevelHandler)

	// Cache management endpoint
	mux.HandleFunc("/cache/clear", s.cacheClearHandler)

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

		h := handler.New(s.dbManager, s.cache, q, s.config.Server)
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

	// Build per-database status
	databases := make(map[string]string)
	allHealthy := true
	for name, err := range dbResults {
		if err != nil {
			databases[name] = "disconnected"
			allHealthy = false
		} else {
			databases[name] = "connected"
		}
	}

	status := "healthy"
	httpStatus := http.StatusOK

	if !allHealthy {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    status,
		"databases": databases,
		"uptime":    time.Since(s.startTime()).String(),
	})
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	snap := metrics.GetSnapshot()
	if snap == nil {
		json.NewEncoder(w).Encode(map[string]string{
			"error": "metrics not enabled",
		})
		return
	}

	json.NewEncoder(w).Encode(snap)
}

func (s *Server) openAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // Allow Swagger UI from anywhere

	spec := openapi.Spec(s.config)
	json.NewEncoder(w).Encode(spec)
}

func (s *Server) logLevelHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		level := r.URL.Query().Get("level")
		if level == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "level parameter required (debug, info, warn, error)",
			})
			return
		}

		logging.SetLevel(level)
		logging.Info("log_level_changed", map[string]any{
			"new_level": level,
		})

		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"level":  level,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"current_level": logging.GetLevel(),
		"usage":         "POST /config/loglevel?level=debug|info|warn|error",
	})
}

func (s *Server) cacheClearHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Only allow POST/DELETE methods
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed, use POST or DELETE",
		})
		return
	}

	// Check if cache is enabled
	if s.cache == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "cache not enabled",
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
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"message":  "cache cleared for endpoint",
			"endpoint": endpoint,
		})
	} else {
		// Clear all cache
		s.cache.ClearAll()
		logging.Info("cache_cleared_all", nil)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"message": "all cache cleared",
		})
	}
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
		"default_timeout_sec": s.config.Server.DefaultTimeoutSec,
		"max_timeout_sec":     s.config.Server.MaxTimeoutSec,
		"databases":           s.dbManager.Names(),
		"db_healthy":          s.dbHealthy.Load(),
		"endpoints":           endpoints,
	}

	if len(scheduled) > 0 {
		response["scheduled_queries"] = scheduled
	}

	json.NewEncoder(w).Encode(response)
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
				json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error":   "internal server error",
				})
			}
		}()

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
			gz.Close()
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

	// Close metrics (exports final metrics)
	if err := metrics.Close(); err != nil {
		logging.Warn("metrics_close_error", map[string]any{
			"error": err.Error(),
		})
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
