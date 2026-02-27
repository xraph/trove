// Package dedup provides content-hash deduplication middleware for Trove.
// It hashes data on write and checks for existing content before storing.
package dedup

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/xraph/trove/internal"
	"github.com/xraph/trove/middleware"
)

// Compile-time interface checks.
var (
	_ middleware.Middleware      = (*Dedup)(nil)
	_ middleware.WriteMiddleware = (*Dedup)(nil)
)

// Store tracks content hashes to detect duplicates.
type Store interface {
	// Exists checks if content with the given hash already exists.
	Exists(ctx context.Context, hash string) (bool, error)

	// Record stores a hash → key mapping.
	Record(ctx context.Context, hash, key string) error
}

// MemoryStore is an in-memory Store for testing.
type MemoryStore struct {
	mu     sync.RWMutex
	hashes map[string]string // hash → key
}

// NewMemoryStore creates a new in-memory dedup store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{hashes: make(map[string]string)}
}

// Exists checks if the hash exists.
func (s *MemoryStore) Exists(_ context.Context, hash string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.hashes[hash]
	return ok, nil
}

// Record stores a hash → key mapping.
func (s *MemoryStore) Record(_ context.Context, hash, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hashes[hash] = key
	return nil
}

// Option configures the Dedup middleware.
type Option func(*Dedup)

// WithHashAlgorithm sets the hash algorithm (default: blake3).
func WithHashAlgorithm(alg internal.ChecksumAlgorithm) Option {
	return func(d *Dedup) { d.algorithm = alg }
}

// WithStore sets the dedup store for tracking hashes.
func WithStore(store Store) Option {
	return func(d *Dedup) { d.store = store }
}

// OnDuplicate is called when a duplicate is detected.
// The handler receives the key being written and the hash that matched.
type OnDuplicate func(ctx context.Context, key, hash string)

// WithOnDuplicate sets a callback for duplicate detection.
func WithOnDuplicate(fn OnDuplicate) Option {
	return func(d *Dedup) { d.onDuplicate = fn }
}

// Dedup provides content-hash deduplication middleware.
type Dedup struct {
	algorithm   internal.ChecksumAlgorithm
	store       Store
	onDuplicate OnDuplicate
}

// New creates a new deduplication middleware.
func New(opts ...Option) *Dedup {
	d := &Dedup{
		algorithm: internal.AlgBlake3,
		store:     NewMemoryStore(),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Name returns the middleware identifier.
func (d *Dedup) Name() string { return "dedup" }

// Direction returns Write since dedup only checks on the write path.
func (d *Dedup) Direction() middleware.Direction { return middleware.DirectionWrite }

// WrapWriter wraps a writer to hash content and check for duplicates.
// If a duplicate is found, the write is skipped (empty data written).
func (d *Dedup) WrapWriter(ctx context.Context, w io.WriteCloser, key string) (io.WriteCloser, error) {
	return &dedupWriter{
		ctx:       ctx,
		inner:     w,
		key:       key,
		algorithm: d.algorithm,
		store:     d.store,
		onDup:     d.onDuplicate,
		buf:       &bytes.Buffer{},
	}, nil
}

type dedupWriter struct {
	ctx       context.Context
	inner     io.WriteCloser
	key       string
	algorithm internal.ChecksumAlgorithm
	store     Store
	onDup     OnDuplicate
	buf       *bytes.Buffer
}

func (w *dedupWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *dedupWriter) Close() error {
	data := w.buf.Bytes()

	// Compute hash.
	h, err := internal.NewHash(w.algorithm)
	if err != nil {
		return fmt.Errorf("dedup: new hash: %w", err)
	}
	if _, hashErr := h.Write(data); hashErr != nil {
		return fmt.Errorf("dedup: compute hash: %w", hashErr)
	}
	hash := string(w.algorithm) + ":" + hex.EncodeToString(h.Sum(nil))

	// Check for duplicate.
	exists, err := w.store.Exists(w.ctx, hash)
	if err != nil {
		return fmt.Errorf("dedup: check exists: %w", err)
	}

	if exists {
		if w.onDup != nil {
			w.onDup(w.ctx, w.key, hash)
		}
		// Still write the data — the CAS layer handles actual dedup storage.
		// This middleware just detects and reports duplicates.
	}

	// Record the hash.
	if err := w.store.Record(w.ctx, hash, w.key); err != nil {
		return fmt.Errorf("dedup: record hash: %w", err)
	}

	// Write data through.
	if _, err := w.inner.Write(data); err != nil {
		return err
	}
	return w.inner.Close()
}
