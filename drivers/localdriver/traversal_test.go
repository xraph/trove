package localdriver_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/localdriver"
)

// The local driver resolves every bucket and key against a root directory on
// a real filesystem, so a caller that controls either one controls a path.
// These tests assert the containment rule at both boundaries: a key stays
// inside its bucket, and a bucket stays inside the root.
//
// Each case checks the escape did not happen as well as the error, because an
// error alone does not prove containment — an operation can fail for an
// unrelated reason while still having touched the file it should not have.

// traversalFixture is a root with one populated bucket, a second bucket to
// serve as a cross-tenant target, and a file planted outside the root.
type traversalFixture struct {
	drv *localdriver.LocalDriver

	base    string // parent of root — nothing here is reachable
	root    string // driver root
	outside string // base/outside.txt, one level above the root
	victim  string // root/tenant-b/secret.txt, a sibling bucket's object
}

const (
	outsideContent = "top secret, outside the root"
	victimContent  = "tenant b's private object"
)

func newTraversalFixture(t *testing.T) *traversalFixture {
	t.Helper()

	base := t.TempDir()
	root := filepath.Join(base, "root")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tenant-a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tenant-b"), 0o750))

	outside := filepath.Join(base, "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte(outsideContent), 0o600))

	victim := filepath.Join(root, "tenant-b", "secret.txt")
	require.NoError(t, os.WriteFile(victim, []byte(victimContent), 0o600))

	drv := localdriver.New()
	drv.SetRootDir(root)
	t.Cleanup(func() { _ = drv.Close(context.Background()) })

	return &traversalFixture{drv: drv, base: base, root: root, outside: outside, victim: victim}
}

// assertIntact verifies the two files an escape would reach are still exactly
// as they were, and that nothing new was written beside them.
func (f *traversalFixture) assertIntact(t *testing.T) {
	t.Helper()

	got, err := os.ReadFile(f.outside)
	require.NoError(t, err, "file outside the root was deleted")
	require.Equal(t, outsideContent, string(got), "file outside the root was overwritten")

	got, err = os.ReadFile(f.victim)
	require.NoError(t, err, "a sibling bucket's object was deleted")
	require.Equal(t, victimContent, string(got), "a sibling bucket's object was overwritten")

	entries, err := os.ReadDir(f.base)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{"root", "outside.txt"}, names,
		"an operation created an entry outside the root")
}

// traversalKeys are object keys that must never resolve to a path outside the
// bucket they were addressed to.
var traversalKeys = []struct {
	name string
	key  string
}{
	{"parent", "../x"},
	{"escapes the bucket into the root", "a/../../x"},
	{"escapes the root", "../../outside.txt"},
	{"reaches a sibling bucket", "../tenant-b/secret.txt"},
	{"trailing climb", "a/b/../../../outside.txt"},
	{"bare parent", ".."},
	{"names the bucket itself", "."},
	{"absolute", "/etc/passwd"},
	{"absolute with climb", "/../../etc/passwd"},
	{"empty", ""},
	{"nul byte", "a\x00b"},
}

// traversalBuckets are bucket names that must never resolve to a path outside
// the root. "." is included because DeleteBucket would otherwise hand the
// entire storage root to os.RemoveAll.
var traversalBuckets = []struct {
	name   string
	bucket string
}{
	{"parent", "../evil"},
	{"bare parent", ".."},
	{"names the root itself", "."},
	{"deep climb", "a/../../evil"},
	{"escapes then returns", "../root"},
	{"absolute", "/tmp"},
	{"empty", ""},
	{"nul byte", "a\x00b"},
}

func TestTraversal_ObjectOperations(t *testing.T) {
	ctx := context.Background()

	// Every operation that takes a bucket and a key, exercised through the
	// same tainted key so no operation can be quietly left unguarded.
	ops := map[string]func(f *traversalFixture, key string) error{
		"Put": func(f *traversalFixture, key string) error {
			_, err := f.drv.Put(ctx, "tenant-a", key, strings.NewReader("pwned"))
			return err
		},
		"Get": func(f *traversalFixture, key string) error {
			r, err := f.drv.Get(ctx, "tenant-a", key)
			if err == nil {
				_ = r.Close()
			}
			return err
		},
		"Head": func(f *traversalFixture, key string) error {
			_, err := f.drv.Head(ctx, "tenant-a", key)
			return err
		},
		"Delete": func(f *traversalFixture, key string) error {
			return f.drv.Delete(ctx, "tenant-a", key)
		},
		"Copy/source": func(f *traversalFixture, key string) error {
			_, err := f.drv.Copy(ctx, "tenant-a", key, "tenant-a", "stolen.txt")
			return err
		},
		"Copy/destination": func(f *traversalFixture, key string) error {
			// Give the copy a real source so the only thing under test is
			// the destination — otherwise it fails before reaching it.
			_, err := f.drv.Put(ctx, "tenant-a", "seed.txt", strings.NewReader("seed"))
			require.NoError(t, err)
			_, err = f.drv.Copy(ctx, "tenant-a", "seed.txt", "tenant-a", key)
			return err
		},
	}

	for opName, op := range ops {
		for _, tc := range traversalKeys {
			t.Run(opName+"/"+tc.name, func(t *testing.T) {
				f := newTraversalFixture(t)

				err := op(f, tc.key)

				require.Error(t, err, "%s accepted traversal key %q", opName, tc.key)
				// Classified, so the HTTP extension answers 400 rather than
				// falling through to an opaque 500.
				require.ErrorIs(t, err, trove.ErrInvalidPath,
					"%s rejected %q without classifying it", opName, tc.key)
				f.assertIntact(t)
			})
		}
	}
}

func TestTraversal_BucketOperations(t *testing.T) {
	ctx := context.Background()

	// Every operation that takes a bucket name and no key. Delete's key is
	// held constant and benign so the bucket name is the only variable.
	ops := map[string]func(f *traversalFixture, bucket string) error{
		"List": func(f *traversalFixture, bucket string) error {
			it, err := f.drv.List(ctx, bucket)
			if err == nil {
				it.Close()
			}
			return err
		},
		"CreateBucket": func(f *traversalFixture, bucket string) error {
			return f.drv.CreateBucket(ctx, bucket)
		},
		"DeleteBucket": func(f *traversalFixture, bucket string) error {
			return f.drv.DeleteBucket(ctx, bucket)
		},
		"Put": func(f *traversalFixture, bucket string) error {
			_, err := f.drv.Put(ctx, bucket, "obj.txt", strings.NewReader("pwned"))
			return err
		},
		"Get": func(f *traversalFixture, bucket string) error {
			r, err := f.drv.Get(ctx, bucket, "obj.txt")
			if err == nil {
				_ = r.Close()
			}
			return err
		},
		"Head": func(f *traversalFixture, bucket string) error {
			_, err := f.drv.Head(ctx, bucket, "obj.txt")
			return err
		},
		"Delete": func(f *traversalFixture, bucket string) error {
			return f.drv.Delete(ctx, bucket, "obj.txt")
		},
		"Copy/source": func(f *traversalFixture, bucket string) error {
			_, err := f.drv.Copy(ctx, bucket, "obj.txt", "tenant-a", "stolen.txt")
			return err
		},
		"Copy/destination": func(f *traversalFixture, bucket string) error {
			_, err := f.drv.Put(ctx, "tenant-a", "seed.txt", strings.NewReader("seed"))
			require.NoError(t, err)
			_, err = f.drv.Copy(ctx, "tenant-a", "seed.txt", bucket, "obj.txt")
			return err
		},
	}

	for opName, op := range ops {
		for _, tc := range traversalBuckets {
			t.Run(opName+"/"+tc.name, func(t *testing.T) {
				f := newTraversalFixture(t)

				err := op(f, tc.bucket)

				require.Error(t, err, "%s accepted traversal bucket %q", opName, tc.bucket)
				require.ErrorIs(t, err, trove.ErrInvalidPath,
					"%s rejected %q without classifying it", opName, tc.bucket)
				f.assertIntact(t)

				// The root itself must survive: DeleteBucket(".") resolved to
				// the root before the guard covered it.
				_, statErr := os.Stat(f.root)
				require.NoError(t, statErr, "%s removed the storage root", opName)
			})
		}
	}
}

// TestTraversal_LegitimateKeysStillWork is the control. Without it the
// traversal tests above would pass just as well against a guard that rejects
// every key, which would be a denial of service rather than a fix.
func TestTraversal_LegitimateKeysStillWork(t *testing.T) {
	ctx := context.Background()

	keys := []struct {
		name string
		key  string
	}{
		{"flat", "file.txt"},
		{"nested", "a/b/c.txt"},
		{"interior parent that stays inside", "a/../b.txt"},
		{"deep interior parent", "a/b/../c/d.txt"},
		{"dotted name", "..hidden.txt"},
		{"name containing dots", "v1..2/file.txt"},
	}

	for _, tc := range keys {
		t.Run(tc.name, func(t *testing.T) {
			f := newTraversalFixture(t)

			_, err := f.drv.Put(ctx, "tenant-a", tc.key, strings.NewReader("hello"))
			require.NoError(t, err)

			r, err := f.drv.Get(ctx, "tenant-a", tc.key)
			require.NoError(t, err)
			defer r.Close()

			_, err = f.drv.Head(ctx, "tenant-a", tc.key)
			require.NoError(t, err)

			// The object landed inside the bucket, not merely somewhere the
			// guard tolerated.
			resolved := filepath.Join(f.root, "tenant-a", filepath.Clean(tc.key))
			require.FileExists(t, resolved)

			f.assertIntact(t)
		})
	}
}

// TestTraversal_BucketLifecycleStillWorks is the control for bucket names.
func TestTraversal_BucketLifecycleStillWorks(t *testing.T) {
	ctx := context.Background()
	f := newTraversalFixture(t)

	require.NoError(t, f.drv.CreateBucket(ctx, "tenant-c"))
	require.DirExists(t, filepath.Join(f.root, "tenant-c"))

	it, err := f.drv.List(ctx, "tenant-c")
	require.NoError(t, err)
	it.Close()

	require.NoError(t, f.drv.DeleteBucket(ctx, "tenant-c"))
	require.NoDirExists(t, filepath.Join(f.root, "tenant-c"))

	f.assertIntact(t)
}
