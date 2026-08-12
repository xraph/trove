package trove

import (
	"errors"

	"github.com/xraph/trove/driver"
)

// Errors that drivers classify. These are aliases of the sentinels defined
// in the driver package — the canonical values live there so that drivers in
// their own modules can wrap them without importing this package. Because
// errors.Is compares by identity, trove.ErrObjectNotFound and
// driver.ErrObjectNotFound match each other.
//
// ErrObjectNotFound and ErrBucketNotFound unwrap to ErrNotFound, so a caller
// that only needs to know "this is permanently missing" can match ErrNotFound
// and catch both.
var (
	// ErrNotFound is returned when a requested resource cannot be found.
	// It is the parent of ErrBucketNotFound and ErrObjectNotFound.
	ErrNotFound = driver.ErrNotFound

	// ErrBucketNotFound is returned when a bucket cannot be found.
	ErrBucketNotFound = driver.ErrBucketNotFound

	// ErrBucketExists is returned when creating a bucket that already exists.
	ErrBucketExists = driver.ErrBucketExists

	// ErrObjectNotFound is returned when an object cannot be found.
	ErrObjectNotFound = driver.ErrObjectNotFound

	// ErrPermissionDenied is returned when the backend rejects the operation
	// as unauthorized. Retrying will not help until access is granted.
	ErrPermissionDenied = driver.ErrPermissionDenied

	// ErrQuotaExceeded is returned when a storage quota or rate limit is
	// exceeded. Retrying later may succeed.
	ErrQuotaExceeded = driver.ErrQuotaExceeded
)

// Permanent reports whether err describes a condition that retrying cannot
// resolve, so the caller should fail the operation now rather than spend its
// backoff budget on attempts certain to fail the same way.
//
// A missing object, a permission failure, and an already-existing bucket are
// permanent. An exceeded quota is not: it may be granted later. Neither is an
// unclassified error — a condition Trove has not mapped is assumed transient,
// because treating it as permanent would silently discard work whenever a
// backend grows a failure mode the drivers do not yet recognize.
//
//	reader, err := t.Get(ctx, "artifacts", key)
//	if err != nil {
//	    if trove.Permanent(err) {
//	        return job.Fail(err) // straight to the dead letter queue
//	    }
//	    return job.Retry(err) // transient — back off and try again
//	}
//
// See the driver package for the full classification.
func Permanent(err error) bool { return driver.Permanent(err) }

// Sentinel errors returned by Trove operations.
var (
	// ErrKeyEmpty is returned when an object key is empty.
	ErrKeyEmpty = errors.New("trove: key is required")

	// ErrBucketEmpty is returned when a bucket name is empty.
	ErrBucketEmpty = errors.New("trove: bucket name is required")

	// ErrDriverClosed is returned when an operation is attempted on a closed driver.
	ErrDriverClosed = errors.New("trove: driver is closed")

	// ErrNilDriver is returned when Open is called with a nil driver.
	ErrNilDriver = errors.New("trove: driver is required")

	// ErrInvalidDSN is returned when a DSN string cannot be parsed.
	ErrInvalidDSN = errors.New("trove: invalid DSN")

	// ErrChecksumMismatch is returned when a checksum verification fails.
	ErrChecksumMismatch = errors.New("trove: checksum mismatch")

	// ErrContentBlocked is returned when content is rejected by scanning middleware.
	ErrContentBlocked = errors.New("trove: content blocked")

	// ErrStreamClosed is returned when writing to a closed stream.
	ErrStreamClosed = errors.New("trove: stream is closed")

	// ErrUploadExpired is returned when a resumable upload session has expired.
	ErrUploadExpired = errors.New("trove: upload session expired")

	// ErrBackendNotFound is returned when a named backend cannot be found.
	ErrBackendNotFound = errors.New("trove: backend not found")

	// ErrPoolClosed is returned when an operation targets a closed stream pool.
	ErrPoolClosed = errors.New("trove: stream pool is closed")

	// ErrStreamNotActive is returned when an operation requires an active stream.
	ErrStreamNotActive = errors.New("trove: stream is not active")

	// ErrMaxStreamsReached is returned when the pool's concurrency limit is hit.
	ErrMaxStreamsReached = errors.New("trove: maximum concurrent streams reached")
)
