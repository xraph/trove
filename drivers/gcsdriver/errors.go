package gcsdriver

import (
	"errors"
	"fmt"
	"net/http"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"

	"github.com/xraph/trove/driver"
)

// classifyErr maps a GCS API error onto the Trove sentinels so callers can
// classify it with errors.Is. It returns nil when the error is not one of the
// classified conditions, leaving the caller to wrap it as an opaque failure.
//
// The storage client reports missing resources with its own sentinels, which
// are checked first; everything else is read off the typed googleapi.Error
// rather than its message. GCS answers both "you may not do this" and "you
// have done this too often" with 403, so the reason code on the error item is
// what separates a permission failure from a quota failure.
//
// key may be empty for bucket-scoped operations; an ambiguous 404 is then
// attributed to the bucket rather than an object.
func classifyErr(err error, bucket, key string) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, storage.ErrObjectNotExist):
		return objectNotFound(bucket, key)
	case errors.Is(err, storage.ErrBucketNotExist):
		return bucketNotFound(bucket)
	}

	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case http.StatusNotFound:
			if key == "" {
				return bucketNotFound(bucket)
			}
			return objectNotFound(bucket, key)
		case http.StatusForbidden:
			if isQuotaReason(apiErr) {
				return quotaExceeded(bucket, key)
			}
			return permissionDenied(bucket, key)
		case http.StatusUnauthorized:
			return permissionDenied(bucket, key)
		case http.StatusTooManyRequests:
			return quotaExceeded(bucket, key)
		}
	}

	return nil
}

// classifyCreateBucketErr is classifyErr for CreateBucket, plus the one
// mapping only a create can make: GCS answers 409 Conflict when the bucket
// already exists, whether you own it or someone else does. classifyErr can't
// map 409 on its own, because DeleteBucket on a bucket that still holds
// objects is a 409 as well, and that bucket doesn't "already exist".
func classifyCreateBucketErr(err error, bucket string) error {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
		return fmt.Errorf("gcsdriver: bucket %q already exists: %w", bucket, driver.ErrBucketExists)
	}
	return classifyErr(err, bucket, "")
}

// isQuotaReason reports whether a 403 was raised for exceeding a quota or
// rate limit rather than for lacking permission.
func isQuotaReason(apiErr *googleapi.Error) bool {
	for _, item := range apiErr.Errors {
		switch item.Reason {
		case "quotaExceeded", "rateLimitExceeded", "userRateLimitExceeded", "dailyLimitExceeded":
			return true
		}
	}
	return false
}

func objectNotFound(bucket, key string) error {
	return fmt.Errorf("gcsdriver: object %q not found in bucket %q: %w", key, bucket, driver.ErrObjectNotFound)
}

func bucketNotFound(bucket string) error {
	return fmt.Errorf("gcsdriver: bucket %q not found: %w", bucket, driver.ErrBucketNotFound)
}

func permissionDenied(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("gcsdriver: permission denied for bucket %q: %w", bucket, driver.ErrPermissionDenied)
	}
	return fmt.Errorf("gcsdriver: permission denied for object %q in bucket %q: %w", key, bucket, driver.ErrPermissionDenied)
}

func quotaExceeded(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("gcsdriver: quota exceeded for bucket %q: %w", bucket, driver.ErrQuotaExceeded)
	}
	return fmt.Errorf("gcsdriver: quota exceeded for object %q in bucket %q: %w", key, bucket, driver.ErrQuotaExceeded)
}
