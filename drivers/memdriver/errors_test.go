package memdriver_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
)

// TestErrorClassification asserts that every not-found path reports which
// resource was missing through the package sentinels. Callers such as a job
// scheduler use this to tell a permanently missing object from a transient
// backend failure, so an unclassified error here is a behavioral bug.
//
// The assertions use the trove.* aliases deliberately: they must be the same
// values as driver.*, and this is what downstream code actually imports.
func TestErrorClassification(t *testing.T) {
	ctx := context.Background()

	// setup gives every case a driver holding bucket "data" with object "present".
	setup := func(t *testing.T) *memdriver.MemDriver {
		t.Helper()
		d := memdriver.New()
		require.NoError(t, d.CreateBucket(ctx, "data"))
		_, err := d.Put(ctx, "data", "present", bytes.NewReader([]byte("x")))
		require.NoError(t, err)
		return d
	}

	tests := []struct {
		name string
		op   func(d *memdriver.MemDriver) error
		want error
	}{
		{
			name: "Get missing object",
			op:   func(d *memdriver.MemDriver) error { _, err := d.Get(ctx, "data", "missing"); return err },
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Get missing bucket",
			op:   func(d *memdriver.MemDriver) error { _, err := d.Get(ctx, "nope", "present"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Head missing object",
			op:   func(d *memdriver.MemDriver) error { _, err := d.Head(ctx, "data", "missing"); return err },
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Head missing bucket",
			op:   func(d *memdriver.MemDriver) error { _, err := d.Head(ctx, "nope", "present"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Put missing bucket",
			op: func(d *memdriver.MemDriver) error {
				_, err := d.Put(ctx, "nope", "k", bytes.NewReader(nil))
				return err
			},
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Delete missing bucket",
			op:   func(d *memdriver.MemDriver) error { return d.Delete(ctx, "nope", "present") },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "List missing bucket",
			op:   func(d *memdriver.MemDriver) error { _, err := d.List(ctx, "nope"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Copy missing source object",
			op: func(d *memdriver.MemDriver) error {
				_, err := d.Copy(ctx, "data", "missing", "data", "dst")
				return err
			},
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Copy missing source bucket",
			op: func(d *memdriver.MemDriver) error {
				_, err := d.Copy(ctx, "nope", "present", "data", "dst")
				return err
			},
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Copy missing destination bucket",
			op: func(d *memdriver.MemDriver) error {
				_, err := d.Copy(ctx, "data", "present", "nope", "dst")
				return err
			},
			want: trove.ErrBucketNotFound,
		},
		{
			name: "DeleteBucket missing bucket",
			op:   func(d *memdriver.MemDriver) error { return d.DeleteBucket(ctx, "nope") },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "CreateBucket existing bucket",
			op:   func(d *memdriver.MemDriver) error { return d.CreateBucket(ctx, "data") },
			want: trove.ErrBucketExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op(setup(t))
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.want)

			// Both not-found sentinels must roll up to the general one, which
			// is what a caller matches when any missing resource is fatal.
			if errors.Is(tt.want, trove.ErrNotFound) {
				assert.ErrorIs(t, err, trove.ErrNotFound)
			}
		})
	}
}

// TestSentinelAliasIdentity guards the re-export in the root package: if these
// ever became distinct values, every errors.Is check in downstream code would
// silently start returning false.
func TestSentinelAliasIdentity(t *testing.T) {
	assert.Same(t, driver.ErrNotFound, trove.ErrNotFound)
	assert.Same(t, driver.ErrObjectNotFound, trove.ErrObjectNotFound)
	assert.Same(t, driver.ErrBucketNotFound, trove.ErrBucketNotFound)
	assert.Same(t, driver.ErrBucketExists, trove.ErrBucketExists)
	assert.Same(t, driver.ErrPermissionDenied, trove.ErrPermissionDenied)
	assert.Same(t, driver.ErrQuotaExceeded, trove.ErrQuotaExceeded)
}

// TestDeleteIsIdempotent documents the deliberate exception: a missing object
// is not an error for Delete, only a missing bucket is.
func TestDeleteIsIdempotent(t *testing.T) {
	d := memdriver.New()
	require.NoError(t, d.CreateBucket(context.Background(), "data"))
	assert.NoError(t, d.Delete(context.Background(), "data", "never-existed"))
}
