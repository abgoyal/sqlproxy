package metrics

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Duration unit: 100 nanoseconds (0.1 microseconds)
// This gives us sub-microsecond precision while using int64 atomics.
// Max int64 = ~29,247 years of cumulative duration.
const durationUnit = 100 // nanoseconds per unit

// RequestMetrics captures metrics for a single request
type RequestMetrics struct {
	Endpoint      string
	QueryName     string
	Database      string
	Method        string
	TotalDuration time.Duration
	QueryDuration time.Duration
	RowCount      int
	StatusCode    int
	Error         string
	ErrorType     string // timeout, query_failed, rate_limited, etc.
	CacheHit      bool
}

// EndpointStats aggregates stats for an endpoint
type EndpointStats struct {
	Endpoint      string  `json:"endpoint"`
	QueryName     string  `json:"query_name"`
	RequestCount  int64   `json:"request_count"`
	ErrorCount    int64   `json:"error_count"`
	TimeoutCount  int64   `json:"timeout_count"`
	TotalRows     int64   `json:"total_rows"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	MaxDurationMs float64 `json:"max_duration_ms"`
	MinDurationMs float64 `json:"min_duration_ms"`
	AvgQueryMs    float64 `json:"avg_query_ms"`
}

// RuntimeStats captures Go runtime metrics
type RuntimeStats struct {
	GoVersion      string `json:"go_version"`
	NumGoroutine   int    `json:"goroutines"`
	NumCPU         int    `json:"num_cpu"`
	MemAllocBytes  uint64 `json:"mem_alloc_bytes"`
	MemTotalAlloc  uint64 `json:"mem_total_alloc"`
	MemSysBytes    uint64 `json:"mem_sys_bytes"`
	MemHeapObjects uint64 `json:"mem_heap_objects"`
	NumGC          uint32 `json:"gc_runs"`
	GCPauseNs      uint64 `json:"gc_pause_total_ns"`
	LastGCPauseNs  uint64 `json:"gc_last_pause_ns"`
}

// CacheSnapshotProvider is a function that returns cache metrics
type CacheSnapshotProvider func() any

// RateLimitSnapshotProvider is a function that returns rate limit metrics
type RateLimitSnapshotProvider func() any

// DBStatsProvider is a function that returns database connection pool stats
type DBStatsProvider func() map[string]DBPoolStats

// DBPoolStats contains connection pool statistics
type DBPoolStats struct {
	OpenConnections int `json:"open_connections"`
	IdleConnections int `json:"idle_connections"`
	InUseConns      int `json:"in_use_connections"`
	WaitCount       int `json:"wait_count"`
}

// Snapshot represents metrics at a point in time
type Snapshot struct {
	Timestamp     time.Time                 `json:"timestamp"`
	Version       string                    `json:"version,omitempty"`
	BuildTime     string                    `json:"build_time,omitempty"`
	UptimeSec     int64                     `json:"uptime_sec"`
	TotalRequests int64                     `json:"total_requests"`
	TotalErrors   int64                     `json:"total_errors"`
	TotalTimeouts int64                     `json:"total_timeouts"`
	Endpoints     map[string]*EndpointStats `json:"endpoints"`
	DBHealthy     bool                      `json:"db_healthy"`
	Runtime       RuntimeStats              `json:"runtime"`
	Cache         any                       `json:"cache,omitempty"`
	RateLimits    any                       `json:"rate_limits,omitempty"`
}

// endpointData stores per-endpoint metrics using atomic counters
type endpointData struct {
	queryName string

	// Atomic counters
	requestCount atomic.Int64
	errorCount   atomic.Int64
	timeoutCount atomic.Int64
	totalRows    atomic.Int64

	// Durations in 100ns units for atomic operations
	totalDuration atomic.Int64 // sum of durations
	totalQueryMs  atomic.Int64 // sum of query durations
	maxDuration   atomic.Int64
	minDuration   atomic.Int64 // initialized to max int64, updated with CompareAndSwap
}

// Collector collects metrics
type Collector struct {
	startTime                 time.Time
	version                   string
	buildTime                 string
	dbHealthChecker           func() bool
	cacheSnapshotProvider     CacheSnapshotProvider
	rateLimitSnapshotProvider RateLimitSnapshotProvider
	dbStatsProvider           DBStatsProvider

	// Atomic global counters
	totalRequests atomic.Int64
	totalErrors   atomic.Int64
	totalTimeouts atomic.Int64

	// Endpoint map requires mutex for map access, but values are atomic
	mu        sync.RWMutex
	endpoints map[string]*endpointData

	// Prometheus metrics
	promRegistry      *prometheus.Registry
	promInfo          *prometheus.GaugeVec
	promUptime        prometheus.Gauge
	promRequests      *prometheus.CounterVec
	promDuration      *prometheus.HistogramVec
	promQueryDuration *prometheus.HistogramVec
	promRows          *prometheus.CounterVec
	promErrors        *prometheus.CounterVec
	promDBHealthy     *prometheus.GaugeVec
	promDBConnsOpen   *prometheus.GaugeVec
	promDBConnsIdle   *prometheus.GaugeVec
	promCacheHits     *prometheus.CounterVec
	promCacheMisses   *prometheus.CounterVec
	promCacheSize     *prometheus.GaugeVec
	promCacheKeys     *prometheus.GaugeVec
	promRLAllowed     *prometheus.CounterVec
	promRLDenied      *prometheus.CounterVec
	promRLBuckets     *prometheus.GaugeVec
}

var defaultCollector *Collector

// Default histogram buckets (in seconds)
var defaultDurationBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// Init initializes the global metrics collector
func Init(dbHealthChecker func() bool, version, buildTime string) {
	c := &Collector{
		startTime:       time.Now(),
		version:         version,
		buildTime:       buildTime,
		dbHealthChecker: dbHealthChecker,
		endpoints:       make(map[string]*endpointData),
		promRegistry:    prometheus.NewRegistry(),
	}

	// Register Go runtime collectors
	c.promRegistry.MustRegister(collectors.NewGoCollector())
	c.promRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// Initialize Prometheus metrics
	c.initPrometheusMetrics()

	defaultCollector = c
}

// initPrometheusMetrics creates all Prometheus metric descriptors
func (c *Collector) initPrometheusMetrics() {
	// Build info gauge
	c.promInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_info",
			Help: "Build information",
		},
		[]string{"version", "build_time"},
	)
	c.promInfo.WithLabelValues(c.version, c.buildTime).Set(1)
	c.promRegistry.MustRegister(c.promInfo)

	// Uptime gauge
	c.promUptime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sqlproxy_uptime_seconds",
		Help: "Time since service start in seconds",
	})
	c.promRegistry.MustRegister(c.promUptime)

	// Request counter
	c.promRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"endpoint", "method", "status_code"},
	)
	c.promRegistry.MustRegister(c.promRequests)

	// Request duration histogram
	c.promDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sqlproxy_request_duration_seconds",
			Help:    "Request latency distribution",
			Buckets: defaultDurationBuckets,
		},
		[]string{"endpoint"},
	)
	c.promRegistry.MustRegister(c.promDuration)

	// Query duration histogram
	c.promQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sqlproxy_query_duration_seconds",
			Help:    "SQL query latency distribution",
			Buckets: defaultDurationBuckets,
		},
		[]string{"endpoint", "database"},
	)
	c.promRegistry.MustRegister(c.promQueryDuration)

	// Row counter
	c.promRows = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_query_rows_total",
			Help: "Total rows returned by queries",
		},
		[]string{"endpoint"},
	)
	c.promRegistry.MustRegister(c.promRows)

	// Error counter
	c.promErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_errors_total",
			Help: "Total errors by type",
		},
		[]string{"endpoint", "error_type"},
	)
	c.promRegistry.MustRegister(c.promErrors)

	// Database health gauge
	c.promDBHealthy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_db_healthy",
			Help: "Database health status (1=healthy, 0=unhealthy)",
		},
		[]string{"database"},
	)
	c.promRegistry.MustRegister(c.promDBHealthy)

	// Database connection gauges
	c.promDBConnsOpen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_db_connections_open",
			Help: "Current open database connections",
		},
		[]string{"database"},
	)
	c.promRegistry.MustRegister(c.promDBConnsOpen)

	c.promDBConnsIdle = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_db_connections_idle",
			Help: "Current idle database connections",
		},
		[]string{"database"},
	)
	c.promRegistry.MustRegister(c.promDBConnsIdle)

	// Cache metrics
	c.promCacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_cache_hits_total",
			Help: "Cache hits",
		},
		[]string{"endpoint"},
	)
	c.promRegistry.MustRegister(c.promCacheHits)

	c.promCacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_cache_misses_total",
			Help: "Cache misses",
		},
		[]string{"endpoint"},
	)
	c.promRegistry.MustRegister(c.promCacheMisses)

	c.promCacheSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_cache_size_bytes",
			Help: "Current cache size in bytes",
		},
		[]string{"endpoint"},
	)
	c.promRegistry.MustRegister(c.promCacheSize)

	c.promCacheKeys = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_cache_keys",
			Help: "Number of cached keys",
		},
		[]string{"endpoint"},
	)
	c.promRegistry.MustRegister(c.promCacheKeys)

	// Rate limit metrics
	c.promRLAllowed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_ratelimit_allowed_total",
			Help: "Requests allowed by rate limiter",
		},
		[]string{"pool"},
	)
	c.promRegistry.MustRegister(c.promRLAllowed)

	c.promRLDenied = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqlproxy_ratelimit_denied_total",
			Help: "Requests denied by rate limiter",
		},
		[]string{"pool"},
	)
	c.promRegistry.MustRegister(c.promRLDenied)

	c.promRLBuckets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sqlproxy_ratelimit_active_buckets",
			Help: "Active rate limit buckets",
		},
		[]string{"pool"},
	)
	c.promRegistry.MustRegister(c.promRLBuckets)
}

// Registry returns the Prometheus registry for use with promhttp.Handler
func Registry() *prometheus.Registry {
	if defaultCollector == nil {
		return nil
	}
	return defaultCollector.promRegistry
}

// SetCacheSnapshotProvider sets the function that provides cache metrics
func SetCacheSnapshotProvider(provider CacheSnapshotProvider) {
	if defaultCollector != nil {
		defaultCollector.cacheSnapshotProvider = provider
	}
}

// SetRateLimitSnapshotProvider sets the function that provides rate limit metrics
func SetRateLimitSnapshotProvider(provider RateLimitSnapshotProvider) {
	if defaultCollector != nil {
		defaultCollector.rateLimitSnapshotProvider = provider
	}
}

// SetDBStatsProvider sets the function that provides database pool stats
func SetDBStatsProvider(provider DBStatsProvider) {
	if defaultCollector != nil {
		defaultCollector.dbStatsProvider = provider
	}
}

// Reset clears all metrics counters while preserving configuration.
func Reset() {
	if defaultCollector == nil {
		return
	}
	c := defaultCollector

	c.startTime = time.Now()
	c.totalRequests.Store(0)
	c.totalErrors.Store(0)
	c.totalTimeouts.Store(0)

	c.mu.Lock()
	c.endpoints = make(map[string]*endpointData)
	c.mu.Unlock()
}

// Clear removes the global collector entirely (for testing)
func Clear() {
	defaultCollector = nil
}

// Record records metrics for a completed request
func Record(m RequestMetrics) {
	if defaultCollector == nil {
		return
	}
	c := defaultCollector

	// Update global atomic counters
	c.totalRequests.Add(1)
	if m.Error != "" {
		c.totalErrors.Add(1)
	}
	if m.StatusCode == 504 {
		c.totalTimeouts.Add(1)
	}

	// Convert durations to our unit (100ns)
	durationUnits := m.TotalDuration.Nanoseconds() / durationUnit
	queryUnits := m.QueryDuration.Nanoseconds() / durationUnit

	// Get or create endpoint data
	ep := c.getOrCreateEndpoint(m.Endpoint, m.QueryName)

	// Update endpoint atomic counters
	ep.requestCount.Add(1)
	ep.totalRows.Add(int64(m.RowCount))
	ep.totalDuration.Add(durationUnits)
	ep.totalQueryMs.Add(queryUnits)

	if m.Error != "" {
		ep.errorCount.Add(1)
	}
	if m.StatusCode == 504 {
		ep.timeoutCount.Add(1)
	}

	// Update max duration (atomic compare-and-swap loop)
	for {
		oldMax := ep.maxDuration.Load()
		if durationUnits <= oldMax {
			break
		}
		if ep.maxDuration.CompareAndSwap(oldMax, durationUnits) {
			break
		}
	}

	// Update min duration (atomic compare-and-swap loop)
	for {
		oldMin := ep.minDuration.Load()
		if durationUnits >= oldMin {
			break
		}
		if ep.minDuration.CompareAndSwap(oldMin, durationUnits) {
			break
		}
	}

	// Update Prometheus metrics
	c.promRequests.WithLabelValues(m.Endpoint, m.Method, statusCodeString(m.StatusCode)).Inc()
	c.promDuration.WithLabelValues(m.Endpoint).Observe(m.TotalDuration.Seconds())
	if m.Database != "" {
		c.promQueryDuration.WithLabelValues(m.Endpoint, m.Database).Observe(m.QueryDuration.Seconds())
	}
	c.promRows.WithLabelValues(m.Endpoint).Add(float64(m.RowCount))

	if m.ErrorType != "" {
		c.promErrors.WithLabelValues(m.Endpoint, m.ErrorType).Inc()
	} else if m.Error != "" {
		c.promErrors.WithLabelValues(m.Endpoint, "unknown").Inc()
	}

	// Cache hit/miss
	if m.CacheHit {
		c.promCacheHits.WithLabelValues(m.Endpoint).Inc()
	} else if m.Endpoint != "" {
		// Only count misses for requests that could have been cached
		// (we don't know here, but handler will set CacheHit=false for cache-enabled endpoints)
	}
}

// RecordCacheMiss records a cache miss for Prometheus metrics
func RecordCacheMiss(endpoint string) {
	if defaultCollector == nil {
		return
	}
	defaultCollector.promCacheMisses.WithLabelValues(endpoint).Inc()
}

// RecordRateLimitAllowed records an allowed request for a rate limit pool
func RecordRateLimitAllowed(pool string) {
	if defaultCollector == nil {
		return
	}
	defaultCollector.promRLAllowed.WithLabelValues(pool).Inc()
}

// RecordRateLimitDenied records a denied request for a rate limit pool
func RecordRateLimitDenied(pool string) {
	if defaultCollector == nil {
		return
	}
	defaultCollector.promRLDenied.WithLabelValues(pool).Inc()
}

// UpdateDBHealth updates database health gauge for Prometheus
func UpdateDBHealth(database string, healthy bool) {
	if defaultCollector == nil {
		return
	}
	val := 0.0
	if healthy {
		val = 1.0
	}
	defaultCollector.promDBHealthy.WithLabelValues(database).Set(val)
}

// UpdateDBPoolStats updates database connection pool gauges for Prometheus
func UpdateDBPoolStats(database string, open, idle int) {
	if defaultCollector == nil {
		return
	}
	defaultCollector.promDBConnsOpen.WithLabelValues(database).Set(float64(open))
	defaultCollector.promDBConnsIdle.WithLabelValues(database).Set(float64(idle))
}

// UpdateCacheStats updates cache gauges for Prometheus
func UpdateCacheStats(endpoint string, sizeBytes, keyCount int64) {
	if defaultCollector == nil {
		return
	}
	defaultCollector.promCacheSize.WithLabelValues(endpoint).Set(float64(sizeBytes))
	defaultCollector.promCacheKeys.WithLabelValues(endpoint).Set(float64(keyCount))
}

// UpdateRateLimitBuckets updates rate limit bucket gauge for Prometheus
func UpdateRateLimitBuckets(pool string, count int64) {
	if defaultCollector == nil {
		return
	}
	defaultCollector.promRLBuckets.WithLabelValues(pool).Set(float64(count))
}

// getOrCreateEndpoint returns existing endpoint data or creates new one
func (c *Collector) getOrCreateEndpoint(endpoint, queryName string) *endpointData {
	c.mu.RLock()
	ep, exists := c.endpoints[endpoint]
	c.mu.RUnlock()

	if exists {
		return ep
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if ep, exists = c.endpoints[endpoint]; exists {
		return ep
	}

	ep = &endpointData{
		queryName: queryName,
	}
	// Initialize minDuration to max int64 so first request sets it
	ep.minDuration.Store(1<<63 - 1)
	c.endpoints[endpoint] = ep

	return ep
}

// GetSnapshot returns current metrics snapshot (for JSON endpoint)
func GetSnapshot() *Snapshot {
	if defaultCollector == nil {
		return nil
	}
	c := defaultCollector

	now := time.Now()

	// Update uptime for Prometheus
	c.promUptime.Set(now.Sub(c.startTime).Seconds())

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	snap := &Snapshot{
		Timestamp:     now,
		Version:       c.version,
		BuildTime:     c.buildTime,
		UptimeSec:     int64(now.Sub(c.startTime).Seconds()),
		TotalRequests: c.totalRequests.Load(),
		TotalErrors:   c.totalErrors.Load(),
		TotalTimeouts: c.totalTimeouts.Load(),
		Endpoints:     make(map[string]*EndpointStats),
		DBHealthy:     c.dbHealthChecker != nil && c.dbHealthChecker(),
		Runtime: RuntimeStats{
			GoVersion:      runtime.Version(),
			NumGoroutine:   runtime.NumGoroutine(),
			NumCPU:         runtime.NumCPU(),
			MemAllocBytes:  memStats.Alloc,
			MemTotalAlloc:  memStats.TotalAlloc,
			MemSysBytes:    memStats.Sys,
			MemHeapObjects: memStats.HeapObjects,
			NumGC:          memStats.NumGC,
			GCPauseNs:      memStats.PauseTotalNs,
			LastGCPauseNs:  memStats.PauseNs[(memStats.NumGC+255)%256],
		},
	}

	c.mu.RLock()
	for endpoint, ep := range c.endpoints {
		reqCount := ep.requestCount.Load()
		totalDur := ep.totalDuration.Load()
		totalQuery := ep.totalQueryMs.Load()
		minDur := ep.minDuration.Load()
		maxDur := ep.maxDuration.Load()

		// Convert from 100ns units to milliseconds
		unitToMs := float64(durationUnit) / 1e6

		stats := &EndpointStats{
			Endpoint:      endpoint,
			QueryName:     ep.queryName,
			RequestCount:  reqCount,
			ErrorCount:    ep.errorCount.Load(),
			TimeoutCount:  ep.timeoutCount.Load(),
			TotalRows:     ep.totalRows.Load(),
			MaxDurationMs: float64(maxDur) * unitToMs,
			MinDurationMs: float64(minDur) * unitToMs,
		}

		// Handle case where no requests yet (min would be max int64)
		if reqCount == 0 || minDur == 1<<63-1 {
			stats.MinDurationMs = 0
		}

		if reqCount > 0 {
			stats.AvgDurationMs = (float64(totalDur) * unitToMs) / float64(reqCount)
			stats.AvgQueryMs = (float64(totalQuery) * unitToMs) / float64(reqCount)
		}

		snap.Endpoints[endpoint] = stats
	}
	c.mu.RUnlock()

	// Include cache stats if provider is set
	if c.cacheSnapshotProvider != nil {
		snap.Cache = c.cacheSnapshotProvider()
	}

	// Include rate limit stats if provider is set
	if c.rateLimitSnapshotProvider != nil {
		snap.RateLimits = c.rateLimitSnapshotProvider()
	}

	return snap
}

// statusCodeString converts status code to string for labels
func statusCodeString(code int) string {
	switch code {
	case 200:
		return "200"
	case 400:
		return "400"
	case 404:
		return "404"
	case 405:
		return "405"
	case 429:
		return "429"
	case 500:
		return "500"
	case 504:
		return "504"
	default:
		// Avoid creating too many unique label values
		if code >= 200 && code < 300 {
			return "2xx"
		} else if code >= 400 && code < 500 {
			return "4xx"
		} else if code >= 500 {
			return "5xx"
		}
		return "other"
	}
}
