package ratelimit

import (
	"testing"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/tmpl"
)

func TestNew(t *testing.T) {
	engine := tmpl.New()

	tests := []struct {
		name    string
		pools   []config.RateLimitPoolConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty pools",
			pools:   nil,
			wantErr: false,
		},
		{
			name: "valid single pool",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.ClientIP}}"},
			},
			wantErr: false,
		},
		{
			name: "valid multiple pools",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.ClientIP}}"},
				{Name: "api", RequestsPerSecond: 100, Burst: 200, Key: "{{.ClientIP}}"},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			pools: []config.RateLimitPoolConfig{
				{RequestsPerSecond: 10, Burst: 20, Key: "{{.ClientIP}}"},
			},
			wantErr: true,
			errMsg:  "missing name",
		},
		{
			name: "duplicate name",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.ClientIP}}"},
				{Name: "default", RequestsPerSecond: 20, Burst: 40, Key: "{{.ClientIP}}"},
			},
			wantErr: true,
			errMsg:  "duplicate",
		},
		{
			name: "zero requests_per_second",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 0, Burst: 20, Key: "{{.ClientIP}}"},
			},
			wantErr: true,
			errMsg:  "requests_per_second must be positive",
		},
		{
			name: "zero burst",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 0, Key: "{{.ClientIP}}"},
			},
			wantErr: true,
			errMsg:  "burst must be positive",
		},
		{
			name: "missing key",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20},
			},
			wantErr: true,
			errMsg:  "key template required",
		},
		{
			name: "invalid template",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.Invalid"},
			},
			wantErr: true,
			errMsg:  "invalid key template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := New(tt.pools, engine)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if l == nil {
				t.Fatal("limiter is nil")
			}
		})
	}
}

func TestAllow_NoLimits(t *testing.T) {
	engine := tmpl.New()
	l, err := New(nil, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}

	allowed, err := l.Allow(nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with no limits")
	}

	allowed, err = l.Allow([]config.QueryRateLimitConfig{}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with empty limits")
	}
}

func TestAllow_NamedPool(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "strict", RequestsPerSecond: 1, Burst: 2, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}
	limits := []config.QueryRateLimitConfig{
		{Pool: "strict"},
	}

	// First two requests should be allowed (burst=2)
	for i := 0; i < 2; i++ {
		allowed, err := l.Allow(limits, ctx)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("request %d: expected allowed", i)
		}
	}

	// Third request should be denied (burst exhausted)
	allowed, err := l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected denied after burst exhausted")
	}
}

func TestAllow_InlineConfig(t *testing.T) {
	engine := tmpl.New()
	l, err := New(nil, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}
	limits := []config.QueryRateLimitConfig{
		{RequestsPerSecond: 1, Burst: 1, Key: "{{.ClientIP}}"},
	}

	// First request allowed
	allowed, err := l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected first request allowed")
	}

	// Second request denied
	allowed, err = l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected second request denied")
	}
}

func TestAllow_MultiplePools(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "pool1", RequestsPerSecond: 10, Burst: 10, Key: "{{.ClientIP}}"},
		{Name: "pool2", RequestsPerSecond: 1, Burst: 1, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}

	// Both pools must pass
	limits := []config.QueryRateLimitConfig{
		{Pool: "pool1"},
		{Pool: "pool2"},
	}

	// First request allowed (both pools pass)
	allowed, err := l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed")
	}

	// Second request denied (pool2 exhausted, even though pool1 has capacity)
	allowed, err = l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected denied when any pool is exhausted")
	}
}

func TestAllow_DifferentClients(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "default", RequestsPerSecond: 1, Burst: 1, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	limits := []config.QueryRateLimitConfig{
		{Pool: "default"},
	}

	// Client 1 makes a request
	ctx1 := &tmpl.Context{ClientIP: "192.168.1.1"}
	allowed, err := l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("client1 first request should be allowed")
	}

	// Client 1 is now limited
	allowed, err = l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("client1 second request should be denied")
	}

	// Client 2 should still be allowed (separate bucket)
	ctx2 := &tmpl.Context{ClientIP: "192.168.1.2"}
	allowed, err = l.Allow(limits, ctx2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("client2 first request should be allowed")
	}
}

func TestAllow_HeaderBasedKey(t *testing.T) {
	engine := tmpl.New()
	// Use index function for headers with dashes
	pools := []config.RateLimitPoolConfig{
		{Name: "api", RequestsPerSecond: 1, Burst: 1, Key: `{{index .Header "X-Api-Key"}}`},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	limits := []config.QueryRateLimitConfig{
		{Pool: "api"},
	}

	// API key "key1"
	ctx1 := &tmpl.Context{
		ClientIP: "192.168.1.1",
		Header:   map[string]string{"X-Api-Key": "key1"},
	}
	allowed, err := l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("key1 first request should be allowed")
	}

	// Same API key is limited
	allowed, err = l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("key1 second request should be denied")
	}

	// Different API key has separate bucket
	ctx2 := &tmpl.Context{
		ClientIP: "192.168.1.1", // Same IP but different API key
		Header:   map[string]string{"X-Api-Key": "key2"},
	}
	allowed, err = l.Allow(limits, ctx2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("key2 first request should be allowed")
	}
}

func TestAllow_MissingTemplateData(t *testing.T) {
	engine := tmpl.New()
	// Use require function to enforce strict key checking
	pools := []config.RateLimitPoolConfig{
		{Name: "api", RequestsPerSecond: 10, Burst: 10, Key: `{{require .Header "X-Api-Key"}}`},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	limits := []config.QueryRateLimitConfig{
		{Pool: "api"},
	}

	// Missing header should cause error (not silently fallback)
	ctx := &tmpl.Context{
		ClientIP: "192.168.1.1",
		Header:   map[string]string{}, // No X-Api-Key
	}
	_, err = l.Allow(limits, ctx)
	if err == nil {
		t.Error("expected error for missing template data")
	}
}

func TestAllow_NonexistentPool(t *testing.T) {
	engine := tmpl.New()
	l, err := New(nil, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}
	limits := []config.QueryRateLimitConfig{
		{Pool: "nonexistent"},
	}

	_, err = l.Allow(limits, ctx)
	if err == nil {
		t.Error("expected error for nonexistent pool")
	}
}

func TestMetrics(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "default", RequestsPerSecond: 1, Burst: 2, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}
	limits := []config.QueryRateLimitConfig{
		{Pool: "default"},
	}

	// Make 3 requests: 2 allowed, 1 denied
	for i := 0; i < 3; i++ {
		l.Allow(limits, ctx)
	}

	snap := l.Snapshot()
	if snap.TotalAllowed != 2 {
		t.Errorf("expected 2 allowed, got %d", snap.TotalAllowed)
	}
	if snap.TotalDenied != 1 {
		t.Errorf("expected 1 denied, got %d", snap.TotalDenied)
	}

	poolMetrics := snap.Pools["default"]
	if poolMetrics == nil {
		t.Fatal("missing pool metrics")
	}
	if poolMetrics.Allowed != 2 {
		t.Errorf("expected pool allowed=2, got %d", poolMetrics.Allowed)
	}
	if poolMetrics.Denied != 1 {
		t.Errorf("expected pool denied=1, got %d", poolMetrics.Denied)
	}
	if poolMetrics.ActiveBuckets != 1 {
		t.Errorf("expected 1 active bucket, got %d", poolMetrics.ActiveBuckets)
	}
}

func TestPoolNames(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "pool1", RequestsPerSecond: 10, Burst: 10, Key: "{{.ClientIP}}"},
		{Name: "pool2", RequestsPerSecond: 10, Burst: 10, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	names := l.PoolNames()
	if len(names) != 2 {
		t.Errorf("expected 2 pools, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["pool1"] || !nameSet["pool2"] {
		t.Errorf("unexpected pool names: %v", names)
	}
}

func TestGetPool(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	pool := l.GetPool("default")
	if pool == nil {
		t.Error("expected to find pool")
	}

	pool = l.GetPool("nonexistent")
	if pool != nil {
		t.Error("expected nil for nonexistent pool")
	}
}

func TestBucketCleanup(t *testing.T) {
	// This test verifies the cleanup logic by directly manipulating the pool
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "default", RequestsPerSecond: 10, Burst: 10, Key: "{{.ClientIP}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	pool := l.GetPool("default")
	pool.cleanEvery = 1 * time.Millisecond // Speed up for test

	ctx := &tmpl.Context{ClientIP: "192.168.1.1"}
	limits := []config.QueryRateLimitConfig{{Pool: "default"}}

	// Create a bucket
	l.Allow(limits, ctx)

	pool.bucketsMu.RLock()
	if len(pool.buckets) != 1 {
		t.Errorf("expected 1 bucket, got %d", len(pool.buckets))
	}
	pool.bucketsMu.RUnlock()

	// Mark bucket as old
	pool.bucketsMu.Lock()
	for _, b := range pool.buckets {
		b.lastUsed.Store(time.Now().Add(-15 * time.Minute).Unix())
	}
	pool.bucketsMu.Unlock()

	// Trigger cleanup
	time.Sleep(2 * time.Millisecond)
	pool.lastClean = time.Time{} // Force cleanup
	pool.maybeCleanup()

	pool.bucketsMu.RLock()
	if len(pool.buckets) != 0 {
		t.Errorf("expected 0 buckets after cleanup, got %d", len(pool.buckets))
	}
	pool.bucketsMu.RUnlock()
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
