package cas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/internal/telemetry"
	"github.com/xraph/trove/observe"
)

// CAS provides content-addressable storage operations.
// Objects are stored under their content hash, enabling deduplication
// and integrity verification.
type CAS struct {
	store     driver.Driver
	bucket    string
	algorithm HashAlgorithm
	index     Index
	metrics   observe.Recorder
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

// WithMetrics records CAS stores and garbage collection on r
// (trove_cas_stores_total, trove_cas_gc_deleted_total and
// trove_cas_gc_freed_bytes_total). A nil r, the default, records nothing.
// trove.WithCAS passes the Trove's own Recorder.
func WithMetrics(r observe.Recorder) Option {
	return func(c *CAS) { c.metrics = r }
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
//
// It records a "store cas object" span with the algorithm, the size and
// whether the content was a duplicate. The hash is never recorded: it
// identifies the content.
func (c *CAS) Store(ctx context.Context, r io.Reader) (hash string, info *driver.ObjectInfo, err error) {
	driverLabel := telemetry.DriverName(c.store)
	ctx, span := telemetry.Start(ctx, telemetry.SpanStoreCASObject, trace.SpanKindInternal,
		telemetry.AttrCASAlgorithm.String(c.algorithm.String()),
		telemetry.AttrDriver.String(driverLabel),
	)
	deduplicated := false
	defer func() {
		result := observe.CASStored
		switch {
		case err != nil:
			result = observe.CASError
			telemetry.Fail(span, err)
		case deduplicated:
			result = observe.CASDeduplicated
		}
		span.SetAttributes(telemetry.AttrCASDeduplicated.Bool(deduplicated))
		span.End()
		telemetry.Add(c.metrics, observe.MetricCASStores, 1, result)
	}()

	// Read all data to compute hash.
	data, err := io.ReadAll(r)
	if err != nil {
		return "", nil, fmt.Errorf("cas: read content: %w", err)
	}
	span.SetAttributes(telemetry.AttrObjectSize.Int(len(data)))

	hash, err = computeHashBytes(data, c.algorithm)
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
		deduplicated = true
		return hash, &driver.ObjectInfo{
			Key:  entry.Key,
			Size: entry.Size,
		}, nil
	}

	// Store under hash as key.
	key := hash
	info, err = c.store.Put(telemetry.DriverContext(ctx), c.bucket, key, bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("cas: store object: %w", err)
	}

	// Record in index.
	if putErr := c.index.Put(ctx, &Entry{
		Hash:     hash,
		Bucket:   c.bucket,
		Key:      key,
		Size:     int64(len(data)),
		RefCount: 1,
	}); putErr != nil {
		return "", nil, fmt.Errorf("cas: index put: %w", putErr)
	}

	return hash, info, nil
}

// Retrieve gets content by hash.
func (c *CAS) Retrieve(ctx context.Context, hash string) (*driver.ObjectReader, error) {
	entry, err := c.index.Get(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("cas: retrieve: %w", err)
	}

	return c.store.Get(telemetry.DriverContext(ctx), entry.Bucket, entry.Key)
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

// Bucket returns the bucket this CAS stores content in.
func (c *CAS) Bucket() string {
	return c.bucket
}

// Stat returns the index entry for a hash without reading its content.
//
// It returns ErrNotFound when the index has no entry. With the default
// in-memory index that includes content still present in the bucket after
// a restart, because the index does not survive one.
func (c *CAS) Stat(ctx context.Context, hash string) (*Entry, error) {
	entry, err := c.index.Get(ctx, hash)
	if err != nil {
		return nil, err
	}
	cp := *entry
	return &cp, nil
}
