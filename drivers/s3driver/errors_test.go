package s3driver

import (
	"errors"
	"net/http"
	"testing"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

// apiErr builds an error carrying only a service error code, which is how
// S3-compatible services (MinIO, Ceph, R2) report failures that Amazon S3
// would return as a modeled type.
func apiErr(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: code}
}

// statusErr builds an error carrying only an HTTP status, for services that
// return neither a modeled type nor a recognized code.
func statusErr(status int) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
			Err:      errors.New("request failed"),
		},
	}
}

// TestClassifyErr checks the mapping from provider errors onto the Trove
// sentinels. Every case is a typed error or a status code — the driver must
// never fall back to matching message text.
func TestClassifyErr(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		bucket string
		key    string
		want   error // nil means "leave it unclassified"
	}{
		// Modeled types, as returned by Amazon S3 itself.
		{"NoSuchKey", &types.NoSuchKey{}, "b", "k", driver.ErrObjectNotFound},
		{"NotFound from HeadObject", &types.NotFound{}, "b", "k", driver.ErrObjectNotFound},
		{"NoSuchBucket", &types.NoSuchBucket{}, "b", "", driver.ErrBucketNotFound},
		{"NoSuchUpload", &types.NoSuchUpload{}, "b", "k", driver.ErrNotFound},
		{"BucketAlreadyExists", &types.BucketAlreadyExists{}, "b", "", driver.ErrBucketExists},
		{"BucketAlreadyOwnedByYou", &types.BucketAlreadyOwnedByYou{}, "b", "", driver.ErrBucketExists},

		// Error codes, as returned by S3-compatible services.
		{"code NoSuchKey", apiErr("NoSuchKey"), "b", "k", driver.ErrObjectNotFound},
		{"code NoSuchBucket", apiErr("NoSuchBucket"), "b", "", driver.ErrBucketNotFound},
		{"code AccessDenied", apiErr("AccessDenied"), "b", "k", driver.ErrPermissionDenied},
		{"code InvalidAccessKeyId", apiErr("InvalidAccessKeyId"), "b", "k", driver.ErrPermissionDenied},
		{"code SlowDown", apiErr("SlowDown"), "b", "k", driver.ErrQuotaExceeded},
		{"code QuotaExceeded", apiErr("QuotaExceeded"), "b", "k", driver.ErrQuotaExceeded},

		// Status-only fallback.
		{"404 with key", statusErr(http.StatusNotFound), "b", "k", driver.ErrObjectNotFound},
		{"404 without key", statusErr(http.StatusNotFound), "b", "", driver.ErrBucketNotFound},
		{"403", statusErr(http.StatusForbidden), "b", "k", driver.ErrPermissionDenied},
		{"429", statusErr(http.StatusTooManyRequests), "b", "k", driver.ErrQuotaExceeded},

		// Everything else stays unclassified so the caller wraps it as opaque.
		{"500", statusErr(http.StatusInternalServerError), "b", "k", nil},
		{"unknown code", apiErr("InternalError"), "b", "k", nil},
		{"plain error", errors.New("connection reset"), "b", "k", nil},
		{"nil", nil, "b", "k", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyErr(tt.err, tt.bucket, tt.key)

			if tt.want == nil {
				assert.NoError(t, got, "error should be left unclassified")
				return
			}

			require.Error(t, got)
			assert.ErrorIs(t, got, tt.want)

			if tt.want == driver.ErrObjectNotFound || tt.want == driver.ErrBucketNotFound {
				assert.ErrorIs(t, got, driver.ErrNotFound)
			}
		})
	}
}

// TestClassifyErrIgnoresMessageText guards against regressing to substring
// matching: an error that merely says "not found" is not a classified
// not-found, only a typed one is.
func TestClassifyErrIgnoresMessageText(t *testing.T) {
	assert.NoError(t, classifyErr(errors.New("the key was not found somewhere"), "b", "k"))
}
