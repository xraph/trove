// Package driver defines the core interfaces and types that every Trove
// storage backend must implement.
//
// The Driver interface is intentionally minimal — advanced features are
// exposed via capability interfaces (MultipartDriver, PresignDriver, etc.)
// and the Unwrap pattern.
//
// # Error contract
//
// Callers classify failures with errors.Is, so implementations MUST wrap the
// sentinels declared in errors.go rather than returning a bare descriptive
// error. Keep the descriptive message — wrap with %w, do not replace:
//
//	return nil, fmt.Errorf("mydriver: object %q not found in bucket %q: %w",
//	    key, bucket, driver.ErrObjectNotFound)
//
// The conditions that MUST be classified:
//
//   - a missing object   → ErrObjectNotFound (Get, Head, Copy source, GetRange)
//   - a missing bucket   → ErrBucketNotFound (any operation naming a bucket)
//   - an existing bucket → ErrBucketExists (CreateBucket)
//   - an unauthorized op → ErrPermissionDenied
//   - a quota/rate limit → ErrQuotaExceeded
//
// This matters to schedulers built on Trove: a permanently missing object
// must fail its work item immediately, while a transient backend failure
// should be retried. Without the sentinel the two are indistinguishable and
// every retry is burned before the failure is recognized as permanent.
//
// Backends that report these conditions with a typed error or an HTTP status
// code MUST use that signal rather than matching on message text. Delete is
// idempotent by convention and returns nil for a missing object.
package driver

import (
	"context"
	"errors"
	"io"
	"time"
)

// Driver is the core interface every storage backend implements.
// It exposes the minimum surface area for object CRUD and streaming.
type Driver interface {
	// Name returns the driver identifier (e.g., "s3", "gcs", "azure", "local", "mem").
	Name() string

	// Open initializes the driver connection with the given DSN and options.
	Open(ctx context.Context, dsn string, opts ...Option) error

	// Close gracefully shuts down the driver, draining active streams.
	Close(ctx context.Context) error

	// Ping verifies backend connectivity.
	Ping(ctx context.Context) error

	// --- Object Operations ---

	// Put stores an object from a reader. Returns the stored object metadata.
	Put(ctx context.Context, bucket, key string, r io.Reader, opts ...PutOption) (*ObjectInfo, error)

	// Get retrieves an object, returning a ReadCloser for the content.
	Get(ctx context.Context, bucket, key string, opts ...GetOption) (*ObjectReader, error)

	// Delete removes an object.
	Delete(ctx context.Context, bucket, key string, opts ...DeleteOption) error

	// Head returns object metadata without content.
	Head(ctx context.Context, bucket, key string) (*ObjectInfo, error)

	// List returns objects matching the given options.
	List(ctx context.Context, bucket string, opts ...ListOption) (*ObjectIterator, error)

	// Copy copies an object within or across buckets.
	Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...CopyOption) (*ObjectInfo, error)

	// --- Bucket Operations ---

	// CreateBucket creates a new bucket/container.
	CreateBucket(ctx context.Context, name string, opts ...BucketOption) error

	// DeleteBucket removes a bucket.
	DeleteBucket(ctx context.Context, name string) error

	// ListBuckets returns all accessible buckets.
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
}

// ObjectInfo holds the metadata returned by backend operations.
type ObjectInfo struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	ETag         string            `json:"etag"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	VersionID    string            `json:"version_id,omitempty"`
	StorageClass string            `json:"storage_class,omitempty"`
}

// ObjectReader wraps content with metadata.
type ObjectReader struct {
	io.ReadCloser
	Info *ObjectInfo
}

// BucketInfo holds bucket metadata.
type BucketInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ObjectIterator provides cursor-based listing of objects.
type ObjectIterator struct {
	objects []ObjectInfo
	cursor  int
	token   string
	done    bool
}

// NewObjectIterator creates an iterator from a slice of objects and an optional
// continuation token for the next page.
func NewObjectIterator(objects []ObjectInfo, nextToken string) *ObjectIterator {
	return &ObjectIterator{
		objects: objects,
		token:   nextToken,
	}
}

// Next returns the next object in the iterator.
// Returns nil, io.EOF when no more objects are available.
func (it *ObjectIterator) Next(_ context.Context) (*ObjectInfo, error) {
	if it.cursor >= len(it.objects) {
		return nil, io.EOF
	}
	obj := &it.objects[it.cursor]
	it.cursor++
	return obj, nil
}

// NextToken returns the continuation token for fetching the next page.
// Returns empty string when all results have been returned.
func (it *ObjectIterator) NextToken() string {
	return it.token
}

// Close releases any resources held by the iterator.
func (it *ObjectIterator) Close() error {
	it.done = true
	return nil
}

// All collects all remaining objects from the iterator into a slice.
func (it *ObjectIterator) All(ctx context.Context) ([]ObjectInfo, error) {
	var result []ObjectInfo
	for {
		obj, err := it.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		result = append(result, *obj)
	}
	return result, nil
}
