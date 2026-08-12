package cas

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

// TestErrNotFoundClassification pins the relationship between the CAS sentinel
// and the package-wide one. Content missing from the CAS is as permanently gone
// as an object missing from a bucket, so a caller deciding whether to retry must
// not have to special-case the CAS to find that out.
func TestErrNotFoundClassification(t *testing.T) {
	ctx := context.Background()
	idx := NewMemoryIndex()

	tests := []struct {
		name string
		op   func() error
	}{
		{"Get", func() error { _, err := idx.Get(ctx, "missing"); return err }},
		{"IncrementRef", func() error { return idx.IncrementRef(ctx, "missing") }},
		{"DecrementRef", func() error { return idx.DecrementRef(ctx, "missing") }},
		{"Pin", func() error { return idx.Pin(ctx, "missing") }},
		{"Unpin", func() error { return idx.Unpin(ctx, "missing") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			require.Error(t, err)

			// The existing, more specific match still works.
			assert.ErrorIs(t, err, ErrNotFound)

			// And it now rolls up to the package-wide sentinel.
			assert.ErrorIs(t, err, driver.ErrNotFound)
			assert.True(t, driver.Permanent(err),
				"missing content will not appear on a retry")
		})
	}
}

// TestRetrieveWrapsNotFound covers the path through CAS itself rather than the
// index, since that is where a consumer meets the error.
func TestRetrieveWrapsNotFound(t *testing.T) {
	c := newTestCAS(t)

	_, err := c.Retrieve(context.Background(), "0000000000000000000000000000000000000000000000000000000000000000")
	require.Error(t, err)

	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, err, driver.ErrNotFound)
	assert.True(t, driver.Permanent(err))
	assert.Contains(t, err.Error(), "cas: hash not found",
		"the descriptive message must survive the wrapping")
}

// TestErrNotFoundIsNotAnObject keeps the CAS sentinel from being mistaken for a
// missing storage object: they are different resources, and only the general
// sentinel is shared.
func TestErrNotFoundIsNotAnObject(t *testing.T) {
	assert.False(t, errors.Is(ErrNotFound, driver.ErrObjectNotFound))
	assert.False(t, errors.Is(ErrNotFound, driver.ErrBucketNotFound))
}
