package gcsdriver

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"

	"github.com/xraph/trove/driver"
)

// gapi builds a typed googleapi.Error with an optional reason, which is what
// separates a 403 raised for lacking permission from one raised for exceeding
// a rate limit.
func gapi(code int, reasons ...string) error {
	e := &googleapi.Error{Code: code, Message: http.StatusText(code)}
	for _, r := range reasons {
		e.Errors = append(e.Errors, googleapi.ErrorItem{Reason: r})
	}
	return e
}

// TestClassifyErr checks the mapping from provider errors onto the Trove
// sentinels. Every case is a typed error — the driver must never fall back to
// matching message text.
func TestClassifyErr(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		bucket string
		key    string
		want   error // nil means "leave it unclassified"
	}{
		// The storage client's own sentinels.
		{"ErrObjectNotExist", storage.ErrObjectNotExist, "b", "k", driver.ErrObjectNotFound},
		{"ErrBucketNotExist", storage.ErrBucketNotExist, "b", "", driver.ErrBucketNotFound},
		{"wrapped ErrObjectNotExist", fmt.Errorf("read: %w", storage.ErrObjectNotExist), "b", "k", driver.ErrObjectNotFound},

		// Typed API errors.
		{"404 with key", gapi(http.StatusNotFound), "b", "k", driver.ErrObjectNotFound},
		{"404 without key", gapi(http.StatusNotFound), "b", "", driver.ErrBucketNotFound},
		{"403 forbidden", gapi(http.StatusForbidden, "forbidden"), "b", "k", driver.ErrPermissionDenied},
		{"403 no reason", gapi(http.StatusForbidden), "b", "k", driver.ErrPermissionDenied},
		{"401", gapi(http.StatusUnauthorized), "b", "k", driver.ErrPermissionDenied},
		{"429", gapi(http.StatusTooManyRequests), "b", "k", driver.ErrQuotaExceeded},

		// GCS reports quota exhaustion as a 403; only the reason distinguishes it.
		{"403 rateLimitExceeded", gapi(http.StatusForbidden, "rateLimitExceeded"), "b", "k", driver.ErrQuotaExceeded},
		{"403 quotaExceeded", gapi(http.StatusForbidden, "quotaExceeded"), "b", "k", driver.ErrQuotaExceeded},

		// Everything else stays unclassified so the caller wraps it as opaque.
		{"500", gapi(http.StatusInternalServerError), "b", "k", nil},
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
	assert.NoError(t, classifyErr(errors.New("object doesn't exist, probably"), "b", "k"))
}
