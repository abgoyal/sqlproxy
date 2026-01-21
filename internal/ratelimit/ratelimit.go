// Package ratelimit provides per-client rate limiting with templated bucket keys.
package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"sql-proxy/internal/config"
	"sql-proxy/internal/tmpl"
)

// Limiter manages rate limiting for named pools with templated bucket keys
type Limiter struct {
	pools       map[string]*Pool
	inlinePools map[string]*Pool // Keyed by config hash for inline rate limits
	engine      *tmpl.Engine
	metrics     *Metrics
	mu          sync.RWMutex
}

// Pool represents a named rate limit pool with its own bucket collection
type Pool struct {
	name              string
	requestsPerSecond int
	burst             int
	keyTemplate       string

	buckets    map[string]*bucket
	bucketsMu  sync.RWMutex
	lastClean  time.Time
	cleanEvery time.Duration
}

// bucket wraps a rate.Limiter with last-used tracking for cleanup
type bucket struct {
	limiter  *rate.Limiter
	lastUsed atomic.Int64 // Unix timestamp
}

// Metrics tracks rate limiting statistics
type Metrics struct {
	mu sync.RWMutex

	TotalAllowed int64
	TotalDenied  int64

	// Per-pool metrics
	Pools map[string]*PoolMetrics
}

// PoolMetrics tracks statistics for a single pool
type PoolMetrics struct {
	Allowed       int64 `json:"allowed"`
	Denied        int64 `json:"denied"`
	ActiveBuckets int64 `json:"active_buckets"`
}

// Snapshot returns a point-in-time copy of metrics
type Snapshot struct {
	TotalAllowed int64                   `json:"total_allowed"`
	TotalDenied  int64                   `json:"total_denied"`
	Pools        map[string]*PoolMetrics `json:"pools"`
}

// New creates a Limiter from named pool configurations
func New(pools []config.RateLimitPoolConfig, engine *tmpl.Engine) (*Limiter, error) {
	l := &Limiter{
		pools:       make(map[string]*Pool),
		inlinePools: make(map[string]*Pool),
		engine:      engine,
		metrics: &Metrics{
			Pools: make(map[string]*PoolMetrics),
		},
	}

	for _, cfg := range pools {
		if cfg.Name == "" {
			return nil, fmt.Errorf("rate limit pool missing name")
		}
		if _, exists := l.pools[cfg.Name]; exists {
			return nil, fmt.Errorf("duplicate rate limit pool name: %s", cfg.Name)
		}
		if cfg.RequestsPerSecond <= 0 {
			return nil, fmt.Errorf("pool %q: requests_per_second must be positive", cfg.Name)
		}
		if cfg.Burst <= 0 {
			return nil, fmt.Errorf("pool %q: burst must be positive", cfg.Name)
		}
		if cfg.Key == "" {
			return nil, fmt.Errorf("pool %q: key template required", cfg.Name)
		}

		// Validate the key template
		if err := engine.Validate(cfg.Key, tmpl.UsagePreQuery); err != nil {
			return nil, fmt.Errorf("pool %q: invalid key template: %w", cfg.Name, err)
		}

		l.pools[cfg.Name] = &Pool{
			name:              cfg.Name,
			requestsPerSecond: cfg.RequestsPerSecond,
			burst:             cfg.Burst,
			keyTemplate:       cfg.Key,
			buckets:           make(map[string]*bucket),
			cleanEvery:        5 * time.Minute,
		}
		l.metrics.Pools[cfg.Name] = &PoolMetrics{}
	}

	return l, nil
}

// Allow checks if a request should be allowed based on the configured rate limits.
// Returns (allowed, retryAfter, error). If any pool denies, the request is denied.
// retryAfter indicates how long the client should wait before retrying (only set when denied).
// An error indicates a template evaluation failure (config bug, should not happen at runtime).
func (l *Limiter) Allow(limits []config.RateLimitConfig, ctx *tmpl.Context) (bool, time.Duration, error) {
	if len(limits) == 0 {
		return true, 0, nil
	}

	// All limits must pass
	for _, limit := range limits {
		allowed, retryAfter, err := l.allowOne(limit, ctx)
		if err != nil {
			return false, 0, err
		}
		if !allowed {
			return false, retryAfter, nil
		}
	}

	return true, 0, nil
}

// inlinePoolKey generates a unique key for an inline rate limit config
func inlinePoolKey(limit config.RateLimitConfig) string {
	key := limit.Key
	if key == "" {
		key = "{{.trigger.client_ip}}"
	}
	return fmt.Sprintf("%d:%d:%s", limit.RequestsPerSecond, limit.Burst, key)
}

// allowOne checks a single rate limit configuration
// Returns (allowed, retryAfter, error) where retryAfter is set when denied
func (l *Limiter) allowOne(limit config.RateLimitConfig, ctx *tmpl.Context) (bool, time.Duration, error) {
	var pool *Pool
	var keyTemplate string

	if limit.IsPoolReference() {
		// Look up named pool
		l.mu.RLock()
		pool = l.pools[limit.Pool]
		l.mu.RUnlock()

		if pool == nil {
			return false, 0, fmt.Errorf("rate limit pool %q not found", limit.Pool)
		}
		keyTemplate = pool.keyTemplate
	} else if limit.IsInline() {
		// Get or create inline pool (cached by config hash)
		poolKey := inlinePoolKey(limit)
		keyTemplate = limit.Key
		if keyTemplate == "" {
			keyTemplate = "{{.trigger.client_ip}}"
		}

		l.mu.RLock()
		pool = l.inlinePools[poolKey]
		l.mu.RUnlock()

		if pool == nil {
			l.mu.Lock()
			// Double-check after acquiring write lock
			pool = l.inlinePools[poolKey]
			if pool == nil {
				poolName := "_inline:" + poolKey
				pool = &Pool{
					name:              poolName,
					requestsPerSecond: limit.RequestsPerSecond,
					burst:             limit.Burst,
					keyTemplate:       keyTemplate,
					buckets:           make(map[string]*bucket),
					cleanEvery:        5 * time.Minute,
				}
				l.inlinePools[poolKey] = pool
				// Register metrics for the inline pool
				l.metrics.Pools[poolName] = &PoolMetrics{}
			}
			l.mu.Unlock()
		}
	} else {
		// Empty config - no rate limiting
		return true, 0, nil
	}

	// Evaluate key template
	key, err := l.engine.ExecuteInline(keyTemplate, ctx, tmpl.UsagePreQuery)
	if err != nil {
		return false, 0, fmt.Errorf("failed to evaluate rate limit key: %w", err)
	}

	// Get or create bucket
	b := pool.getOrCreateBucket(key)
	b.lastUsed.Store(time.Now().Unix())

	// Use Reserve() to get the delay information
	reservation := b.limiter.Reserve()
	delay := reservation.Delay()

	var allowed bool
	var retryAfter time.Duration

	if delay == 0 {
		// Token available immediately
		allowed = true
	} else {
		// Would need to wait - deny the request and cancel the reservation
		reservation.Cancel()
		allowed = false
		// Round up to next second for Retry-After header (HTTP spec uses seconds)
		retryAfter = delay.Truncate(time.Second) + time.Second
	}

	// Update metrics
	l.mu.Lock()
	if allowed {
		l.metrics.TotalAllowed++
		if poolMetrics, ok := l.metrics.Pools[pool.name]; ok {
			poolMetrics.Allowed++
		}
	} else {
		l.metrics.TotalDenied++
		if poolMetrics, ok := l.metrics.Pools[pool.name]; ok {
			poolMetrics.Denied++
		}
	}
	l.mu.Unlock()

	// Periodic cleanup
	pool.maybeCleanup()

	return allowed, retryAfter, nil
}

// RequestsPerSecond returns the configured rate limit
func (p *Pool) RequestsPerSecond() int {
	return p.requestsPerSecond
}

// Burst returns the configured burst size
func (p *Pool) Burst() int {
	return p.burst
}

// getOrCreateBucket returns an existing bucket or creates a new one
func (p *Pool) getOrCreateBucket(key string) *bucket {
	p.bucketsMu.RLock()
	b, exists := p.buckets[key]
	p.bucketsMu.RUnlock()

	if exists {
		return b
	}

	p.bucketsMu.Lock()
	defer p.bucketsMu.Unlock()

	// Double-check after acquiring write lock
	if b, exists = p.buckets[key]; exists {
		return b
	}

	b = &bucket{
		limiter: rate.NewLimiter(rate.Limit(p.requestsPerSecond), p.burst),
	}
	b.lastUsed.Store(time.Now().Unix())
	p.buckets[key] = b

	return b
}

// maybeCleanup removes stale buckets periodically
func (p *Pool) maybeCleanup() {
	now := time.Now()
	p.bucketsMu.RLock()
	needsClean := now.Sub(p.lastClean) > p.cleanEvery
	p.bucketsMu.RUnlock()

	if !needsClean {
		return
	}

	p.bucketsMu.Lock()
	defer p.bucketsMu.Unlock()

	// Double-check after acquiring write lock
	if now.Sub(p.lastClean) <= p.cleanEvery {
		return
	}

	// Remove buckets not used in the last 10 minutes
	threshold := now.Add(-10 * time.Minute).Unix()
	for key, b := range p.buckets {
		if b.lastUsed.Load() < threshold {
			delete(p.buckets, key)
		}
	}

	p.lastClean = now
}

// Snapshot returns current metrics
func (l *Limiter) Snapshot() *Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()

	snap := &Snapshot{
		TotalAllowed: l.metrics.TotalAllowed,
		TotalDenied:  l.metrics.TotalDenied,
		Pools:        make(map[string]*PoolMetrics),
	}

	for name, pool := range l.pools {
		poolMetrics := l.metrics.Pools[name]
		pool.bucketsMu.RLock()
		activeBuckets := int64(len(pool.buckets))
		pool.bucketsMu.RUnlock()

		snap.Pools[name] = &PoolMetrics{
			Allowed:       poolMetrics.Allowed,
			Denied:        poolMetrics.Denied,
			ActiveBuckets: activeBuckets,
		}
	}

	// Also include inline pools
	for _, pool := range l.inlinePools {
		poolMetrics := l.metrics.Pools[pool.name]
		if poolMetrics == nil {
			continue
		}
		pool.bucketsMu.RLock()
		activeBuckets := int64(len(pool.buckets))
		pool.bucketsMu.RUnlock()

		snap.Pools[pool.name] = &PoolMetrics{
			Allowed:       poolMetrics.Allowed,
			Denied:        poolMetrics.Denied,
			ActiveBuckets: activeBuckets,
		}
	}

	return snap
}

// GetPool returns a named pool or nil if not found
func (l *Limiter) GetPool(name string) *Pool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.pools[name]
}

// PoolNames returns all registered pool names
func (l *Limiter) PoolNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, 0, len(l.pools))
	for name := range l.pools {
		names = append(names, name)
	}
	return names
}

// ResetAll clears all buckets in all pools, returning total buckets cleared
func (l *Limiter) ResetAll() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	total := 0
	for _, pool := range l.pools {
		total += pool.Reset()
	}
	for _, pool := range l.inlinePools {
		total += pool.Reset()
	}
	return total
}

// ResetPool clears all buckets in a specific pool
// Returns (buckets cleared, error if pool not found)
func (l *Limiter) ResetPool(name string) (int, error) {
	l.mu.RLock()
	pool := l.pools[name]
	if pool == nil {
		pool = l.inlinePools[name]
	}
	l.mu.RUnlock()

	if pool == nil {
		return 0, fmt.Errorf("pool %q not found", name)
	}

	return pool.Reset(), nil
}

// ResetKey clears a specific bucket key in a pool
// Returns (true if key existed and was cleared, error if pool not found)
func (l *Limiter) ResetKey(poolName, key string) (bool, error) {
	l.mu.RLock()
	pool := l.pools[poolName]
	if pool == nil {
		pool = l.inlinePools[poolName]
	}
	l.mu.RUnlock()

	if pool == nil {
		return false, fmt.Errorf("pool %q not found", poolName)
	}

	return pool.ResetKey(key), nil
}

// Reset clears all buckets in this pool, returning count of buckets cleared
func (p *Pool) Reset() int {
	p.bucketsMu.Lock()
	defer p.bucketsMu.Unlock()

	count := len(p.buckets)
	p.buckets = make(map[string]*bucket)
	p.lastClean = time.Now()
	return count
}

// ResetKey clears a specific bucket key, returning true if it existed
func (p *Pool) ResetKey(key string) bool {
	p.bucketsMu.Lock()
	defer p.bucketsMu.Unlock()

	if _, exists := p.buckets[key]; exists {
		delete(p.buckets, key)
		return true
	}
	return false
}
