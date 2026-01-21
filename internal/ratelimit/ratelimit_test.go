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
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: false,
		},
		{
			name: "valid multiple pools",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
				{Name: "api", RequestsPerSecond: 100, Burst: 200, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			pools: []config.RateLimitPoolConfig{
				{RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "missing name",
		},
		{
			name: "duplicate name",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
				{Name: "default", RequestsPerSecond: 20, Burst: 40, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "duplicate",
		},
		{
			name: "zero requests_per_second",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 0, Burst: 20, Key: "{{.trigger.client_ip}}"},
			},
			wantErr: true,
			errMsg:  "requests_per_second must be positive",
		},
		{
			name: "zero burst",
			pools: []config.RateLimitPoolConfig{
				{Name: "default", RequestsPerSecond: 10, Burst: 0, Key: "{{.trigger.client_ip}}"},
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

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}

	allowed, _, err := l.Allow(nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with no limits")
	}

	allowed, _, err = l.Allow([]config.RateLimitConfig{}, ctx)
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
		{Name: "strict", RequestsPerSecond: 1, Burst: 2, Key: "{{.trigger.client_ip}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	limits := []config.RateLimitConfig{
		{Pool: "strict"},
	}

	// First two requests should be allowed (burst=2)
	for i := 0; i < 2; i++ {
		allowed, _, err := l.Allow(limits, ctx)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("request %d: expected allowed", i)
		}
	}

	// Third request should be denied (burst exhausted)
	allowed, retryAfter, err := l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected denied after burst exhausted")
	}
	if retryAfter <= 0 {
		t.Error("expected positive retryAfter when denied")
	}
}

func TestAllow_InlineConfig(t *testing.T) {
	engine := tmpl.New()
	l, err := New(nil, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	limits := []config.RateLimitConfig{
		{RequestsPerSecond: 1, Burst: 1, Key: "{{.trigger.client_ip}}"},
	}

	// First request allowed
	allowed, _, err := l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected first request allowed")
	}

	// Second request denied
	allowed, _, err = l.Allow(limits, ctx)
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
		{Name: "pool1", RequestsPerSecond: 10, Burst: 10, Key: "{{.trigger.client_ip}}"},
		{Name: "pool2", RequestsPerSecond: 1, Burst: 1, Key: "{{.trigger.client_ip}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}

	// Both pools must pass
	limits := []config.RateLimitConfig{
		{Pool: "pool1"},
		{Pool: "pool2"},
	}

	// First request allowed (both pools pass)
	allowed, _, err := l.Allow(limits, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed")
	}

	// Second request denied (pool2 exhausted, even though pool1 has capacity)
	allowed, _, err = l.Allow(limits, ctx)
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
		{Name: "default", RequestsPerSecond: 1, Burst: 1, Key: "{{.trigger.client_ip}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	limits := []config.RateLimitConfig{
		{Pool: "default"},
	}

	// Client 1 makes a request
	ctx1 := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	allowed, _, err := l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("client1 first request should be allowed")
	}

	// Client 1 is now limited
	allowed, _, err = l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("client1 second request should be denied")
	}

	// Client 2 should still be allowed (separate bucket)
	ctx2 := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.2"}}
	allowed, _, err = l.Allow(limits, ctx2)
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
		{Name: "api", RequestsPerSecond: 1, Burst: 1, Key: `{{index .trigger.headers "X-Api-Key"}}`},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	limits := []config.RateLimitConfig{
		{Pool: "api"},
	}

	// API key "key1"
	ctx1 := &tmpl.Context{
		Trigger: &tmpl.TriggerContext{
			ClientIP: "192.168.1.1",
			Headers:  map[string]string{"X-Api-Key": "key1"},
		},
	}
	allowed, _, err := l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("key1 first request should be allowed")
	}

	// Same API key is limited
	allowed, _, err = l.Allow(limits, ctx1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("key1 second request should be denied")
	}

	// Different API key has separate bucket
	ctx2 := &tmpl.Context{
		Trigger: &tmpl.TriggerContext{
			ClientIP: "192.168.1.1", // Same IP but different API key
			Headers:  map[string]string{"X-Api-Key": "key2"},
		},
	}
	allowed, _, err = l.Allow(limits, ctx2)
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
		{Name: "api", RequestsPerSecond: 10, Burst: 10, Key: `{{require .trigger.headers "X-Api-Key"}}`},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	limits := []config.RateLimitConfig{
		{Pool: "api"},
	}

	// Missing header should cause error (not silently fallback)
	ctx := &tmpl.Context{
		Trigger: &tmpl.TriggerContext{
			ClientIP: "192.168.1.1",
			Headers:  map[string]string{}, // No X-Api-Key
		},
	}
	_, _, err = l.Allow(limits, ctx)
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

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	limits := []config.RateLimitConfig{
		{Pool: "nonexistent"},
	}

	_, _, err = l.Allow(limits, ctx)
	if err == nil {
		t.Error("expected error for nonexistent pool")
	}
}

func TestMetrics(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "default", RequestsPerSecond: 1, Burst: 2, Key: "{{.trigger.client_ip}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	limits := []config.RateLimitConfig{
		{Pool: "default"},
	}

	// Make 3 requests: 2 allowed, 1 denied
	for i := 0; i < 3; i++ {
		_, _, _ = l.Allow(limits, ctx)
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

// TestMetrics_InlinePool tests that inline rate limits also track metrics
func TestMetrics_InlinePool(t *testing.T) {
	engine := tmpl.New()
	// No named pools - use inline config
	l, err := New(nil, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	// Inline rate limit config
	limits := []config.RateLimitConfig{
		{RequestsPerSecond: 1, Burst: 2, Key: "{{.trigger.client_ip}}"},
	}

	// Make 3 requests: 2 allowed, 1 denied
	for i := 0; i < 3; i++ {
		_, _, _ = l.Allow(limits, ctx)
	}

	snap := l.Snapshot()
	if snap.TotalAllowed != 2 {
		t.Errorf("expected 2 allowed, got %d", snap.TotalAllowed)
	}
	if snap.TotalDenied != 1 {
		t.Errorf("expected 1 denied, got %d", snap.TotalDenied)
	}

	// Find the inline pool metrics (name starts with "_inline:")
	var inlinePoolMetrics *PoolMetrics
	for name, pm := range snap.Pools {
		if len(name) > 8 && name[:8] == "_inline:" {
			inlinePoolMetrics = pm
			break
		}
	}

	if inlinePoolMetrics == nil {
		t.Fatal("missing inline pool metrics")
	}
	if inlinePoolMetrics.Allowed != 2 {
		t.Errorf("expected inline pool allowed=2, got %d", inlinePoolMetrics.Allowed)
	}
	if inlinePoolMetrics.Denied != 1 {
		t.Errorf("expected inline pool denied=1, got %d", inlinePoolMetrics.Denied)
	}
}

func TestPoolNames(t *testing.T) {
	engine := tmpl.New()
	pools := []config.RateLimitPoolConfig{
		{Name: "pool1", RequestsPerSecond: 10, Burst: 10, Key: "{{.trigger.client_ip}}"},
		{Name: "pool2", RequestsPerSecond: 10, Burst: 10, Key: "{{.trigger.client_ip}}"},
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
		{Name: "default", RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
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
		{Name: "default", RequestsPerSecond: 10, Burst: 10, Key: "{{.trigger.client_ip}}"},
	}
	l, err := New(pools, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	pool := l.GetPool("default")
	pool.cleanEvery = 1 * time.Millisecond // Speed up for test

	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "192.168.1.1"}}
	limits := []config.RateLimitConfig{{Pool: "default"}}

	// Create a bucket
	_, _, _ = l.Allow(limits, ctx)

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

func TestReset(t *testing.T) {
	engine := tmpl.New()
	limiter, err := New([]config.RateLimitPoolConfig{
		{Name: "pool1", RequestsPerSecond: 10, Burst: 20, Key: "{{.trigger.client_ip}}"},
		{Name: "pool2", RequestsPerSecond: 5, Burst: 10, Key: "{{.trigger.client_ip}}"},
	}, engine)
	if err != nil {
		t.Fatalf("failed to create limiter: %v", err)
	}

	// Create some buckets by making requests
	ctx := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "1.2.3.4"}}
	limiter.Allow([]config.RateLimitConfig{{Pool: "pool1"}}, ctx)

	ctx2 := &tmpl.Context{Trigger: &tmpl.TriggerContext{ClientIP: "5.6.7.8"}}
	limiter.Allow([]config.RateLimitConfig{{Pool: "pool1"}}, ctx2)
	limiter.Allow([]config.RateLimitConfig{{Pool: "pool2"}}, ctx2)

	// Verify buckets exist
	snapshot := limiter.Snapshot()
	if snapshot.Pools["pool1"].ActiveBuckets != 2 {
		t.Errorf("expected 2 buckets in pool1, got %d", snapshot.Pools["pool1"].ActiveBuckets)
	}
	if snapshot.Pools["pool2"].ActiveBuckets != 1 {
		t.Errorf("expected 1 bucket in pool2, got %d", snapshot.Pools["pool2"].ActiveBuckets)
	}

	t.Run("ResetPool", func(t *testing.T) {
		count, err := limiter.ResetPool("pool1")
		if err != nil {
			t.Fatalf("ResetPool failed: %v", err)
		}
		if count != 2 {
			t.Errorf("expected 2 buckets cleared, got %d", count)
		}

		snapshot := limiter.Snapshot()
		if snapshot.Pools["pool1"].ActiveBuckets != 0 {
			t.Errorf("expected 0 buckets in pool1 after reset, got %d", snapshot.Pools["pool1"].ActiveBuckets)
		}
		// pool2 should be unaffected
		if snapshot.Pools["pool2"].ActiveBuckets != 1 {
			t.Errorf("expected 1 bucket in pool2, got %d", snapshot.Pools["pool2"].ActiveBuckets)
		}
	})

	t.Run("ResetPool_NotFound", func(t *testing.T) {
		_, err := limiter.ResetPool("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent pool")
		}
	})

	t.Run("ResetKey", func(t *testing.T) {
		// Recreate a bucket
		limiter.Allow([]config.RateLimitConfig{{Pool: "pool1"}}, ctx)

		cleared, err := limiter.ResetKey("pool1", "1.2.3.4")
		if err != nil {
			t.Fatalf("ResetKey failed: %v", err)
		}
		if !cleared {
			t.Error("expected key to be cleared")
		}

		// Try to clear non-existent key
		cleared, err = limiter.ResetKey("pool1", "nonexistent")
		if err != nil {
			t.Fatalf("ResetKey failed: %v", err)
		}
		if cleared {
			t.Error("expected key not to be cleared (doesn't exist)")
		}
	})

	t.Run("ResetKey_PoolNotFound", func(t *testing.T) {
		_, err := limiter.ResetKey("nonexistent", "key")
		if err == nil {
			t.Error("expected error for nonexistent pool")
		}
	})

	t.Run("ResetAll", func(t *testing.T) {
		// First reset everything to start clean
		limiter.ResetAll()

		// Create buckets in both pools
		limiter.Allow([]config.RateLimitConfig{{Pool: "pool1"}}, ctx)
		limiter.Allow([]config.RateLimitConfig{{Pool: "pool1"}}, ctx2)
		limiter.Allow([]config.RateLimitConfig{{Pool: "pool2"}}, ctx)

		count := limiter.ResetAll()
		if count != 3 {
			t.Errorf("expected 3 buckets cleared, got %d", count)
		}

		snapshot := limiter.Snapshot()
		if snapshot.Pools["pool1"].ActiveBuckets != 0 {
			t.Errorf("expected 0 buckets in pool1, got %d", snapshot.Pools["pool1"].ActiveBuckets)
		}
		if snapshot.Pools["pool2"].ActiveBuckets != 0 {
			t.Errorf("expected 0 buckets in pool2, got %d", snapshot.Pools["pool2"].ActiveBuckets)
		}
	})
}
