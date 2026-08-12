package azuredriver

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

// respErr builds the typed error the Azure SDK returns, carrying the service
// error code and the HTTP status.
func respErr(code bloberror.Code, status int) error {
	return &azcore.ResponseError{ErrorCode: string(code), StatusCode: status}
}

// TestClassifyErr checks the mapping from provider errors onto the Trove
// sentinels. Every case is a typed error — the driver must never fall back to
// matching message text, which is what it used to do for BlobNotFound.
func TestClassifyErr(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		bucket string
		key    string
		want   error // nil means "leave it unclassified"
	}{
		// Service error codes.
		{"BlobNotFound", respErr(bloberror.BlobNotFound, http.StatusNotFound), "b", "k", driver.ErrObjectNotFound},
		{"ContainerNotFound", respErr(bloberror.ContainerNotFound, http.StatusNotFound), "b", "", driver.ErrBucketNotFound},
		{"ContainerAlreadyExists", respErr(bloberror.ContainerAlreadyExists, http.StatusConflict), "b", "", driver.ErrBucketExists},
		{"AuthorizationFailure", respErr(bloberror.AuthorizationFailure, http.StatusForbidden), "b", "k", driver.ErrPermissionDenied},
		{"AuthenticationFailed", respErr(bloberror.AuthenticationFailed, http.StatusForbidden), "b", "k", driver.ErrPermissionDenied},
		{"InsufficientAccountPermissions", respErr(bloberror.InsufficientAccountPermissions, http.StatusForbidden), "b", "k", driver.ErrPermissionDenied},
		{"ServerBusy", respErr(bloberror.ServerBusy, http.StatusServiceUnavailable), "b", "k", driver.ErrQuotaExceeded},

		// The code survives wrapping, which is how it reaches a caller.
		{
			name:   "wrapped BlobNotFound",
			err:    fmt.Errorf("download: %w", respErr(bloberror.BlobNotFound, http.StatusNotFound)),
			bucket: "b", key: "k", want: driver.ErrObjectNotFound,
		},

		// Status-only fallback, for an emulator or proxy that sends no code.
		{"404 with key", &azcore.ResponseError{StatusCode: http.StatusNotFound}, "b", "k", driver.ErrObjectNotFound},
		{"404 without key", &azcore.ResponseError{StatusCode: http.StatusNotFound}, "b", "", driver.ErrBucketNotFound},
		{"403", &azcore.ResponseError{StatusCode: http.StatusForbidden}, "b", "k", driver.ErrPermissionDenied},
		{"429", &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}, "b", "k", driver.ErrQuotaExceeded},

		// Everything else stays unclassified so the caller wraps it as opaque.
		{"500", &azcore.ResponseError{StatusCode: http.StatusInternalServerError}, "b", "k", nil},
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

// TestClassifyErrIgnoresMessageText guards against regressing to the substring
// match this driver previously used: a message merely containing "BlobNotFound"
// is not a classified not-found, only the typed code is.
func TestClassifyErrIgnoresMessageText(t *testing.T) {
	assert.NoError(t, classifyErr(errors.New("server said BlobNotFound, maybe"), "b", "k"))
}
