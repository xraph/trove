package extension

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/trove"
	"github.com/xraph/trove/extension/store"
)

// TroveManager manages multiple named trove.Trove instances.
// It provides named access, a default store, and bulk close.
type TroveManager struct {
	mu         sync.RWMutex
	stores     map[string]*trove.Trove
	metaStores map[string]store.Store
	defaultKey string
}

// NewTroveManager creates an empty TroveManager.
func NewTroveManager() *TroveManager {
	return &TroveManager{
		stores:     make(map[string]*trove.Trove),
		metaStores: make(map[string]store.Store),
	}
}

// Add registers a named Trove instance and its metadata store.
// The first instance added becomes the default unless SetDefault is called.
func (m *TroveManager) Add(name string, t *trove.Trove, s store.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stores[name] = t
	if s != nil {
		m.metaStores[name] = s
	}
	if m.defaultKey == "" {
		m.defaultKey = name
	}
}

// Get returns the Trove instance registered under name.
func (m *TroveManager) Get(name string) (*trove.Trove, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.stores[name]
	if !ok {
		return nil, fmt.Errorf("trove: store %q not found", name)
	}
	return t, nil
}

// GetStore returns the metadata store registered under name.
func (m *TroveManager) GetStore(name string) (store.Store, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.metaStores[name]
	if !ok {
		return nil, fmt.Errorf("trove: metadata store %q not found", name)
	}
	return s, nil
}

// Default returns the default Trove instance.
func (m *TroveManager) Default() (*trove.Trove, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.defaultKey == "" {
		return nil, fmt.Errorf("trove: no default store configured")
	}
	t, ok := m.stores[m.defaultKey]
	if !ok {
		return nil, fmt.Errorf("trove: default store %q not found", m.defaultKey)
	}
	return t, nil
}

// DefaultStore returns the default metadata store.
func (m *TroveManager) DefaultStore() (store.Store, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.defaultKey == "" {
		return nil, fmt.Errorf("trove: no default store configured")
	}
	s, ok := m.metaStores[m.defaultKey]
	if !ok {
		return nil, fmt.Errorf("trove: default metadata store %q not found", m.defaultKey)
	}
	return s, nil
}

// DefaultName returns the name of the default store.
func (m *TroveManager) DefaultName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultKey
}

// SetDefault sets the default store by name.
// Returns an error if the name is not registered.
func (m *TroveManager) SetDefault(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.stores[name]; !ok {
		return fmt.Errorf("trove: cannot set default: store %q not found", name)
	}
	m.defaultKey = name
	return nil
}

// All returns a shallow copy of the name-to-Trove map.
func (m *TroveManager) All() map[string]*trove.Trove {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]*trove.Trove, len(m.stores))
	for k, v := range m.stores {
		out[k] = v
	}
	return out
}

// Len returns the number of registered stores.
func (m *TroveManager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.stores)
}

// Close closes all registered Trove instances and returns the first error encountered.
func (m *TroveManager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, t := range m.stores {
		if err := t.Close(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("trove: close store %q: %w", name, err)
		}
	}
	return firstErr
}
