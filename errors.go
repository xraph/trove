package trove

import "errors"

// Sentinel errors returned by Trove operations.
var (
	// ErrNotFound is returned when a requested resource cannot be found.
	ErrNotFound = errors.New("trove: not found")

	// ErrBucketNotFound is returned when a bucket cannot be found.
	ErrBucketNotFound = errors.New("trove: bucket not found")

	// ErrBucketExists is returned when creating a bucket that already exists.
	ErrBucketExists = errors.New("trove: bucket already exists")

	// ErrObjectNotFound is returned when an object cannot be found.
	ErrObjectNotFound = errors.New("trove: object not found")

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

	// ErrQuotaExceeded is returned when a storage quota is exceeded.
	ErrQuotaExceeded = errors.New("trove: quota exceeded")

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
