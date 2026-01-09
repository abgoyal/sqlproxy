package metrics

import (
	"testing"
	"time"
)

// BenchmarkRecord measures single metric recording throughput
func BenchmarkRecord(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	m := RequestMetrics{
		Endpoint:      "/api/test",
		QueryName:     "test_query",
		TotalDuration: time.Millisecond * 100,
		QueryDuration: time.Millisecond * 50,
		RowCount:      10,
		StatusCode:    200,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Record(m)
	}
}

// BenchmarkRecord_Concurrent measures parallel metric recording with RunParallel
func BenchmarkRecord_Concurrent(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	m := RequestMetrics{
		Endpoint:      "/api/test",
		QueryName:     "test_query",
		TotalDuration: time.Millisecond * 100,
		QueryDuration: time.Millisecond * 50,
		RowCount:      10,
		StatusCode:    200,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Record(m)
		}
	})
}

// BenchmarkRecord_MultipleEndpoints measures recording across 5 different endpoints
func BenchmarkRecord_MultipleEndpoints(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	endpoints := []string{"/api/a", "/api/b", "/api/c", "/api/d", "/api/e"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := RequestMetrics{
			Endpoint:      endpoints[i%len(endpoints)],
			QueryName:     "test_query",
			TotalDuration: time.Millisecond * 100,
			RowCount:      10,
			StatusCode:    200,
		}
		Record(m)
	}
}

// BenchmarkGetSnapshot measures snapshot retrieval with 10 pre-populated endpoints
func BenchmarkGetSnapshot(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	// Pre-populate with some data
	for i := 0; i < 10; i++ {
		Record(RequestMetrics{
			Endpoint:      "/api/test" + string(rune('a'+i)),
			QueryName:     "test_query",
			TotalDuration: time.Millisecond * 100,
			RowCount:      10,
			StatusCode:    200,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetSnapshot()
	}
}

// BenchmarkGetSnapshot_Concurrent measures parallel snapshot reads under load
func BenchmarkGetSnapshot_Concurrent(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	// Pre-populate with some data
	for i := 0; i < 10; i++ {
		Record(RequestMetrics{
			Endpoint:      "/api/test" + string(rune('a'+i)),
			QueryName:     "test_query",
			TotalDuration: time.Millisecond * 100,
			RowCount:      10,
			StatusCode:    200,
		})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = GetSnapshot()
		}
	})
}

// BenchmarkRecord_WithError measures error metric recording with status 500
func BenchmarkRecord_WithError(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	m := RequestMetrics{
		Endpoint:      "/api/error",
		QueryName:     "bad_query",
		TotalDuration: time.Millisecond * 50,
		StatusCode:    500,
		Error:         "database error occurred",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Record(m)
	}
}

// BenchmarkRecord_WithTimeout measures timeout metric recording with status 504
func BenchmarkRecord_WithTimeout(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	m := RequestMetrics{
		Endpoint:      "/api/slow",
		QueryName:     "slow_query",
		TotalDuration: time.Second * 30,
		StatusCode:    504,
		Error:         "timeout",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Record(m)
	}
}

// BenchmarkInit measures collector initialization throughput
func BenchmarkInit(b *testing.B) {
	checker := func() bool { return true }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		defaultCollector = nil
		Init(checker)
	}
}

// BenchmarkMixedWorkload simulates real-world usage: 90% success, 10% error, 1% snapshots
func BenchmarkMixedWorkload(b *testing.B) {
	defaultCollector = nil
	Init(func() bool { return true })

	endpoints := []string{"/api/users", "/api/products", "/api/orders"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 90% success, 10% error
			statusCode := 200
			err := ""
			if i%10 == 0 {
				statusCode = 500
				err = "error"
			}

			Record(RequestMetrics{
				Endpoint:      endpoints[i%len(endpoints)],
				QueryName:     "query_" + endpoints[i%len(endpoints)],
				TotalDuration: time.Millisecond * time.Duration(50+i%100),
				QueryDuration: time.Millisecond * time.Duration(20+i%50),
				RowCount:      i % 100,
				StatusCode:    statusCode,
				Error:         err,
			})

			// Occasionally get snapshot (1% of requests)
			if i%100 == 0 {
				_ = GetSnapshot()
			}

			i++
		}
	})
}
