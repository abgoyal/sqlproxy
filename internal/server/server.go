package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/handler"
	"sql-proxy/internal/logging"
	"sql-proxy/internal/metrics"
	"sql-proxy/internal/openapi"
	"sql-proxy/internal/scheduler"
)

type Server struct {
	httpServer *http.Server
	dbManager  *db.Manager
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
		logging.Info("database_connected", map[string]any{
			"name":     dbCfg.Name,
			"host":     dbCfg.Host,
			"database": dbCfg.Database,
			"readonly": dbCfg.IsReadOnly(),
		})
	}

	s := &Server{
		dbManager: dbManager,
		config:    cfg,
		createdAt: time.Now(),
	}
	s.dbHealthy.Store(true)

	// Initialize metrics
	if cfg.Metrics.Enabled {
		metrics.Init(s.checkDBHealth)
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
	writeTimeout := time.Duration(cfg.Server.MaxTimeoutSec+30) * time.Second

	// Middleware chain: recovery -> gzip -> routes
	handler := s.recoveryMiddleware(s.gzipMiddleware(mux))

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	return s, nil
}

// runHealthChecker periodically checks database connectivity for all connections
func (s *Server) runHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Track consecutive failures per database
	consecutiveFailures := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
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

					// After 3 consecutive failures, try to reconnect
					if consecutiveFailures[name] >= 3 {
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

	// List available endpoints
	mux.HandleFunc("/", s.listEndpointsHandler)

	// Register query endpoints (only for queries with HTTP paths)
	for _, q := range s.config.Queries {
		if q.Path == "" {
			continue // Schedule-only query, no HTTP endpoint
		}
		h := handler.New(s.dbManager, q, s.config.Server)
		mux.Handle(q.Path, h)

		logging.Info("endpoint_registered", map[string]any{
			"method":      q.Method,
			"path":        q.Path,
			"name":        q.Name,
			"database":    q.Database,
			"description": q.Description,
			"scheduled":   q.Schedule != nil,
		})
	}
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	dbHealthy := s.dbHealthy.Load()

	status := "healthy"
	httpStatus := http.StatusOK
	dbStatus := "connected"

	if !dbHealthy {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
		dbStatus = "disconnected"
	}

	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   status,
		"database": dbStatus,
		"uptime":   time.Since(s.startTime()).String(),
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
		"current_level": s.config.Logging.Level,
		"usage":         "POST /config/loglevel?level=debug|info|warn|error",
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

// recoveryMiddleware catches panics and logs them
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logging.Error("panic_recovered", map[string]any{
					"error":  fmt.Sprintf("%v", err),
					"path":   r.URL.Path,
					"method": r.Method,
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
