package s3driver

import (
	"errors"
	"fmt"
	"net/http"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/xraph/trove/driver"
)

// classifyErr maps an S3 API error onto the Trove sentinels so callers can
// classify it with errors.Is. It returns nil when the error is not one of the
// classified conditions, leaving the caller to wrap it as an opaque failure.
//
// Three signals are consulted, in decreasing order of reliability:
//
//  1. The modeled error types (NoSuchKey, NoSuchBucket, NotFound, …), which
//     Amazon S3 itself returns.
//  2. The API error code, because S3-compatible services (MinIO, Ceph, R2)
//     often return an equivalent code without the modeled shape.
//  3. The HTTP status, for services that return neither.
//
// key may be empty for bucket-scoped operations; an ambiguous 404 is then
// attributed to the bucket rather than an object.
func classifyErr(err error, bucket, key string) error {
	if err == nil {
		return nil
	}

	var (
		noSuchKey    *types.NoSuchKey
		notFound     *types.NotFound
		noSuchBucket *types.NoSuchBucket
		noSuchUpload *types.NoSuchUpload
		bucketExists *types.BucketAlreadyExists
		bucketOwned  *types.BucketAlreadyOwnedByYou
	)

	switch {
	case errors.As(err, &noSuchKey), errors.As(err, &notFound):
		return objectNotFound(bucket, key)
	case errors.As(err, &noSuchBucket):
		return bucketNotFound(bucket)
	case errors.As(err, &noSuchUpload):
		return fmt.Errorf("s3driver: multipart upload for %q not found: %w", key, driver.ErrNotFound)
	case errors.As(err, &bucketExists), errors.As(err, &bucketOwned):
		return fmt.Errorf("s3driver: bucket %q already exists: %w", bucket, driver.ErrBucketExists)
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return objectNotFound(bucket, key)
		case "NoSuchBucket":
			return bucketNotFound(bucket)
		case "NoSuchUpload":
			return fmt.Errorf("s3driver: multipart upload for %q not found: %w", key, driver.ErrNotFound)
		case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
			return fmt.Errorf("s3driver: bucket %q already exists: %w", bucket, driver.ErrBucketExists)
		case "AccessDenied", "AllAccessDisabled", "InvalidAccessKeyId", "SignatureDoesNotMatch", "UnauthorizedAccess":
			return permissionDenied(bucket, key)
		case "QuotaExceeded", "SlowDown", "TooManyRequests", "RequestLimitExceeded", "ServiceQuotaExceededException":
			return quotaExceeded(bucket, key)
		}
	}

	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case http.StatusNotFound:
			if key == "" {
				return bucketNotFound(bucket)
			}
			return objectNotFound(bucket, key)
		case http.StatusForbidden:
			return permissionDenied(bucket, key)
		case http.StatusTooManyRequests:
			return quotaExceeded(bucket, key)
		}
	}

	return nil
}

func objectNotFound(bucket, key string) error {
	return fmt.Errorf("s3driver: object %q not found in bucket %q: %w", key, bucket, driver.ErrObjectNotFound)
}

func bucketNotFound(bucket string) error {
	return fmt.Errorf("s3driver: bucket %q not found: %w", bucket, driver.ErrBucketNotFound)
}

func permissionDenied(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("s3driver: permission denied for bucket %q: %w", bucket, driver.ErrPermissionDenied)
	}
	return fmt.Errorf("s3driver: permission denied for object %q in bucket %q: %w", key, bucket, driver.ErrPermissionDenied)
}

func quotaExceeded(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("s3driver: quota exceeded for bucket %q: %w", bucket, driver.ErrQuotaExceeded)
	}
	return fmt.Errorf("s3driver: quota exceeded for object %q in bucket %q: %w", key, bucket, driver.ErrQuotaExceeded)
}
