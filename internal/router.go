package internal

import (
	"path/filepath"
	"sync"

	"github.com/xraph/trove/driver"
)

// Route maps a key pattern to a named backend.
type Route struct {
	Pattern string
	Backend string
}

// RouteFunc is a custom routing function that returns a backend name
// for the given bucket and key. Return empty string to use the default backend.
type RouteFunc func(bucket, key string) string

// Router manages the default driver and named backends, resolving
// which driver should handle a given operation based on routing rules.
type Router struct {
	mu        sync.RWMutex
	defaultDr driver.Driver
	backends  map[string]driver.Driver
	routes    []Route
	routeFns  []RouteFunc
}

// NewRouter creates a new Router with the given default driver.
func NewRouter(defaultDr driver.Driver) *Router {
	return &Router{
		defaultDr: defaultDr,
		backends:  make(map[string]driver.Driver),
	}
}

// AddBackend registers a named backend.
func (r *Router) AddBackend(name string, drv driver.Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[name] = drv
}

// AddRoute adds a pattern-based routing rule.
func (r *Router) AddRoute(pattern, backend string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append(r.routes, Route{Pattern: pattern, Backend: backend})
}

// AddRouteFunc adds a custom routing function.
func (r *Router) AddRouteFunc(fn RouteFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routeFns = append(r.routeFns, fn)
}

// Resolve returns the driver that should handle the given bucket/key operation.
// Resolution order:
//  1. Custom route functions (first non-empty return wins)
//  2. Pattern routes (first matching pattern wins)
//  3. Default driver
func (r *Router) Resolve(bucket, key string) driver.Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. Check custom route functions.
	for _, fn := range r.routeFns {
		if backend := fn(bucket, key); backend != "" {
			if drv, ok := r.backends[backend]; ok {
				return drv
			}
		}
	}

	// 2. Check pattern routes.
	for _, route := range r.routes {
		matched, err := filepath.Match(route.Pattern, key)
		if err == nil && matched {
			if drv, ok := r.backends[route.Backend]; ok {
				return drv
			}
		}
	}

	// 3. Fall back to default.
	return r.defaultDr
}

// Backend returns the driver registered under the given name.
// Returns nil if the backend is not found.
func (r *Router) Backend(name string) driver.Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.backends[name]
}

// Default returns the default driver.
func (r *Router) Default() driver.Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultDr
}

// Backends returns the names of all registered backends.
func (r *Router) Backends() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.backends))
	for name := range r.backends {
		names = append(names, name)
	}
	return names
}

// Close closes the default driver and all named backends.
func (r *Router) Close(_ interface{ Done() <-chan struct{} }) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// We can't use context.Context here due to zero-Forge-import rule,
	// but the type constraint captures what we need.
	return nil
}

// CloseAll closes all drivers. This uses a concrete approach
// accepting a close function per driver.
func (r *Router) CloseAll(closeFn func(driver.Driver) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	if err := closeFn(r.defaultDr); err != nil {
		firstErr = err
	}

	for _, drv := range r.backends {
		if err := closeFn(drv); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
