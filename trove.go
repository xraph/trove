package trove

import (
	"context"
	"fmt"
	"io"
	"sort"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/internal"
	"github.com/xraph/trove/internal/telemetry"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/observe"
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
	metrics   observe.Recorder     // receives measurements (nil records nothing)

	// Set by WithCAS. The CAS is built once every option has run, so it
	// records on the Recorder whatever order the options came in.
	casEnabled bool
	casAlg     cas.HashAlgorithm
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

	if t.casEnabled {
		t.cas = cas.New(t.driver,
			cas.WithAlgorithm(t.casAlg),
			cas.WithMetrics(t.metrics),
		)
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
func (t *Trove) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (info *driver.ObjectInfo, err error) {
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
	ctx, o := t.startOp(ctx, drv, observe.OperationPut, telemetry.SpanPutObject)
	defer func() { o.end(err, infoSize(info), observe.DirectionWrite) }()

	// Resolve the write middleware pipeline.
	pipeline := t.resolver.ResolveWrite(ctx, bucket, key)
	if len(pipeline) > 0 {
		pr := t.applyWritePipeline(telemetry.ContextWithRecorder(ctx, t.metrics), r, key, pipeline)
		// The driver is done with the reader once Put returns. Closing it
		// releases the pipeline goroutine when the driver stopped reading
		// before the end (a failed upload), which otherwise blocked on its
		// next write forever.
		defer pr.Close()
		r = pr
	}

	return drv.Put(telemetry.DriverContext(ctx), bucket, key, r, opts...)
}

// Get retrieves an object, returning a ReadCloser for the content.
// Data passes through the read middleware pipeline (e.g., decryption,
// decompression) after being retrieved from the driver.
//
// Its span ends when Get returns, so it covers the driver's request and
// the response headers but not the time the caller spends reading the body.
func (t *Trove) Get(ctx context.Context, bucket, key string, opts ...driver.GetOption) (obj *driver.ObjectReader, err error) {
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
	ctx, o := t.startOp(ctx, drv, observe.OperationGet, telemetry.SpanGetObject)
	defer func() { o.end(err, readerSize(obj), observe.DirectionRead) }()

	obj, err = drv.Get(telemetry.DriverContext(ctx), bucket, key, opts...)
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
func (t *Trove) Delete(ctx context.Context, bucket, key string, opts ...driver.DeleteOption) (err error) {
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
	ctx, o := t.startOp(ctx, drv, observe.OperationDelete, telemetry.SpanDeleteObject)
	defer func() { o.end(err, noSize, "") }()

	return drv.Delete(telemetry.DriverContext(ctx), bucket, key, opts...)
}

// Head returns object metadata without content.
func (t *Trove) Head(ctx context.Context, bucket, key string) (info *driver.ObjectInfo, err error) {
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
	ctx, o := t.startOp(ctx, drv, observe.OperationHead, telemetry.SpanHeadObject)
	defer func() { o.end(err, infoSize(info), "") }()

	return drv.Head(telemetry.DriverContext(ctx), bucket, key)
}

// List returns objects matching the given options.
func (t *Trove) List(ctx context.Context, bucket string, opts ...driver.ListOption) (it *driver.ObjectIterator, err error) {
	if bucket == "" {
		bucket = t.config.DefaultBucket
	}
	if bucket == "" {
		return nil, ErrBucketEmpty
	}

	drv := t.router.Resolve(bucket, "")
	ctx, o := t.startOp(ctx, drv, observe.OperationList, telemetry.SpanListObjects)
	defer func() { o.end(err, noSize, "") }()

	return drv.List(telemetry.DriverContext(ctx), bucket, opts...)
}

// Copy copies an object within or across buckets.
func (t *Trove) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (info *driver.ObjectInfo, err error) {
	if srcBucket == "" || dstBucket == "" {
		return nil, ErrBucketEmpty
	}
	if srcKey == "" || dstKey == "" {
		return nil, ErrKeyEmpty
	}

	// Use the source driver for the copy operation.
	drv := t.router.Resolve(srcBucket, srcKey)
	ctx, o := t.startOp(ctx, drv, observe.OperationCopy, telemetry.SpanCopyObject)
	defer func() { o.end(err, infoSize(info), "") }()

	return drv.Copy(telemetry.DriverContext(ctx), srcBucket, srcKey, dstBucket, dstKey, opts...)
}

// --- Bucket Operations ---

// CreateBucket creates a new bucket on the default driver.
func (t *Trove) CreateBucket(ctx context.Context, name string, opts ...driver.BucketOption) (err error) {
	if name == "" {
		return ErrBucketEmpty
	}

	ctx, o := t.startOp(ctx, t.driver, observe.OperationCreateBucket, telemetry.SpanCreateBucket)
	defer func() { o.end(err, noSize, "") }()

	return t.driver.CreateBucket(telemetry.DriverContext(ctx), name, opts...)
}

// DeleteBucket removes a bucket from the default driver.
func (t *Trove) DeleteBucket(ctx context.Context, name string) (err error) {
	if name == "" {
		return ErrBucketEmpty
	}

	ctx, o := t.startOp(ctx, t.driver, observe.OperationDeleteBucket, telemetry.SpanDeleteBucket)
	defer func() { o.end(err, noSize, "") }()

	return t.driver.DeleteBucket(telemetry.DriverContext(ctx), name)
}

// ListBuckets returns all buckets from the default driver.
func (t *Trove) ListBuckets(ctx context.Context) (buckets []driver.BucketInfo, err error) {
	ctx, o := t.startOp(ctx, t.driver, observe.OperationListBuckets, telemetry.SpanListBuckets)
	defer func() { o.end(err, noSize, "") }()

	return t.driver.ListBuckets(telemetry.DriverContext(ctx))
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
		metrics:  t.metrics,  // record on the parent's recorder
	}, nil
}

// Driver returns the primary (default) storage driver.
func (t *Trove) Driver() driver.Driver {
	return t.driver
}

// DriverFor returns the driver that serves bucket and key, after routing.
// Driver returns only the default; an object routed elsewhere is served by
// this one.
func (t *Trove) DriverFor(bucket, key string) driver.Driver {
	return t.router.Resolve(bucket, key)
}

// Backends returns the names of the backends registered with WithBackend,
// sorted. The default driver has no name and is never in the list, so an
// empty list means every operation goes to the default. A handle returned
// by Backend is pinned to one driver and returns an empty list.
func (t *Trove) Backends() []string {
	names := t.router.Backends()
	sort.Strings(names)
	return names
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
// copy data through, and return the reader side. The caller closes the
// reader side once the driver has finished with it.
//
// The goroutine records one "apply write middleware" span, from wrapping
// the chain to closing it, as a child of the operation's span.
func (t *Trove) applyWritePipeline(ctx context.Context, r io.Reader, key string, pipeline []middleware.WriteMiddleware) *io.PipeReader {
	// Create a buffer to hold the transformed data.
	pr, pw := io.Pipe()

	go func() {
		mwCtx, span := telemetry.Start(ctx, telemetry.SpanApplyWriteMiddleware, trace.SpanKindInternal,
			telemetry.AttrMiddlewareCount.Int(len(pipeline)),
			telemetry.AttrMiddlewareNames.StringSlice(middlewareNames(pipeline)),
		)
		var err error
		defer func() {
			telemetry.Fail(span, err, rootSentinels...)
			span.End()
		}()

		// Build the writer chain: innermost writer is the pipe writer.
		var w io.WriteCloser = pw

		// Apply middleware in reverse order so the first middleware in the
		// pipeline is the outermost wrapper (data flows through it first).
		for i := len(pipeline) - 1; i >= 0; i-- {
			wrapped, wrapErr := pipeline[i].WrapWriter(mwCtx, w, key)
			if wrapErr != nil {
				err = fmt.Errorf("trove: middleware %d wrap writer: %w", i, wrapErr)
				_ = pw.CloseWithError(err)
				return
			}
			w = wrapped
		}

		// Copy original data through the middleware chain.
		if _, err = io.Copy(w, r); err != nil {
			_ = pw.CloseWithError(err)
			return
		}

		// Close the writer chain to flush/finalize middleware.
		if err = w.Close(); err != nil {
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
