package trove

import (
	"context"
	"fmt"
	"io"

	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/internal"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/stream"
	"github.com/xraph/trove/vfs"
)

// Trove is the root handle for object storage operations.
// It wraps one or more storage drivers, routing operations through
// the configured backend(s).
type Trove struct {
	config    Config
	router    *internal.Router
	driver    driver.Driver        // primary driver (alias for router.Default())
	pool      *stream.Pool         // default streaming pool
	resolver  *middleware.Resolver // middleware pipeline resolver
	cas       *cas.CAS             // content-addressable storage (nil if disabled)
	vfsBucket string               // VFS default bucket (empty = use config.DefaultBucket)
}

// Open creates a new Trove instance with the given default driver and options.
//
// Example:
//
//	t, err := trove.Open(localdriver.New(),
//	    trove.WithDefaultBucket("primary"),
//	    trove.WithChunkSize(8 * 1024 * 1024),
//	)
func Open(drv driver.Driver, opts ...Option) (*Trove, error) {
	if drv == nil {
		return nil, ErrNilDriver
	}

	t := &Trove{
		config:   DefaultConfig(),
		router:   internal.NewRouter(drv),
		driver:   drv,
		resolver: middleware.NewResolver(),
	}

	for _, opt := range opts {
		if err := opt(t); err != nil {
			return nil, fmt.Errorf("trove: apply option: %w", err)
		}
	}

	// Initialize the default streaming pool if not set by options.
	if t.pool == nil {
		t.pool = stream.NewPool("default", stream.PoolConfig{
			MaxStreams: t.config.PoolSize,
			ChunkSize:  t.config.ChunkSize,
		})
	}

	return t, nil
}

// --- Object Operations ---

// Put stores an object from a reader. Data passes through the write
// middleware pipeline (e.g., encryption, compression) before reaching the driver.
func (t *Trove) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return nil, ErrBucketEmpty
	}
	if key == "" {
		return nil, ErrKeyEmpty
	}

	// Resolve the write middleware pipeline.
	pipeline := t.resolver.ResolveWrite(ctx, bucket, key)
	if len(pipeline) > 0 {
		r = t.applyWritePipeline(ctx, r, key, pipeline)
	}

	drv := t.router.Resolve(bucket, key)
	return drv.Put(ctx, bucket, key, r, opts...)
}

// Get retrieves an object, returning a ReadCloser for the content.
// Data passes through the read middleware pipeline (e.g., decryption,
// decompression) after being retrieved from the driver.
func (t *Trove) Get(ctx context.Context, bucket, key string, opts ...driver.GetOption) (*driver.ObjectReader, error) {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return nil, ErrBucketEmpty
	}
	if key == "" {
		return nil, ErrKeyEmpty
	}

	drv := t.router.Resolve(bucket, key)
	obj, err := drv.Get(ctx, bucket, key, opts...)
	if err != nil {
		return nil, err
	}

	// Resolve the read middleware pipeline.
	pipeline := t.resolver.ResolveRead(ctx, bucket, key)
	if len(pipeline) > 0 {
		wrapped, wrapErr := t.applyReadPipeline(ctx, obj.ReadCloser, obj.Info, pipeline)
		if wrapErr != nil {
			_ = obj.Close()
			return nil, wrapErr
		}
		obj.ReadCloser = wrapped
	}

	return obj, nil
}

// Delete removes an object.
func (t *Trove) Delete(ctx context.Context, bucket, key string, opts ...driver.DeleteOption) error {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return ErrBucketEmpty
	}
	if key == "" {
		return ErrKeyEmpty
	}

	drv := t.router.Resolve(bucket, key)
	return drv.Delete(ctx, bucket, key, opts...)
}

// Head returns object metadata without content.
func (t *Trove) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return nil, ErrBucketEmpty
	}
	if key == "" {
		return nil, ErrKeyEmpty
	}

	drv := t.router.Resolve(bucket, key)
	return drv.Head(ctx, bucket, key)
}

// List returns objects matching the given options.
func (t *Trove) List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return nil, ErrBucketEmpty
	}

	drv := t.router.Resolve(bucket, "")
	return drv.List(ctx, bucket, opts...)
}

// Copy copies an object within or across buckets.
func (t *Trove) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error) {
	if srcBucket == "" || dstBucket == "" {
		return nil, ErrBucketEmpty
	}
	if srcKey == "" || dstKey == "" {
		return nil, ErrKeyEmpty
	}

	// Use the source driver for the copy operation.
	drv := t.router.Resolve(srcBucket, srcKey)
	return drv.Copy(ctx, srcBucket, srcKey, dstBucket, dstKey, opts...)
}

// --- Bucket Operations ---

// CreateBucket creates a new bucket on the default driver.
func (t *Trove) CreateBucket(ctx context.Context, name string, opts ...driver.BucketOption) error {
	if name == "" {
		return ErrBucketEmpty
	}

	return t.driver.CreateBucket(ctx, name, opts...)
}

// DeleteBucket removes a bucket from the default driver.
func (t *Trove) DeleteBucket(ctx context.Context, name string) error {
	if name == "" {
		return ErrBucketEmpty
	}

	return t.driver.DeleteBucket(ctx, name)
}

// ListBuckets returns all buckets from the default driver.
func (t *Trove) ListBuckets(ctx context.Context) ([]driver.BucketInfo, error) {
	return t.driver.ListBuckets(ctx)
}

// --- Accessors ---

// Backend returns a new Trove handle pinned to the named backend.
// All operations on the returned handle bypass routing and go directly
// to the specified backend.
func (t *Trove) Backend(name string) (*Trove, error) {
	drv := t.router.Backend(name)
	if drv == nil {
		return nil, fmt.Errorf("%w: %q", ErrBackendNotFound, name)
	}

	return &Trove{
		config:   t.config,
		router:   internal.NewRouter(drv), // pinned to single backend
		driver:   drv,
		pool:     t.pool,     // share the parent's streaming pool
		resolver: t.resolver, // share the parent's middleware pipeline
		cas:      t.cas,      // share the parent's CAS
	}, nil
}

// Driver returns the primary (default) storage driver.
func (t *Trove) Driver() driver.Driver {
	return t.driver
}

// Config returns a copy of the current configuration.
func (t *Trove) Config() Config {
	return t.config
}

// Pool returns the default streaming pool.
func (t *Trove) Pool() *stream.Pool {
	return t.pool
}

// Stream creates a new managed stream within the default pool. The stream
// provides chunked transfer with backpressure, resumability, and lifecycle hooks.
func (t *Trove) Stream(ctx context.Context, bucket, key string, dir stream.Direction, opts ...stream.Option) (*stream.Stream, error) {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return nil, ErrBucketEmpty
	}
	if key == "" {
		return nil, ErrKeyEmpty
	}

	return t.pool.Acquire(ctx, dir, bucket, key, opts...)
}

// Resolver returns the middleware pipeline resolver.
func (t *Trove) Resolver() *middleware.Resolver {
	return t.resolver
}

// CAS returns the content-addressable storage engine, or nil if CAS is not enabled.
func (t *Trove) CAS() *cas.CAS {
	return t.cas
}

// VFS returns a virtual filesystem view of the given bucket.
// If no bucket is specified, it uses the VFS bucket (set via WithVFS)
// or falls back to the default bucket.
func (t *Trove) VFS(bucket ...string) *vfs.FS {
	b := t.vfsBucket
	if len(bucket) > 0 && bucket[0] != "" {
		b = bucket[0]
	}
	if b == "" {
		b = t.config.DefaultBucket
	}
	return vfs.New(t, b)
}

// UseMiddleware registers a middleware at runtime. Thread-safe.
func (t *Trove) UseMiddleware(reg middleware.Registration) {
	t.resolver.Register(reg)
}

// RemoveMiddleware removes all registrations matching the given middleware
// name and scope. If scope is nil, removes all registrations with that name.
func (t *Trove) RemoveMiddleware(name string, scope middleware.Scope) {
	t.resolver.Remove(name, scope)
}

// Health checks the health of the Trove by pinging its primary driver.
func (t *Trove) Health(ctx context.Context) error {
	return t.driver.Ping(ctx)
}

// Close gracefully shuts down the streaming pool and all drivers.
func (t *Trove) Close(ctx context.Context) error {
	if t.pool != nil {
		_ = t.pool.Close()
	}
	return t.router.CloseAll(func(drv driver.Driver) error {
		return drv.Close(ctx)
	})
}

// --- Middleware Pipeline Helpers ---

// applyWritePipeline wraps the reader through the write middleware chain.
// Write middleware wraps writers, so we create a pipe, wrap the writer side,
// copy data through, and return the reader side.
func (t *Trove) applyWritePipeline(ctx context.Context, r io.Reader, key string, pipeline []middleware.WriteMiddleware) io.Reader {
	// Create a buffer to hold the transformed data.
	pr, pw := io.Pipe()

	go func() {
		// Build the writer chain: innermost writer is the pipe writer.
		var w io.WriteCloser = pw

		// Apply middleware in reverse order so the first middleware in the
		// pipeline is the outermost wrapper (data flows through it first).
		for i := len(pipeline) - 1; i >= 0; i-- {
			wrapped, err := pipeline[i].WrapWriter(ctx, w, key)
			if err != nil {
				_ = pw.CloseWithError(fmt.Errorf("trove: middleware %d wrap writer: %w", i, err))
				return
			}
			w = wrapped
		}

		// Copy original data through the middleware chain.
		if _, err := io.Copy(w, r); err != nil {
			_ = pw.CloseWithError(err)
			return
		}

		// Close the writer chain to flush/finalize middleware.
		if err := w.Close(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
	}()

	return pr
}

// applyReadPipeline wraps the reader through the read middleware chain.
func (t *Trove) applyReadPipeline(ctx context.Context, r io.ReadCloser, info *driver.ObjectInfo, pipeline []middleware.ReadMiddleware) (io.ReadCloser, error) {
	var err error
	for _, mw := range pipeline {
		r, err = mw.WrapReader(ctx, r, info)
		if err != nil {
			return nil, fmt.Errorf("trove: middleware wrap reader: %w", err)
		}
	}
	return r, nil
}
