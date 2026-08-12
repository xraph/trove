package driver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

// TestSentinelHierarchy pins the relationships callers depend on: the specific
// not-found sentinels must also match the general one, and must not match each
// other.
func TestSentinelHierarchy(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{"object not found matches itself", driver.ErrObjectNotFound, driver.ErrObjectNotFound, true},
		{"object not found matches general", driver.ErrObjectNotFound, driver.ErrNotFound, true},
		{"bucket not found matches itself", driver.ErrBucketNotFound, driver.ErrBucketNotFound, true},
		{"bucket not found matches general", driver.ErrBucketNotFound, driver.ErrNotFound, true},
		{"object not found is not a bucket", driver.ErrObjectNotFound, driver.ErrBucketNotFound, false},
		{"bucket not found is not an object", driver.ErrBucketNotFound, driver.ErrObjectNotFound, false},
		{"general is not specific", driver.ErrNotFound, driver.ErrObjectNotFound, false},
		{"permission denied is not not-found", driver.ErrPermissionDenied, driver.ErrNotFound, false},
		{"quota exceeded is not not-found", driver.ErrQuotaExceeded, driver.ErrNotFound, false},
		{"quota exceeded is not permission denied", driver.ErrQuotaExceeded, driver.ErrPermissionDenied, false},
		{"bucket exists is not not-found", driver.ErrBucketExists, driver.ErrNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errors.Is(tt.err, tt.target))
		})
	}
}

// TestPermanent pins the retry taxonomy. A wrong answer here is expensive in
// both directions: a permanent failure classed as retryable burns the caller's
// whole backoff budget, and a transient one classed as permanent discards work
// that would have succeeded.
func TestPermanent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Permanent: retrying cannot change the outcome.
		{"object not found", driver.ErrObjectNotFound, true},
		{"bucket not found", driver.ErrBucketNotFound, true},
		{"general not found", driver.ErrNotFound, true},
		{"permission denied", driver.ErrPermissionDenied, true},
		{"bucket exists", driver.ErrBucketExists, true},

		// Wrapped sentinels are what a caller actually receives.
		{
			name: "wrapped object not found",
			err:  fmt.Errorf("get %q: %w", "k", driver.ErrObjectNotFound),
			want: true,
		},
		{
			name: "deeply wrapped permission denied",
			err:  fmt.Errorf("job: %w", fmt.Errorf("s3driver: %w", driver.ErrPermissionDenied)),
			want: true,
		},

		// Retryable: the condition may clear on its own.
		{"quota exceeded", driver.ErrQuotaExceeded, false},
		{"wrapped quota exceeded", fmt.Errorf("put: %w", driver.ErrQuotaExceeded), false},

		// Unclassified errors are assumed transient.
		{"opaque backend error", errors.New("connection reset by peer"), false},
		{"context deadline", context.DeadlineExceeded, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, driver.Permanent(tt.err))
		})
	}
}

// TestPermanentPrefersRetry documents the precedence rule: when an error
// carries both a quota signal and a not-found signal, the retryable reading
// wins, so the caller backs off instead of discarding the work.
func TestPermanentPrefersRetry(t *testing.T) {
	err := fmt.Errorf("%w (while %w)", driver.ErrQuotaExceeded, driver.ErrObjectNotFound)

	require.ErrorIs(t, err, driver.ErrQuotaExceeded)
	require.ErrorIs(t, err, driver.ErrObjectNotFound)
	assert.False(t, driver.Permanent(err))
}

// TestSentinelSurvivesWrapping is the property drivers rely on: a sentinel
// wrapped in a descriptive message is still matchable, at any depth.
func TestSentinelSurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("memdriver: object %q not found in bucket %q: %w", "k", "b", driver.ErrObjectNotFound)
	err = fmt.Errorf("trove: get: %w", err)

	assert.ErrorIs(t, err, driver.ErrObjectNotFound)
	assert.ErrorIs(t, err, driver.ErrNotFound)
	assert.NotErrorIs(t, err, driver.ErrBucketNotFound)
	assert.Contains(t, err.Error(), `object "k" not found in bucket "b"`,
		"wrapping must preserve the descriptive message, not replace it")
}
