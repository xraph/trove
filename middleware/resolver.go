package middleware

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Resolver assembles the effective middleware pipeline per-operation.
// It evaluates all registrations against the current context, bucket, and key,
// then returns an ordered pipeline sorted by priority.
type Resolver struct {
	mu            sync.RWMutex
	registrations []Registration
	cache         *scopeCache
}

// NewResolver creates a new pipeline resolver with LRU caching.
func NewResolver() *Resolver {
	return &Resolver{
		cache: newScopeCache(1024),
	}
}

// Register adds a middleware registration. Thread-safe.
func (r *Resolver) Register(reg Registration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if reg.Scope == nil {
		reg.Scope = ScopeGlobal{}
	}
	r.registrations = append(r.registrations, reg)
	r.cache.Invalidate()
}

// Remove removes all registrations matching the given middleware name and scope.
// If scope is nil, removes all registrations with the given name.
func (r *Resolver) Remove(name string, scope Scope) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := r.registrations[:0]
	for _, reg := range r.registrations {
		if reg.Middleware.Name() == name {
			if scope == nil || reg.effectiveScope().String() == scope.String() {
				continue // remove this one
			}
		}
		filtered = append(filtered, reg)
	}
	r.registrations = filtered
	r.cache.Invalidate()
}

// Registrations returns a copy of all current registrations.
func (r *Resolver) Registrations() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Registration, len(r.registrations))
	copy(result, r.registrations)
	return result
}

// ResolveRead returns the ordered read middleware pipeline for the given operation.
func (r *Resolver) ResolveRead(ctx context.Context, bucket, key string) []ReadMiddleware {
	cacheKey := fmt.Sprintf("r:%s:%s", bucket, key)

	if cached := r.cache.getRead(cacheKey); cached != nil {
		return cached
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []orderedMiddleware
	for i, reg := range r.registrations {
		dir := reg.effectiveDirection()
		if dir&DirectionRead == 0 {
			continue
		}
		if !reg.effectiveScope().Match(ctx, bucket, key) {
			continue
		}
		if rm, ok := reg.Middleware.(ReadMiddleware); ok {
			active = append(active, orderedMiddleware{
				read:     rm,
				priority: reg.Priority,
				index:    i,
			})
		}
	}

	sort.Slice(active, func(i, j int) bool {
		if active[i].priority != active[j].priority {
			return active[i].priority < active[j].priority
		}
		return active[i].index < active[j].index
	})

	result := make([]ReadMiddleware, len(active))
	for i, a := range active {
		result[i] = a.read
	}

	r.cache.putRead(cacheKey, result)
	return result
}

// ResolveWrite returns the ordered write middleware pipeline for the given operation.
func (r *Resolver) ResolveWrite(ctx context.Context, bucket, key string) []WriteMiddleware {
	cacheKey := fmt.Sprintf("w:%s:%s", bucket, key)

	if cached := r.cache.getWrite(cacheKey); cached != nil {
		return cached
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []orderedMiddleware
	for i, reg := range r.registrations {
		dir := reg.effectiveDirection()
		if dir&DirectionWrite == 0 {
			continue
		}
		if !reg.effectiveScope().Match(ctx, bucket, key) {
			continue
		}
		if wm, ok := reg.Middleware.(WriteMiddleware); ok {
			active = append(active, orderedMiddleware{
				write:    wm,
				priority: reg.Priority,
				index:    i,
			})
		}
	}

	sort.Slice(active, func(i, j int) bool {
		if active[i].priority != active[j].priority {
			return active[i].priority < active[j].priority
		}
		return active[i].index < active[j].index
	})

	result := make([]WriteMiddleware, len(active))
	for i, a := range active {
		result[i] = a.write
	}

	r.cache.putWrite(cacheKey, result)
	return result
}

type orderedMiddleware struct {
	read     ReadMiddleware
	write    WriteMiddleware
	priority int
	index    int
}
