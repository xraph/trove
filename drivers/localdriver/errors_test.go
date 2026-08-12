package localdriver_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/localdriver"
)

// TestErrorClassification asserts that every not-found path reports which
// resource was missing through the package sentinels, so a caller can tell a
// permanently missing object from a transient backend failure.
func TestErrorClassification(t *testing.T) {
	ctx := context.Background()

	// setup gives every case a driver holding bucket "data" with object "present".
	setup := func(t *testing.T) *localdriver.LocalDriver {
		t.Helper()
		d := localdriver.New()
		d.SetRootDir(t.TempDir())
		require.NoError(t, d.CreateBucket(ctx, "data"))
		_, err := d.Put(ctx, "data", "present", bytes.NewReader([]byte("x")))
		require.NoError(t, err)
		return d
	}

	tests := []struct {
		name string
		op   func(d *localdriver.LocalDriver) error
		want error
	}{
		{
			name: "Get missing object",
			op:   func(d *localdriver.LocalDriver) error { _, err := d.Get(ctx, "data", "missing"); return err },
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Get missing bucket",
			op:   func(d *localdriver.LocalDriver) error { _, err := d.Get(ctx, "nope", "present"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Head missing object",
			op:   func(d *localdriver.LocalDriver) error { _, err := d.Head(ctx, "data", "missing"); return err },
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Head missing bucket",
			op:   func(d *localdriver.LocalDriver) error { _, err := d.Head(ctx, "nope", "present"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "List missing bucket",
			op:   func(d *localdriver.LocalDriver) error { _, err := d.List(ctx, "nope"); return err },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "Copy missing source object",
			op: func(d *localdriver.LocalDriver) error {
				_, err := d.Copy(ctx, "data", "missing", "data", "dst")
				return err
			},
			want: trove.ErrObjectNotFound,
		},
		{
			name: "Copy missing source bucket",
			op: func(d *localdriver.LocalDriver) error {
				_, err := d.Copy(ctx, "nope", "present", "data", "dst")
				return err
			},
			want: trove.ErrBucketNotFound,
		},
		{
			name: "DeleteBucket missing bucket",
			op:   func(d *localdriver.LocalDriver) error { return d.DeleteBucket(ctx, "nope") },
			want: trove.ErrBucketNotFound,
		},
		{
			name: "CreateBucket existing bucket",
			op:   func(d *localdriver.LocalDriver) error { return d.CreateBucket(ctx, "data") },
			want: trove.ErrBucketExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op(setup(t))
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.want)

			if errors.Is(tt.want, trove.ErrNotFound) {
				assert.ErrorIs(t, err, trove.ErrNotFound)
			}
		})
	}
}

// TestPermissionDeniedClassification covers the other condition a caller must
// not retry: an unreadable object is a permission failure, not a missing one.
func TestPermissionDeniedClassification(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions are not enforced")
	}

	ctx := context.Background()
	root := t.TempDir()

	d := localdriver.New()
	d.SetRootDir(root)
	require.NoError(t, d.CreateBucket(ctx, "data"))
	_, err := d.Put(ctx, "data", "secret", bytes.NewReader([]byte("x")))
	require.NoError(t, err)

	objPath := filepath.Join(root, "data", "secret")
	require.NoError(t, os.Chmod(objPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(objPath, 0o600) })

	_, err = d.Get(ctx, "data", "secret")
	require.Error(t, err)
	assert.ErrorIs(t, err, trove.ErrPermissionDenied)
	assert.NotErrorIs(t, err, trove.ErrNotFound,
		"an unreadable object exists — reporting it as missing would send the caller down the wrong path")
}
