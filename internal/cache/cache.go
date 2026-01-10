package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/robfig/cron/v3"

	"sql-proxy/internal/config"
)

const (
	// defaultMaxCacheSizeMB is the default total cache size limit if not configured
	defaultMaxCacheSizeMB = 256

	// defaultCacheTTL is the default TTL for cached entries if not configured
	defaultCacheTTL = 5 * time.Minute

	// ristrettoNumCounters is the number of counters for frequency tracking
	ristrettoNumCounters = 1e6

	// ristrettoBufferItems is the number of keys per Get buffer
	ristrettoBufferItems = 64
)

// Cache wraps Ristretto with per-endpoint tracking and metrics
type Cache struct {
	store     *ristretto.Cache
	maxCost   int64 // Total max size in bytes
	defaultTTL time.Duration

	mu        sync.RWMutex
	endpoints map[string]*EndpointCache

	// Global metrics (atomic for lock-free reads)
	totalHits   atomic.Int64
	totalMisses atomic.Int64
}

// EndpointCache tracks per-endpoint cache state
type EndpointCache struct {
	mu          sync.RWMutex
	name        string
	maxCost     int64                  // Per-endpoint limit (0 = no limit)
	keys        map[string]*entryMeta  // Track keys and their metadata
	cronEvict   *cron.Cron             // Optional cron for scheduled eviction

	// Metrics
	hits      atomic.Int64
	misses    atomic.Int64
	sets      atomic.Int64
	evictions atomic.Int64
	sizeBytes atomic.Int64
}

// entryMeta stores metadata about a cached entry
type entryMeta struct {
	sizeBytes int64
	cachedAt  time.Time
	ttl       time.Duration
}

// Entry is what we store in the cache
type Entry struct {
	Data      []map[string]any
	SizeBytes int64
	CachedAt  time.Time
	TTL       time.Duration
}

// CacheSnapshot represents metrics at a point in time
type CacheSnapshot struct {
	Enabled        bool                        `json:"enabled"`
	TotalSizeBytes int64                       `json:"total_size_bytes"`
	MaxSizeBytes   int64                       `json:"max_size_bytes"`
	TotalKeys      int64                       `json:"total_keys"`
	TotalHits      int64                       `json:"total_hits"`
	TotalMisses    int64                       `json:"total_misses"`
	HitRatio       float64                     `json:"hit_ratio"`
	Endpoints      map[string]*EndpointMetrics `json:"endpoints"`
}

// EndpointMetrics contains per-endpoint cache statistics
type EndpointMetrics struct {
	Hits      int64   `json:"hits"`
	Misses    int64   `json:"misses"`
	Sets      int64   `json:"sets"`
	Evictions int64   `json:"evictions"`
	KeyCount  int64   `json:"key_count"`
	SizeBytes int64   `json:"size_bytes"`
	HitRatio  float64 `json:"hit_ratio"`
}

// New creates a new cache with the given configuration
func New(cfg config.CacheConfig) (*Cache, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	maxCost := int64(cfg.MaxSizeMB) * 1024 * 1024
	if maxCost == 0 {
		maxCost = defaultMaxCacheSizeMB * 1024 * 1024
	}

	ttl := time.Duration(cfg.DefaultTTLSec) * time.Second
	if ttl == 0 {
		ttl = defaultCacheTTL
	}

	// Configure Ristretto
	// NumCounters should be ~10x max items expected
	store, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: ristrettoNumCounters,
		MaxCost:     maxCost,
		BufferItems: ristrettoBufferItems,
		Metrics:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ristretto cache: %w", err)
	}

	return &Cache{
		store:      store,
		maxCost:    maxCost,
		defaultTTL: ttl,
		endpoints:  make(map[string]*EndpointCache),
	}, nil
}

// RegisterEndpoint sets up per-endpoint tracking with optional cron eviction
func (c *Cache) RegisterEndpoint(endpoint string, cfg *config.QueryCacheConfig) error {
	if c == nil || cfg == nil || !cfg.Enabled {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	maxCost := int64(cfg.MaxSizeMB) * 1024 * 1024

	ep := &EndpointCache{
		name:    endpoint,
		maxCost: maxCost,
		keys:    make(map[string]*entryMeta),
	}

	// Set up cron eviction if configured
	if cfg.EvictCron != "" {
		cr := cron.New()
		_, err := cr.AddFunc(cfg.EvictCron, func() {
			c.Clear(endpoint)
		})
		if err != nil {
			return fmt.Errorf("invalid evict_cron expression: %w", err)
		}
		ep.cronEvict = cr
		cr.Start()
	}

	c.endpoints[endpoint] = ep
	return nil
}

// Get retrieves a cached entry
func (c *Cache) Get(endpoint, key string) ([]map[string]any, bool) {
	if c == nil {
		return nil, false
	}

	fullKey := endpoint + ":" + key
	val, found := c.store.Get(fullKey)

	ep := c.getEndpoint(endpoint)

	if !found {
		c.totalMisses.Add(1)
		if ep != nil {
			ep.misses.Add(1)
		}
		return nil, false
	}

	entry, ok := val.(*Entry)
	if !ok {
		c.totalMisses.Add(1)
		if ep != nil {
			ep.misses.Add(1)
		}
		return nil, false
	}

	c.totalHits.Add(1)
	if ep != nil {
		ep.hits.Add(1)
	}

	return entry.Data, true
}

// Set stores data in the cache
func (c *Cache) Set(endpoint, key string, data []map[string]any, ttl time.Duration) bool {
	if c == nil {
		return false
	}

	// Calculate size
	sizeBytes := calculateSize(data)

	// Check per-endpoint limit
	ep := c.getEndpoint(endpoint)
	if ep != nil && ep.maxCost > 0 {
		currentSize := ep.sizeBytes.Load()
		if currentSize+sizeBytes > ep.maxCost {
			// Need to evict some entries from this endpoint
			c.evictFromEndpoint(endpoint, sizeBytes)
		}
	}

	fullKey := endpoint + ":" + key
	entry := &Entry{
		Data:      data,
		SizeBytes: sizeBytes,
		CachedAt:  time.Now(),
		TTL:       ttl,
	}

	if ttl == 0 {
		ttl = c.defaultTTL
	}

	// Store in ristretto with TTL
	success := c.store.SetWithTTL(fullKey, entry, sizeBytes, ttl)

	// Wait for value to pass through buffers
	c.store.Wait()

	if success && ep != nil {
		ep.mu.Lock()
		// Track if this is a new key or update
		if existing, exists := ep.keys[key]; exists {
			ep.sizeBytes.Add(-existing.sizeBytes)
		}
		ep.keys[key] = &entryMeta{
			sizeBytes: sizeBytes,
			cachedAt:  time.Now(),
			ttl:       ttl,
		}
		ep.sizeBytes.Add(sizeBytes)
		ep.sets.Add(1)
		ep.mu.Unlock()
	}

	return success
}

// Delete removes a specific key from the cache
func (c *Cache) Delete(endpoint, key string) {
	if c == nil {
		return
	}

	fullKey := endpoint + ":" + key
	c.store.Del(fullKey)

	ep := c.getEndpoint(endpoint)
	if ep != nil {
		ep.mu.Lock()
		if meta, exists := ep.keys[key]; exists {
			ep.sizeBytes.Add(-meta.sizeBytes)
			delete(ep.keys, key)
			ep.evictions.Add(1)
		}
		ep.mu.Unlock()
	}
}

// Clear removes all entries for an endpoint
func (c *Cache) Clear(endpoint string) {
	if c == nil {
		return
	}

	ep := c.getEndpoint(endpoint)
	if ep == nil {
		return
	}

	ep.mu.Lock()
	keys := make([]string, 0, len(ep.keys))
	for k := range ep.keys {
		keys = append(keys, k)
	}
	ep.mu.Unlock()

	for _, key := range keys {
		c.Delete(endpoint, key)
	}
}

// ClearAll removes all entries from the cache
func (c *Cache) ClearAll() {
	if c == nil {
		return
	}

	c.store.Clear()

	c.mu.RLock()
	for _, ep := range c.endpoints {
		ep.mu.Lock()
		ep.keys = make(map[string]*entryMeta)
		ep.sizeBytes.Store(0)
		ep.mu.Unlock()
	}
	c.mu.RUnlock()
}

// Close shuts down the cache and any cron jobs
func (c *Cache) Close() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, ep := range c.endpoints {
		if ep.cronEvict != nil {
			ep.cronEvict.Stop()
		}
	}

	c.store.Close()
}

// GetSnapshot returns current cache metrics
func (c *Cache) GetSnapshot() *CacheSnapshot {
	if c == nil {
		return nil
	}

	hits := c.totalHits.Load()
	misses := c.totalMisses.Load()

	snap := &CacheSnapshot{
		Enabled:      true,
		MaxSizeBytes: c.maxCost,
		TotalHits:    hits,
		TotalMisses:  misses,
		Endpoints:    make(map[string]*EndpointMetrics),
	}

	if hits+misses > 0 {
		snap.HitRatio = float64(hits) / float64(hits+misses)
	}

	c.mu.RLock()
	for name, ep := range c.endpoints {
		epHits := ep.hits.Load()
		epMisses := ep.misses.Load()

		ep.mu.RLock()
		keyCount := int64(len(ep.keys))
		ep.mu.RUnlock()

		epMetrics := &EndpointMetrics{
			Hits:      epHits,
			Misses:    epMisses,
			Sets:      ep.sets.Load(),
			Evictions: ep.evictions.Load(),
			KeyCount:  keyCount,
			SizeBytes: ep.sizeBytes.Load(),
		}
		if epHits+epMisses > 0 {
			epMetrics.HitRatio = float64(epHits) / float64(epHits+epMisses)
		}

		snap.Endpoints[name] = epMetrics
		snap.TotalKeys += keyCount
		snap.TotalSizeBytes += epMetrics.SizeBytes
	}
	c.mu.RUnlock()

	return snap
}

// GetTTLRemaining returns the remaining TTL for a cached entry
func (c *Cache) GetTTLRemaining(endpoint, key string) time.Duration {
	if c == nil {
		return 0
	}

	ep := c.getEndpoint(endpoint)
	if ep == nil {
		return 0
	}

	ep.mu.RLock()
	meta, exists := ep.keys[key]
	ep.mu.RUnlock()

	if !exists {
		return 0
	}

	elapsed := time.Since(meta.cachedAt)
	remaining := meta.ttl - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// getEndpoint returns the endpoint cache, creating if needed
func (c *Cache) getEndpoint(endpoint string) *EndpointCache {
	c.mu.RLock()
	ep := c.endpoints[endpoint]
	c.mu.RUnlock()
	return ep
}

// evictFromEndpoint removes entries to make room for new data
func (c *Cache) evictFromEndpoint(endpoint string, needed int64) {
	ep := c.getEndpoint(endpoint)
	if ep == nil {
		return
	}

	ep.mu.Lock()

	// Build list of keys sorted by access (we'll use cachedAt as proxy)
	type keyAge struct {
		key      string
		cachedAt time.Time
		size     int64
	}

	keys := make([]keyAge, 0, len(ep.keys))
	for k, meta := range ep.keys {
		keys = append(keys, keyAge{k, meta.cachedAt, meta.sizeBytes})
	}
	ep.mu.Unlock()

	// Sort by age (oldest first) - simple LRU approximation
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].cachedAt.Before(keys[j].cachedAt)
	})

	// Evict until we have enough space
	freed := int64(0)
	for _, ka := range keys {
		if freed >= needed {
			break
		}
		c.Delete(endpoint, ka.key)
		freed += ka.size
	}
}

// BuildKey creates a cache key from a template and parameters
func BuildKey(tmpl string, params map[string]any) (string, error) {
	if tmpl == "" {
		return "", fmt.Errorf("cache key template is empty")
	}

	t, err := template.New("key").Funcs(keyFuncMap).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing cache key template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("executing cache key template: %w", err)
	}

	return buf.String(), nil
}

// keyFuncMap provides template functions for cache keys
var keyFuncMap = template.FuncMap{
	"default": func(def, val any) any {
		if val == nil || val == "" {
			return def
		}
		return val
	},
}

// calculateSize returns the approximate size in bytes of the data
func calculateSize(data []map[string]any) int64 {
	if len(data) == 0 {
		return 0
	}
	b, _ := json.Marshal(data)
	return int64(len(b))
}
