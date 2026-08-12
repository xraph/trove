package azuredriver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"

	"github.com/xraph/trove/driver"
)

// classifyErr maps an Azure Blob Storage error onto the Trove sentinels so
// callers can classify it with errors.Is. It returns nil when the error is not
// one of the classified conditions, leaving the caller to wrap it as an opaque
// failure.
//
// Azure returns a service error code in the response, which bloberror.HasCode
// reads off the typed azcore.ResponseError — matching on it is exact, unlike
// searching the rendered message for "BlobNotFound". The HTTP status is only
// consulted when no recognized code is present, e.g. from an emulator or a
// proxy that answers before the service does.
//
// key may be empty for container-scoped operations; an ambiguous 404 is then
// attributed to the container rather than a blob.
func classifyErr(err error, bucket, key string) error {
	if err == nil {
		return nil
	}

	switch {
	case bloberror.HasCode(err, bloberror.BlobNotFound):
		return objectNotFound(bucket, key)
	case bloberror.HasCode(err, bloberror.ContainerNotFound):
		return bucketNotFound(bucket)
	case bloberror.HasCode(err, bloberror.ContainerAlreadyExists):
		return fmt.Errorf("azuredriver: bucket %q already exists: %w", bucket, driver.ErrBucketExists)
	case bloberror.HasCode(err,
		bloberror.AuthorizationFailure,
		bloberror.AuthenticationFailed,
		bloberror.InsufficientAccountPermissions,
		bloberror.InvalidAuthenticationInfo,
		bloberror.AccountIsDisabled):
		return permissionDenied(bucket, key)
	case bloberror.HasCode(err, bloberror.ServerBusy):
		return quotaExceeded(bucket, key)
	}

	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.StatusCode {
		case http.StatusNotFound:
			if key == "" {
				return bucketNotFound(bucket)
			}
			return objectNotFound(bucket, key)
		case http.StatusForbidden, http.StatusUnauthorized:
			return permissionDenied(bucket, key)
		case http.StatusTooManyRequests:
			return quotaExceeded(bucket, key)
		}
	}

	return nil
}

func objectNotFound(bucket, key string) error {
	return fmt.Errorf("azuredriver: object %q not found in bucket %q: %w", key, bucket, driver.ErrObjectNotFound)
}

func bucketNotFound(bucket string) error {
	return fmt.Errorf("azuredriver: bucket %q not found: %w", bucket, driver.ErrBucketNotFound)
}

func permissionDenied(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("azuredriver: permission denied for bucket %q: %w", bucket, driver.ErrPermissionDenied)
	}
	return fmt.Errorf("azuredriver: permission denied for object %q in bucket %q: %w", key, bucket, driver.ErrPermissionDenied)
}

func quotaExceeded(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("azuredriver: quota exceeded for bucket %q: %w", bucket, driver.ErrQuotaExceeded)
	}
	return fmt.Errorf("azuredriver: quota exceeded for object %q in bucket %q: %w", key, bucket, driver.ErrQuotaExceeded)
}
