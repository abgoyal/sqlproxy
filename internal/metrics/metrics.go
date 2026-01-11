package metrics

import (
	"runtime"
	"sync"
	"time"
)

// RequestMetrics captures metrics for a single request
type RequestMetrics struct {
	Endpoint      string
	QueryName     string
	TotalDuration time.Duration
	QueryDuration time.Duration
	RowCount      int
	StatusCode    int
	Error         string
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

// Collector collects metrics
type Collector struct {
	startTime                 time.Time
	version                   string
	buildTime                 string
	dbHealthChecker           func() bool
	cacheSnapshotProvider     CacheSnapshotProvider
	rateLimitSnapshotProvider RateLimitSnapshotProvider

	mu            sync.RWMutex
	totalRequests int64
	totalErrors   int64
	totalTimeouts int64
	endpoints     map[string]*endpointData
}

type endpointData struct {
	queryName     string
	requestCount  int64
	errorCount    int64
	timeoutCount  int64
	totalRows     int64
	totalDuration float64 // sum of durations for avg
	totalQueryMs  float64
	maxDuration   float64
	minDuration   float64
}

var defaultCollector *Collector

// Init initializes the global metrics collector
func Init(dbHealthChecker func() bool, version, buildTime string) {
	defaultCollector = &Collector{
		startTime:       time.Now(),
		version:         version,
		buildTime:       buildTime,
		dbHealthChecker: dbHealthChecker,
		endpoints:       make(map[string]*endpointData),
	}
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

// Reset clears all metrics counters while preserving configuration.
// This is useful for operational purposes like starting fresh metrics
// after a maintenance window.
func Reset() {
	if defaultCollector == nil {
		return
	}
	c := defaultCollector

	c.mu.Lock()
	defer c.mu.Unlock()

	c.startTime = time.Now()
	c.totalRequests = 0
	c.totalErrors = 0
	c.totalTimeouts = 0
	c.endpoints = make(map[string]*endpointData)
}

// Record records metrics for a completed request
func Record(m RequestMetrics) {
	if defaultCollector == nil {
		return
	}
	c := defaultCollector

	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalRequests++
	if m.Error != "" {
		c.totalErrors++
	}
	if m.StatusCode == 504 {
		c.totalTimeouts++
	}

	durationMs := float64(m.TotalDuration.Milliseconds())
	queryMs := float64(m.QueryDuration.Milliseconds())

	ep, exists := c.endpoints[m.Endpoint]
	if !exists {
		ep = &endpointData{
			queryName:   m.QueryName,
			minDuration: durationMs,
		}
		c.endpoints[m.Endpoint] = ep
	}

	ep.requestCount++
	ep.totalRows += int64(m.RowCount)
	ep.totalDuration += durationMs
	ep.totalQueryMs += queryMs

	if durationMs > ep.maxDuration {
		ep.maxDuration = durationMs
	}
	if durationMs < ep.minDuration {
		ep.minDuration = durationMs
	}
	if m.Error != "" {
		ep.errorCount++
	}
	if m.StatusCode == 504 {
		ep.timeoutCount++
	}
}

// GetSnapshot returns current metrics snapshot
func GetSnapshot() *Snapshot {
	if defaultCollector == nil {
		return nil
	}
	c := defaultCollector

	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	snap := &Snapshot{
		Timestamp:     now,
		Version:       c.version,
		BuildTime:     c.buildTime,
		UptimeSec:     int64(now.Sub(c.startTime).Seconds()),
		TotalRequests: c.totalRequests,
		TotalErrors:   c.totalErrors,
		TotalTimeouts: c.totalTimeouts,
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

	for endpoint, ep := range c.endpoints {
		stats := &EndpointStats{
			Endpoint:      endpoint,
			QueryName:     ep.queryName,
			RequestCount:  ep.requestCount,
			ErrorCount:    ep.errorCount,
			TimeoutCount:  ep.timeoutCount,
			TotalRows:     ep.totalRows,
			MaxDurationMs: ep.maxDuration,
			MinDurationMs: ep.minDuration,
		}
		if ep.requestCount > 0 {
			stats.AvgDurationMs = ep.totalDuration / float64(ep.requestCount)
			stats.AvgQueryMs = ep.totalQueryMs / float64(ep.requestCount)
		}
		snap.Endpoints[endpoint] = stats
	}

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
