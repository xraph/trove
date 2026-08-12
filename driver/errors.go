package driver

import "errors"

// Sentinel errors that drivers must wrap so callers can classify failures
// with errors.Is, independent of which backend produced them.
//
// These live in the driver package rather than the root trove package so
// that out-of-tree drivers (which live in their own Go modules) can depend
// on them without importing the whole Trove facade. The root package
// re-exports every one of them, and since errors.Is compares by identity,
// trove.ErrObjectNotFound and driver.ErrObjectNotFound are interchangeable.
//
// The not-found sentinels form a small hierarchy: ErrObjectNotFound and
// ErrBucketNotFound both unwrap to ErrNotFound, so a caller that only cares
// whether something is missing can match the general sentinel, while a
// caller that needs to know what was missing can match the specific one.
//
//	errors.Is(err, driver.ErrObjectNotFound) // this exact object is gone
//	errors.Is(err, driver.ErrNotFound)       // something is gone — also true
var (
	// ErrNotFound reports that a requested resource does not exist.
	// It is the parent of ErrObjectNotFound and ErrBucketNotFound; match it
	// when any missing resource should be handled the same way.
	ErrNotFound error = errors.New("trove: not found")

	// ErrObjectNotFound reports that an object does not exist.
	// It unwraps to ErrNotFound.
	ErrObjectNotFound error = &categoryError{msg: "trove: object not found", parent: ErrNotFound}

	// ErrBucketNotFound reports that a bucket or container does not exist.
	// It unwraps to ErrNotFound.
	ErrBucketNotFound error = &categoryError{msg: "trove: bucket not found", parent: ErrNotFound}

	// ErrBucketExists reports that a bucket being created already exists.
	ErrBucketExists error = errors.New("trove: bucket already exists")

	// ErrPermissionDenied reports that the credentials in use are not
	// authorized for the operation. Retrying will not help until the
	// credentials or the backend's access policy change.
	ErrPermissionDenied error = errors.New("trove: permission denied")

	// ErrQuotaExceeded reports that a storage quota or rate limit was
	// exceeded. Unlike ErrPermissionDenied, retrying later may succeed.
	ErrQuotaExceeded error = errors.New("trove: quota exceeded")
)

// Permanent reports whether err describes a condition that retrying cannot
// resolve, so the caller should fail the operation now rather than spend its
// backoff budget on attempts that are certain to fail the same way.
//
// Permanent:
//
//   - ErrNotFound, and with it ErrObjectNotFound and ErrBucketNotFound — the
//     resource is gone and will not reappear on its own.
//   - ErrPermissionDenied — nothing changes until the credentials or the
//     backend's access policy do.
//   - ErrBucketExists — the bucket will not stop existing because you asked
//     again.
//
// Not permanent:
//
//   - ErrQuotaExceeded — the operation is refused now but may be granted
//     later. Retry it, ideally with a longer backoff than a network failure.
//   - Everything else, including unclassified errors. A driver that has not
//     classified a condition is assumed to have hit a transient one; treating
//     unrecognized errors as permanent would silently discard work whenever a
//     backend grows a failure mode Trove does not yet map.
//
// The quota check is evaluated first, so an error carrying both a quota
// signal and a not-found signal is reported as retryable.
//
//	if err != nil {
//	    if driver.Permanent(err) {
//	        return job.Fail(err)
//	    }
//	    return job.Retry(err)
//	}
func Permanent(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, ErrQuotaExceeded):
		return false
	case errors.Is(err, ErrNotFound),
		errors.Is(err, ErrPermissionDenied),
		errors.Is(err, ErrBucketExists):
		return true
	default:
		return false
	}
}

// categoryError is a sentinel that belongs to a broader category, so that
// errors.Is matches both the specific sentinel and its parent. errors.New
// cannot express this because it produces a leaf with nothing to unwrap.
type categoryError struct {
	msg    string
	parent error
}

func (e *categoryError) Error() string { return e.msg }

// Unwrap returns the broader category this sentinel belongs to.
func (e *categoryError) Unwrap() error { return e.parent }
