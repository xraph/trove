package driver

import (
	"fmt"
	"sync"
)

// Factory creates a new driver instance.
type Factory func() Driver

// registry is the global thread-safe driver registry.
var registry = &driverRegistry{
	drivers: make(map[string]Factory),
}

type driverRegistry struct {
	mu      sync.RWMutex
	drivers map[string]Factory
}

// Register registers a driver factory under the given name.
// It panics if a driver with the same name is already registered.
func Register(name string, factory Factory) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if _, exists := registry.drivers[name]; exists {
		panic(fmt.Sprintf("driver: %q already registered", name))
	}

	registry.drivers[name] = factory
}

// Lookup returns the driver factory registered under the given name.
func Lookup(name string) (Factory, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	factory, ok := registry.drivers[name]
	return factory, ok
}

// Drivers returns the names of all registered drivers.
func Drivers() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	names := make([]string, 0, len(registry.drivers))
	for name := range registry.drivers {
		names = append(names, name)
	}
	return names
}
