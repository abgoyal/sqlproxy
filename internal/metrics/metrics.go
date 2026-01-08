package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"sql-proxy/internal/logging"
)

// Config for metrics collection
type Config struct {
	Enabled        bool   `yaml:"enabled"`
	FilePath       string `yaml:"file_path"`        // Where to write metrics
	IntervalSec    int    `yaml:"interval_sec"`     // Export interval (default 300 = 5 min)
	RetainFiles    int    `yaml:"retain_files"`     // Number of metric files to retain
}

// RequestMetrics captures metrics for a single request
type RequestMetrics struct {
	RequestID       string        `json:"request_id"`
	Endpoint        string        `json:"endpoint"`
	QueryName       string        `json:"query_name"`
	StartTime       time.Time     `json:"start_time"`
	TotalDuration   time.Duration `json:"total_duration_ms"`
	QueryDuration   time.Duration `json:"query_duration_ms"`
	RowCount        int           `json:"row_count"`
	StatusCode      int           `json:"status_code"`
	TimeoutUsed     int           `json:"timeout_sec"`
	Error           string        `json:"error,omitempty"`
	ParamCount      int           `json:"param_count"`
}

// EndpointStats aggregates stats for an endpoint
type EndpointStats struct {
	Endpoint        string  `json:"endpoint"`
	QueryName       string  `json:"query_name"`
	RequestCount    int64   `json:"request_count"`
	ErrorCount      int64   `json:"error_count"`
	TimeoutCount    int64   `json:"timeout_count"`
	TotalRows       int64   `json:"total_rows"`
	AvgDurationMs   float64 `json:"avg_duration_ms"`
	MaxDurationMs   float64 `json:"max_duration_ms"`
	MinDurationMs   float64 `json:"min_duration_ms"`
	P50DurationMs   float64 `json:"p50_duration_ms"`
	P95DurationMs   float64 `json:"p95_duration_ms"`
	P99DurationMs   float64 `json:"p99_duration_ms"`
	AvgQueryMs      float64 `json:"avg_query_ms"`
	AvgRowsPerReq   float64 `json:"avg_rows_per_request"`
}

// RuntimeStats captures Go runtime metrics
type RuntimeStats struct {
	GoVersion       string `json:"go_version"`
	NumGoroutine    int    `json:"goroutines"`
	NumCPU          int    `json:"num_cpu"`

	// Memory stats (in bytes)
	MemAllocBytes   uint64 `json:"mem_alloc_bytes"`      // Currently allocated heap
	MemTotalAlloc   uint64 `json:"mem_total_alloc"`      // Cumulative bytes allocated
	MemSysBytes     uint64 `json:"mem_sys_bytes"`        // Total memory from OS
	MemHeapObjects  uint64 `json:"mem_heap_objects"`     // Number of allocated heap objects

	// GC stats
	NumGC           uint32 `json:"gc_runs"`              // Number of completed GC cycles
	GCPauseNs       uint64 `json:"gc_pause_total_ns"`    // Total GC pause time
	LastGCPauseNs   uint64 `json:"gc_last_pause_ns"`     // Last GC pause duration
}

// Snapshot represents metrics at a point in time
type Snapshot struct {
	Timestamp       time.Time                 `json:"timestamp"`
	PeriodStartTime time.Time                 `json:"period_start"`
	PeriodEndTime   time.Time                 `json:"period_end"`
	UptimeSec       int64                     `json:"uptime_sec"`
	TotalRequests   int64                     `json:"total_requests"`
	TotalErrors     int64                     `json:"total_errors"`
	TotalTimeouts   int64                     `json:"total_timeouts"`
	Endpoints       map[string]*EndpointStats `json:"endpoints"`
	DBHealthy       bool                      `json:"db_healthy"`
	Runtime         RuntimeStats              `json:"runtime"`
}

// Collector collects and exports metrics
type Collector struct {
	config      Config
	startTime   time.Time

	mu          sync.RWMutex
	requests    []RequestMetrics // Buffer of recent requests

	// Lifetime counters
	totalRequests int64
	totalErrors   int64
	totalTimeouts int64

	// For periodic export
	lastExport  time.Time
	cancelFunc  context.CancelFunc
	wg          sync.WaitGroup

	// DB health checker
	dbHealthChecker func() bool
}

var defaultCollector *Collector

// Init initializes the global metrics collector
func Init(cfg Config, dbHealthChecker func() bool) error {
	collector, err := New(cfg, dbHealthChecker)
	if err != nil {
		return err
	}
	defaultCollector = collector
	return nil
}

// New creates a new metrics collector
func New(cfg Config, dbHealthChecker func() bool) (*Collector, error) {
	if cfg.IntervalSec == 0 {
		cfg.IntervalSec = 300 // 5 minutes default
	}
	if cfg.RetainFiles == 0 {
		cfg.RetainFiles = 288 // 24 hours at 5-minute intervals
	}

	c := &Collector{
		config:          cfg,
		startTime:       time.Now(),
		requests:        make([]RequestMetrics, 0, 1000),
		lastExport:      time.Now(),
		dbHealthChecker: dbHealthChecker,
	}

	if cfg.Enabled && cfg.FilePath != "" {
		// Ensure directory exists
		dir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}

		// Start periodic export
		ctx, cancel := context.WithCancel(context.Background())
		c.cancelFunc = cancel
		c.wg.Add(1)
		go c.exportLoop(ctx)
	}

	return c, nil
}

// Record records metrics for a completed request
func (c *Collector) Record(m RequestMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.requests = append(c.requests, m)
	c.totalRequests++

	if m.Error != "" {
		c.totalErrors++
	}
	if m.StatusCode == 504 { // Gateway timeout
		c.totalTimeouts++
	}
}

// GetSnapshot returns current metrics snapshot
func (c *Collector) GetSnapshot() *Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()

	// Collect Go runtime stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	snap := &Snapshot{
		Timestamp:       now,
		PeriodStartTime: c.lastExport,
		PeriodEndTime:   now,
		UptimeSec:       int64(now.Sub(c.startTime).Seconds()),
		TotalRequests:   c.totalRequests,
		TotalErrors:     c.totalErrors,
		TotalTimeouts:   c.totalTimeouts,
		Endpoints:       make(map[string]*EndpointStats),
		DBHealthy:       c.dbHealthChecker != nil && c.dbHealthChecker(),
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

	// Aggregate by endpoint
	for _, req := range c.requests {
		stats, exists := snap.Endpoints[req.Endpoint]
		if !exists {
			stats = &EndpointStats{
				Endpoint:      req.Endpoint,
				QueryName:     req.QueryName,
				MinDurationMs: float64(req.TotalDuration.Milliseconds()),
			}
			snap.Endpoints[req.Endpoint] = stats
		}

		stats.RequestCount++
		stats.TotalRows += int64(req.RowCount)

		durationMs := float64(req.TotalDuration.Milliseconds())
		queryMs := float64(req.QueryDuration.Milliseconds())

		// Track min/max
		if durationMs > stats.MaxDurationMs {
			stats.MaxDurationMs = durationMs
		}
		if durationMs < stats.MinDurationMs {
			stats.MinDurationMs = durationMs
		}

		// Running averages (will be finalized below)
		stats.AvgDurationMs += durationMs
		stats.AvgQueryMs += queryMs

		if req.Error != "" {
			stats.ErrorCount++
		}
		if req.StatusCode == 504 {
			stats.TimeoutCount++
		}
	}

	// Finalize averages and calculate percentiles
	for endpoint, stats := range snap.Endpoints {
		if stats.RequestCount > 0 {
			stats.AvgDurationMs /= float64(stats.RequestCount)
			stats.AvgQueryMs /= float64(stats.RequestCount)
			stats.AvgRowsPerReq = float64(stats.TotalRows) / float64(stats.RequestCount)
		}

		// Calculate percentiles
		var durations []float64
		for _, req := range c.requests {
			if req.Endpoint == endpoint {
				durations = append(durations, float64(req.TotalDuration.Milliseconds()))
			}
		}
		if len(durations) > 0 {
			sort.Float64s(durations)
			stats.P50DurationMs = percentile(durations, 50)
			stats.P95DurationMs = percentile(durations, 95)
			stats.P99DurationMs = percentile(durations, 99)
		}
	}

	return snap
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p / 100)
	return sorted[idx]
}

// Export writes metrics to file
func (c *Collector) Export() error {
	snap := c.GetSnapshot()

	// Generate timestamped filename
	timestamp := time.Now().Format("20060102-150405")
	filename := c.config.FilePath
	ext := filepath.Ext(filename)
	base := filename[:len(filename)-len(ext)]
	timestampedFile := fmt.Sprintf("%s-%s%s", base, timestamp, ext)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(timestampedFile, data, 0644); err != nil {
		return err
	}

	// Also write to the base filename (latest)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}

	// Log export
	logging.Info("metrics_exported", map[string]any{
		"file":           timestampedFile,
		"total_requests": snap.TotalRequests,
		"period_requests": len(c.requests),
		"endpoints":      len(snap.Endpoints),
	})

	// Clear buffer and update export time
	c.mu.Lock()
	c.requests = c.requests[:0]
	c.lastExport = time.Now()
	c.mu.Unlock()

	// Cleanup old files
	c.cleanupOldFiles()

	return nil
}

func (c *Collector) cleanupOldFiles() {
	dir := filepath.Dir(c.config.FilePath)
	base := filepath.Base(c.config.FilePath)
	ext := filepath.Ext(base)
	prefix := base[:len(base)-len(ext)]

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var metricFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if len(name) > len(prefix) && name[:len(prefix)] == prefix && name != base {
			metricFiles = append(metricFiles, filepath.Join(dir, name))
		}
	}

	// Sort by name (which includes timestamp) and remove oldest
	sort.Strings(metricFiles)
	for len(metricFiles) > c.config.RetainFiles {
		os.Remove(metricFiles[0])
		metricFiles = metricFiles[1:]
	}
}

func (c *Collector) exportLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(time.Duration(c.config.IntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final export on shutdown
			c.Export()
			return
		case <-ticker.C:
			if err := c.Export(); err != nil {
				logging.Error("metrics_export_failed", map[string]any{
					"error": err.Error(),
				})
			}
		}
	}
}

// Close stops the collector and exports final metrics
func (c *Collector) Close() error {
	if c.cancelFunc != nil {
		c.cancelFunc()
		c.wg.Wait()
	}
	return nil
}

// Package-level functions

func Record(m RequestMetrics) {
	if defaultCollector != nil {
		defaultCollector.Record(m)
	}
}

func GetSnapshot() *Snapshot {
	if defaultCollector != nil {
		return defaultCollector.GetSnapshot()
	}
	return nil
}

func Close() error {
	if defaultCollector != nil {
		return defaultCollector.Close()
	}
	return nil
}
