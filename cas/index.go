package cas

import (
	"context"
	"fmt"
	"sync"
)

// Entry represents a CAS index entry mapping a content hash to its storage location.
type Entry struct {
	Hash     string `json:"hash"`
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	Size     int64  `json:"size"`
	RefCount int    `json:"ref_count"`
	Pinned   bool   `json:"pinned"`
}

// Index maps content hashes to their storage locations.
// Implementations can be in-memory, database-backed, etc.
type Index interface {
	// Get returns the entry for a hash, or an error if not found.
	Get(ctx context.Context, hash string) (*Entry, error)

	// Put stores or updates an entry. If the hash already exists,
	// the ref count is incremented.
	Put(ctx context.Context, entry *Entry) error

	// Delete removes an entry by hash.
	Delete(ctx context.Context, hash string) error

	// IncrementRef increments the reference count for a hash.
	IncrementRef(ctx context.Context, hash string) error

	// DecrementRef decrements the reference count for a hash.
	DecrementRef(ctx context.Context, hash string) error

	// Pin prevents garbage collection of the entry.
	Pin(ctx context.Context, hash string) error

	// Unpin allows garbage collection.
	Unpin(ctx context.Context, hash string) error

	// ListUnpinned returns all entries with ref_count=0 and pinned=false.
	ListUnpinned(ctx context.Context) ([]*Entry, error)
}

// ErrNotFound is returned when a hash is not in the index.
var ErrNotFound = fmt.Errorf("cas: hash not found")

// MemoryIndex is an in-memory Index implementation for testing.
type MemoryIndex struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// NewMemoryIndex creates a new in-memory CAS index.
func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{entries: make(map[string]*Entry)}
}

// Get returns the entry for a hash.
func (m *MemoryIndex) Get(_ context.Context, hash string) (*Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[hash]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *entry
	return &cp, nil
}

// Put stores or updates an entry.
func (m *MemoryIndex) Put(_ context.Context, entry *Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.entries[entry.Hash]; ok {
		existing.RefCount++
		return nil
	}
	cp := *entry
	if cp.RefCount == 0 {
		cp.RefCount = 1
	}
	m.entries[cp.Hash] = &cp
	return nil
}

// Delete removes an entry.
func (m *MemoryIndex) Delete(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, hash)
	return nil
}

// IncrementRef increments the reference count.
func (m *MemoryIndex) IncrementRef(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[hash]
	if !ok {
		return ErrNotFound
	}
	entry.RefCount++
	return nil
}

// DecrementRef decrements the reference count (min 0).
func (m *MemoryIndex) DecrementRef(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[hash]
	if !ok {
		return ErrNotFound
	}
	if entry.RefCount > 0 {
		entry.RefCount--
	}
	return nil
}

// Pin marks the entry as pinned.
func (m *MemoryIndex) Pin(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[hash]
	if !ok {
		return ErrNotFound
	}
	entry.Pinned = true
	return nil
}

// Unpin marks the entry as unpinned.
func (m *MemoryIndex) Unpin(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[hash]
	if !ok {
		return ErrNotFound
	}
	entry.Pinned = false
	return nil
}

// ListUnpinned returns entries eligible for garbage collection.
func (m *MemoryIndex) ListUnpinned(_ context.Context) ([]*Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Entry
	for _, entry := range m.entries {
		if !entry.Pinned && entry.RefCount == 0 {
			cp := *entry
			result = append(result, &cp)
		}
	}
	return result, nil
}
