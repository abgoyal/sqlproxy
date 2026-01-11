package metrics

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestInit verifies metrics collector initialization with health checker
func TestInit(t *testing.T) {
	// Reset for test
	defaultCollector = nil

	checker := func() bool { return true }
	Init(checker, "1.0.0", "2024-01-01T00:00:00Z")

	if defaultCollector == nil {
		t.Fatal("expected collector to be initialized")
	}

	if defaultCollector.dbHealthChecker == nil {
		t.Error("expected dbHealthChecker to be set")
	}

	if defaultCollector.endpoints == nil {
		t.Error("expected endpoints map to be initialized")
	}
}

// TestRecord_NoCollector verifies Record handles nil collector without panic
func TestRecord_NoCollector(t *testing.T) {
	// Reset collector
	defaultCollector = nil

	// Should not panic when no collector
	Record(RequestMetrics{
		Endpoint:      "/test",
		QueryName:     "test",
		TotalDuration: time.Millisecond * 100,
		RowCount:      5,
		StatusCode:    200,
	})
}

// TestRecord tests request metric recording and snapshot retrieval
func TestRecord(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	// Record first request
	Record(RequestMetrics{
		Endpoint:      "/api/users",
		QueryName:     "list_users",
		TotalDuration: time.Millisecond * 100,
		QueryDuration: time.Millisecond * 50,
		RowCount:      10,
		StatusCode:    200,
	})

	snap := GetSnapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("expected TotalRequests=1, got %d", snap.TotalRequests)
	}
	if snap.TotalErrors != 0 {
		t.Errorf("expected TotalErrors=0, got %d", snap.TotalErrors)
	}

	ep := snap.Endpoints["/api/users"]
	if ep == nil {
		t.Fatal("expected endpoint stats")
	}
	if ep.RequestCount != 1 {
		t.Errorf("expected RequestCount=1, got %d", ep.RequestCount)
	}
	if ep.TotalRows != 10 {
		t.Errorf("expected TotalRows=10, got %d", ep.TotalRows)
	}
}

// TestRecord_Error tests error counter increment on 500 status
func TestRecord_Error(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	Record(RequestMetrics{
		Endpoint:      "/api/error",
		QueryName:     "bad_query",
		TotalDuration: time.Millisecond * 50,
		StatusCode:    500,
		Error:         "database error",
	})

	snap := GetSnapshot()
	if snap.TotalErrors != 1 {
		t.Errorf("expected TotalErrors=1, got %d", snap.TotalErrors)
	}

	ep := snap.Endpoints["/api/error"]
	if ep == nil {
		t.Fatal("expected endpoint stats")
	}
	if ep.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1, got %d", ep.ErrorCount)
	}
}

// TestRecord_Timeout tests timeout counter increment on 504 status
func TestRecord_Timeout(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	Record(RequestMetrics{
		Endpoint:      "/api/slow",
		QueryName:     "slow_query",
		TotalDuration: time.Second * 30,
		StatusCode:    504,
		Error:         "timeout",
	})

	snap := GetSnapshot()
	if snap.TotalTimeouts != 1 {
		t.Errorf("expected TotalTimeouts=1, got %d", snap.TotalTimeouts)
	}
	if snap.TotalErrors != 1 {
		t.Errorf("expected TotalErrors=1, got %d", snap.TotalErrors)
	}

	ep := snap.Endpoints["/api/slow"]
	if ep.TimeoutCount != 1 {
		t.Errorf("expected TimeoutCount=1, got %d", ep.TimeoutCount)
	}
}

// TestRecord_MinMaxDuration tests min/max duration tracking across requests
func TestRecord_MinMaxDuration(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	// Record multiple requests with different durations
	durations := []time.Duration{
		time.Millisecond * 100,
		time.Millisecond * 50,  // min
		time.Millisecond * 200, // max
	}

	for _, d := range durations {
		Record(RequestMetrics{
			Endpoint:      "/api/test",
			QueryName:     "test",
			TotalDuration: d,
			StatusCode:    200,
		})
	}

	snap := GetSnapshot()
	ep := snap.Endpoints["/api/test"]

	if ep.MinDurationMs != 50 {
		t.Errorf("expected MinDurationMs=50, got %f", ep.MinDurationMs)
	}
	if ep.MaxDurationMs != 200 {
		t.Errorf("expected MaxDurationMs=200, got %f", ep.MaxDurationMs)
	}
	if ep.RequestCount != 3 {
		t.Errorf("expected RequestCount=3, got %d", ep.RequestCount)
	}
}

// TestRecord_Averages tests average duration calculation for total and query times
func TestRecord_Averages(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	// Record two requests: 100ms and 200ms total, 50ms and 100ms query
	Record(RequestMetrics{
		Endpoint:      "/api/avg",
		QueryName:     "avg_test",
		TotalDuration: time.Millisecond * 100,
		QueryDuration: time.Millisecond * 50,
		StatusCode:    200,
	})
	Record(RequestMetrics{
		Endpoint:      "/api/avg",
		QueryName:     "avg_test",
		TotalDuration: time.Millisecond * 200,
		QueryDuration: time.Millisecond * 100,
		StatusCode:    200,
	})

	snap := GetSnapshot()
	ep := snap.Endpoints["/api/avg"]

	// Average total: (100 + 200) / 2 = 150
	if ep.AvgDurationMs != 150 {
		t.Errorf("expected AvgDurationMs=150, got %f", ep.AvgDurationMs)
	}

	// Average query: (50 + 100) / 2 = 75
	if ep.AvgQueryMs != 75 {
		t.Errorf("expected AvgQueryMs=75, got %f", ep.AvgQueryMs)
	}
}

// TestGetSnapshot_NoCollector verifies nil return when collector not initialized
func TestGetSnapshot_NoCollector(t *testing.T) {
	defaultCollector = nil

	snap := GetSnapshot()
	if snap != nil {
		t.Error("expected nil snapshot when collector not initialized")
	}
}

// TestGetSnapshot_RuntimeStats verifies Go runtime stats in snapshot
func TestGetSnapshot_RuntimeStats(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	snap := GetSnapshot()

	if snap.Runtime.GoVersion != runtime.Version() {
		t.Errorf("expected GoVersion=%s, got %s", runtime.Version(), snap.Runtime.GoVersion)
	}
	if snap.Runtime.NumCPU != runtime.NumCPU() {
		t.Errorf("expected NumCPU=%d, got %d", runtime.NumCPU(), snap.Runtime.NumCPU)
	}
	if snap.Runtime.NumGoroutine <= 0 {
		t.Error("expected positive goroutine count")
	}
	if snap.Runtime.MemAllocBytes == 0 {
		t.Error("expected non-zero memory allocation")
	}
}

// TestGetSnapshot_Uptime tests uptime calculation in snapshot
func TestGetSnapshot_Uptime(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	// Wait a bit
	time.Sleep(time.Millisecond * 100)

	snap := GetSnapshot()
	if snap.UptimeSec < 0 {
		t.Errorf("expected non-negative uptime, got %d", snap.UptimeSec)
	}
}

// TestGetSnapshot_DBHealth tests database health status via checker function
func TestGetSnapshot_DBHealth(t *testing.T) {
	// Test with healthy DB
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	snap := GetSnapshot()
	if !snap.DBHealthy {
		t.Error("expected DBHealthy=true")
	}

	// Test with unhealthy DB
	defaultCollector = nil
	Init(func() bool { return false }, "1.0.0", "2024-01-01T00:00:00Z")

	snap = GetSnapshot()
	if snap.DBHealthy {
		t.Error("expected DBHealthy=false")
	}

	// Test with nil checker
	defaultCollector = nil
	Init(nil, "", "")

	snap = GetSnapshot()
	if snap.DBHealthy {
		t.Error("expected DBHealthy=false with nil checker")
	}
}

// TestRecord_Concurrent tests thread-safe metric recording with 100 goroutines
func TestRecord_Concurrent(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	var wg sync.WaitGroup
	numGoroutines := 100
	requestsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				Record(RequestMetrics{
					Endpoint:      "/api/concurrent",
					QueryName:     "concurrent_test",
					TotalDuration: time.Millisecond * time.Duration(10+id),
					RowCount:      1,
					StatusCode:    200,
				})
			}
		}(i)
	}

	wg.Wait()

	snap := GetSnapshot()
	expectedTotal := int64(numGoroutines * requestsPerGoroutine)

	if snap.TotalRequests != expectedTotal {
		t.Errorf("expected TotalRequests=%d, got %d", expectedTotal, snap.TotalRequests)
	}

	ep := snap.Endpoints["/api/concurrent"]
	if ep.RequestCount != expectedTotal {
		t.Errorf("expected endpoint RequestCount=%d, got %d", expectedTotal, ep.RequestCount)
	}
	if ep.TotalRows != expectedTotal {
		t.Errorf("expected TotalRows=%d, got %d", expectedTotal, ep.TotalRows)
	}
}

// TestRecord_MultipleEndpoints tests separate stats tracking per endpoint
func TestRecord_MultipleEndpoints(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	endpoints := []string{"/api/a", "/api/b", "/api/c"}

	for _, ep := range endpoints {
		Record(RequestMetrics{
			Endpoint:      ep,
			QueryName:     "query_" + ep,
			TotalDuration: time.Millisecond * 100,
			RowCount:      5,
			StatusCode:    200,
		})
	}

	snap := GetSnapshot()
	if len(snap.Endpoints) != 3 {
		t.Errorf("expected 3 endpoints, got %d", len(snap.Endpoints))
	}

	for _, ep := range endpoints {
		stats := snap.Endpoints[ep]
		if stats == nil {
			t.Errorf("missing stats for endpoint %s", ep)
		} else if stats.RequestCount != 1 {
			t.Errorf("endpoint %s: expected RequestCount=1, got %d", ep, stats.RequestCount)
		}
	}
}

// TestEndpointStats_Fields verifies all endpoint stat fields are populated
func TestEndpointStats_Fields(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	Record(RequestMetrics{
		Endpoint:      "/api/fields",
		QueryName:     "field_test",
		TotalDuration: time.Millisecond * 100,
		QueryDuration: time.Millisecond * 75,
		RowCount:      42,
		StatusCode:    200,
	})

	snap := GetSnapshot()
	ep := snap.Endpoints["/api/fields"]

	if ep.Endpoint != "/api/fields" {
		t.Errorf("expected Endpoint=/api/fields, got %s", ep.Endpoint)
	}
	if ep.QueryName != "field_test" {
		t.Errorf("expected QueryName=field_test, got %s", ep.QueryName)
	}
	if ep.TotalRows != 42 {
		t.Errorf("expected TotalRows=42, got %d", ep.TotalRows)
	}
}

// TestSnapshot_Timestamp verifies snapshot timestamp is set correctly
func TestSnapshot_Timestamp(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	before := time.Now()
	snap := GetSnapshot()
	after := time.Now()

	if snap.Timestamp.Before(before) || snap.Timestamp.After(after) {
		t.Errorf("snapshot timestamp %v not between %v and %v", snap.Timestamp, before, after)
	}
}

// TestSnapshot_Version verifies version and buildTime are included in snapshot
func TestSnapshot_Version(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "2.0.0", "2024-06-15T12:00:00Z")

	snap := GetSnapshot()
	if snap.Version != "2.0.0" {
		t.Errorf("expected Version=2.0.0, got %s", snap.Version)
	}
	if snap.BuildTime != "2024-06-15T12:00:00Z" {
		t.Errorf("expected BuildTime=2024-06-15T12:00:00Z, got %s", snap.BuildTime)
	}
}

// TestSnapshot_EmptyVersion verifies empty version/buildTime are handled correctly
func TestSnapshot_EmptyVersion(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "", "")

	snap := GetSnapshot()
	if snap.Version != "" {
		t.Errorf("expected empty Version, got %s", snap.Version)
	}
	if snap.BuildTime != "" {
		t.Errorf("expected empty BuildTime, got %s", snap.BuildTime)
	}
}

// TestReset verifies metrics are cleared while preserving configuration
func TestReset(t *testing.T) {
	defaultCollector = nil
	Init(func() bool { return true }, "1.0.0", "2024-01-01T00:00:00Z")

	// Record some requests
	for i := 0; i < 10; i++ {
		Record(RequestMetrics{
			Endpoint:      "/api/test",
			QueryName:     "test",
			TotalDuration: time.Millisecond * 100,
			RowCount:      5,
			StatusCode:    200,
		})
	}

	snap := GetSnapshot()
	if snap.TotalRequests != 10 {
		t.Errorf("expected 10 requests before reset, got %d", snap.TotalRequests)
	}

	// Reset metrics
	Reset()

	snap = GetSnapshot()
	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 requests after reset, got %d", snap.TotalRequests)
	}
	if len(snap.Endpoints) != 0 {
		t.Errorf("expected 0 endpoints after reset, got %d", len(snap.Endpoints))
	}
	// Version should be preserved
	if snap.Version != "1.0.0" {
		t.Errorf("expected version to be preserved, got %s", snap.Version)
	}
	// Uptime should be reset (close to 0)
	if snap.UptimeSec > 1 {
		t.Errorf("expected uptime to be reset, got %d", snap.UptimeSec)
	}
}

// TestReset_NoCollector verifies Reset handles nil collector
func TestReset_NoCollector(t *testing.T) {
	defaultCollector = nil
	// Should not panic
	Reset()
}
