package cas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/xraph/trove/driver"
)

// CAS provides content-addressable storage operations.
// Objects are stored under their content hash, enabling deduplication
// and integrity verification.
type CAS struct {
	store     driver.Driver
	bucket    string
	algorithm HashAlgorithm
	index     Index
}

// Option configures a CAS instance.
type Option func(*CAS)

// WithAlgorithm sets the hash algorithm (default: SHA256).
func WithAlgorithm(alg HashAlgorithm) Option {
	return func(c *CAS) { c.algorithm = alg }
}

// WithIndex sets the CAS index (default: in-memory).
func WithIndex(idx Index) Option {
	return func(c *CAS) { c.index = idx }
}

// WithBucket sets the storage bucket for CAS objects.
func WithBucket(bucket string) Option {
	return func(c *CAS) { c.bucket = bucket }
}

// New creates a new CAS instance backed by the given driver.
func New(store driver.Driver, opts ...Option) *CAS {
	c := &CAS{
		store:     store,
		bucket:    "cas",
		algorithm: AlgSHA256,
		index:     NewMemoryIndex(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Store stores content and returns its hash. If identical content already
// exists, the reference count is incremented and no duplicate is stored.
func (c *CAS) Store(ctx context.Context, r io.Reader) (string, *driver.ObjectInfo, error) {
	// Read all data to compute hash.
	data, err := io.ReadAll(r)
	if err != nil {
		return "", nil, fmt.Errorf("cas: read content: %w", err)
	}

	hash, err := computeHashBytes(data, c.algorithm)
	if err != nil {
		return "", nil, err
	}

	// Check if already stored.
	entry, getErr := c.index.Get(ctx, hash)
	if getErr == nil {
		// Content exists — increment ref count and return.
		if incErr := c.index.IncrementRef(ctx, hash); incErr != nil {
			return "", nil, fmt.Errorf("cas: increment ref: %w", incErr)
		}
		return hash, &driver.ObjectInfo{
			Key:  entry.Key,
			Size: entry.Size,
		}, nil
	}

	// Store under hash as key.
	key := hash
	info, err := c.store.Put(ctx, c.bucket, key, bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("cas: store object: %w", err)
	}

	// Record in index.
	if err := c.index.Put(ctx, &Entry{
		Hash:     hash,
		Bucket:   c.bucket,
		Key:      key,
		Size:     int64(len(data)),
		RefCount: 1,
	}); err != nil {
		return "", nil, fmt.Errorf("cas: index put: %w", err)
	}

	return hash, info, nil
}

// Retrieve gets content by hash.
func (c *CAS) Retrieve(ctx context.Context, hash string) (*driver.ObjectReader, error) {
	entry, err := c.index.Get(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("cas: retrieve: %w", err)
	}

	return c.store.Get(ctx, entry.Bucket, entry.Key)
}

// Exists checks if content with the given hash is stored.
func (c *CAS) Exists(ctx context.Context, hash string) (bool, error) {
	_, err := c.index.Get(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Pin prevents garbage collection of a CAS object.
func (c *CAS) Pin(ctx context.Context, hash string) error {
	return c.index.Pin(ctx, hash)
}

// Unpin allows garbage collection of a CAS object.
func (c *CAS) Unpin(ctx context.Context, hash string) error {
	return c.index.Unpin(ctx, hash)
}

// GC performs garbage collection, removing unreferenced and unpinned objects.
func (c *CAS) GC(ctx context.Context) (*GCResult, error) {
	return c.gc(ctx)
}

// Algorithm returns the hash algorithm used by this CAS instance.
func (c *CAS) Algorithm() HashAlgorithm {
	return c.algorithm
}
