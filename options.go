package trove

import (
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/internal"
	"github.com/xraph/trove/stream"
)

// Option configures a Trove instance.
type Option func(*Trove) error

// WithDefaultBucket sets the bucket used when none is specified in operations.
func WithDefaultBucket(name string) Option {
	return func(t *Trove) error {
		t.config.DefaultBucket = name
		return nil
	}
}

// WithChunkSize sets the size in bytes for streaming chunks.
func WithChunkSize(size int64) Option {
	return func(t *Trove) error {
		t.config.ChunkSize = size
		return nil
	}
}

// WithPoolSize sets the maximum number of concurrent streams.
func WithPoolSize(n int) Option {
	return func(t *Trove) error {
		t.config.PoolSize = n
		return nil
	}
}

// WithChecksumAlgorithm sets the hash algorithm for integrity verification.
func WithChecksumAlgorithm(alg ChecksumAlgorithm) Option {
	return func(t *Trove) error {
		t.config.ChecksumAlgorithm = alg
		return nil
	}
}

// WithStreamBufferSize sets the buffer size in bytes for stream operations.
func WithStreamBufferSize(size int) Option {
	return func(t *Trove) error {
		t.config.StreamBufferSize = size
		return nil
	}
}

// WithRetry configures retry behavior for storage operations.
func WithRetry(policy RetryPolicy) Option {
	return func(t *Trove) error {
		t.config.Retry = policy
		return nil
	}
}

// WithBackend registers a named storage backend.
// Named backends can be selected explicitly via Backend() or
// automatically via routing rules.
func WithBackend(name string, drv driver.Driver) Option {
	return func(t *Trove) error {
		t.router.AddBackend(name, drv)
		return nil
	}
}

// WithRoute adds a pattern-based routing rule that directs matching
// keys to a named backend. Pattern uses filepath.Match syntax.
//
// Example:
//
//	trove.WithRoute("*.log", "archive")   // All .log files → archive backend
//	trove.WithRoute("tmp/*", "local")     // All tmp/ keys → local backend
func WithRoute(pattern, backend string) Option {
	return func(t *Trove) error {
		t.router.AddRoute(pattern, backend)
		return nil
	}
}

// WithRouteFunc adds a custom routing function. The function receives
// the bucket and key and returns the backend name to use. Return empty
// string to fall through to the next rule or the default backend.
func WithRouteFunc(fn func(bucket, key string) string) Option {
	return func(t *Trove) error {
		t.router.AddRouteFunc(internal.RouteFunc(fn))
		return nil
	}
}

// WithPoolConfig configures the default streaming pool. This overrides
// the pool created from PoolSize and ChunkSize in DefaultConfig.
func WithPoolConfig(cfg stream.PoolConfig) Option {
	return func(t *Trove) error {
		t.pool = stream.NewPool("default", cfg)
		return nil
	}
}

// WithVFS sets the default bucket for VFS operations.
// When set, calling t.VFS() returns a filesystem view of this bucket.
func WithVFS(bucket string) Option {
	return func(t *Trove) error {
		t.vfsBucket = bucket
		return nil
	}
}
