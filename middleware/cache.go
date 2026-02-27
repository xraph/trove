package middleware

import "sync"

// scopeCache is a simple LRU cache for resolved middleware pipelines.
// Cache keys are derived from (direction, bucket, key) tuples.
type scopeCache struct {
	mu       sync.RWMutex
	capacity int
	entries  map[string]*cacheEntry
	order    []string // LRU order: most recently used at the end
	version  uint64   // incremented on invalidation
}

type cacheEntry struct {
	readPipeline  []ReadMiddleware
	writePipeline []WriteMiddleware
	version       uint64
}

func newScopeCache(capacity int) *scopeCache {
	return &scopeCache{
		capacity: capacity,
		entries:  make(map[string]*cacheEntry, capacity),
		order:    make([]string, 0, capacity),
	}
}

// getRead returns a cached read pipeline, or nil if not cached.
func (c *scopeCache) getRead(key string) []ReadMiddleware {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || entry.version != c.version {
		return nil
	}
	return entry.readPipeline
}

// getWrite returns a cached write pipeline, or nil if not cached.
func (c *scopeCache) getWrite(key string) []WriteMiddleware {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || entry.version != c.version {
		return nil
	}
	return entry.writePipeline
}

// putRead stores a read pipeline in the cache.
func (c *scopeCache) putRead(key string, pipeline []ReadMiddleware) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[key]; ok {
		entry.readPipeline = pipeline
		entry.version = c.version
		c.touch(key)
		return
	}

	c.evictIfFull()
	c.entries[key] = &cacheEntry{
		readPipeline: pipeline,
		version:      c.version,
	}
	c.order = append(c.order, key)
}

// putWrite stores a write pipeline in the cache.
func (c *scopeCache) putWrite(key string, pipeline []WriteMiddleware) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[key]; ok {
		entry.writePipeline = pipeline
		entry.version = c.version
		c.touch(key)
		return
	}

	c.evictIfFull()
	c.entries[key] = &cacheEntry{
		writePipeline: pipeline,
		version:       c.version,
	}
	c.order = append(c.order, key)
}

// Invalidate bumps the version so all cached entries are stale.
func (c *scopeCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
}

// touch moves a key to the end of the LRU order (most recently used).
func (c *scopeCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

// evictIfFull removes the least recently used entry if at capacity.
func (c *scopeCache) evictIfFull() {
	if len(c.entries) < c.capacity {
		return
	}
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}
