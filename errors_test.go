package trove_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/memdriver"
)

// TestNotFoundThroughFacade exercises the path a consumer actually takes:
// through the Trove handle rather than against a driver directly. The facade
// must pass the driver's classification through untouched, otherwise callers
// cannot tell a permanently missing object from a transient failure.
func TestNotFoundThroughFacade(t *testing.T) {
	ctx := context.Background()

	drv := memdriver.New()
	require.NoError(t, drv.CreateBucket(ctx, "artifacts"))

	tr, err := trove.Open(drv, trove.WithDefaultBucket("artifacts"))
	require.NoError(t, err)

	tests := []struct {
		name string
		op   func() error
		want error
	}{
		{
			name: "Get missing object",
			op:   func() error { _, err := tr.Get(ctx, "artifacts", "gone"); return err },
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Head missing object",
			op:   func() error { _, err := tr.Head(ctx, "artifacts", "gone"); return err },
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Get missing bucket",
			op:   func() error { _, err := tr.Get(ctx, "no-such-bucket", "gone"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "List missing bucket",
			op:   func() error { _, err := tr.List(ctx, "no-such-bucket"); return err },
			want: trove.ErrBucketNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.want)
			assert.ErrorIs(t, err, trove.ErrNotFound,
				"a caller matching the general sentinel must catch this")
			assert.True(t, trove.Permanent(err),
				"a missing resource must not be retried")
		})
	}
}

// TestPermanentThroughFacade exercises the decision a worker actually makes:
// fail the job now, or back off and retry. A missing input must reach the dead
// letter queue on the first attempt rather than after the whole retry budget.
func TestPermanentThroughFacade(t *testing.T) {
	ctx := context.Background()

	drv := memdriver.New()
	require.NoError(t, drv.CreateBucket(ctx, "artifacts"))

	tr, err := trove.Open(drv)
	require.NoError(t, err)

	// A deleted input: permanent, so the job fails immediately.
	_, err = tr.Get(ctx, "artifacts", "deleted-input")
	require.Error(t, err)
	assert.True(t, trove.Permanent(err))

	// The success path must not be mistaken for a failure.
	assert.False(t, trove.Permanent(nil))

	// An error Trove has not classified is assumed transient, so the work is
	// retried rather than discarded.
	assert.False(t, trove.Permanent(errors.New("dial tcp: connection refused")))
}
