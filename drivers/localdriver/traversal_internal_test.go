package localdriver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Put routed its path through safeJoin, but Get, Delete and Stat used
// filepath.Join directly. filepath.Join calls Clean, which *resolves* ".."
// rather than rejecting it, so a crafted bucket or key escaped the root and
// reached arbitrary files. These pin the boundary on every entry point.
func TestReadOperationsRejectTraversal(t *testing.T) {
	root := t.TempDir()

	// A file outside the driver's root that must stay unreachable.
	outside := filepath.Join(filepath.Dir(root), "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("must not be readable"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	defer func() { _ = os.Remove(outside) }()

	d := &LocalDriver{}
	if err := d.Open(context.Background(), "file://"+root); err != nil {
		t.Fatalf("open: %v", err)
	}

	escapes := []struct{ bucket, key string }{
		{"..", "outside-secret.txt"},
		{"bucket", "../../outside-secret.txt"},
		{"../..", "outside-secret.txt"},
		{"bucket", "../../../etc/passwd"},
	}

	for _, e := range escapes {
		t.Run(e.bucket+"|"+e.key, func(t *testing.T) {
			if _, err := d.Get(context.Background(), e.bucket, e.key); err == nil {
				t.Error("Get accepted a path escaping the root")
			} else if !strings.Contains(err.Error(), "escapes root") &&
				!strings.Contains(err.Error(), "invalid path segment") &&
				!os.IsNotExist(err) {
				t.Logf("Get rejected with: %v", err)
			}

			if err := d.Delete(context.Background(), e.bucket, e.key); err == nil {
				t.Error("Delete accepted a path escaping the root")
			}
		})
	}

	// DeleteBucket is the worst case: a caller-supplied name reached
	// os.RemoveAll, so an escaping name meant arbitrary recursive deletion.
	for _, bad := range []string{"..", "../..", "../../tmp"} {
		if err := d.DeleteBucket(context.Background(), bad); err == nil {
			t.Errorf("DeleteBucket accepted %q, which escapes the root", bad)
		}
	}

	// The seeded file must still exist: nothing above may have deleted it.
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("a traversing Delete removed a file outside the root: %v", err)
	}
}

// The guard must not reject legitimate keys, or it trades a vulnerability for
// a broken driver.
func TestOrdinaryKeysStillWork(t *testing.T) {
	root := t.TempDir()
	d := &LocalDriver{}
	if err := d.Open(context.Background(), "file://"+root); err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err := d.Put(context.Background(), "bucket", "nested/dir/object.txt",
		strings.NewReader("hello")); err != nil {
		t.Fatalf("Put of an ordinary nested key failed: %v", err)
	}
	if _, err := d.Head(context.Background(), "bucket", "nested/dir/object.txt"); err != nil {
		t.Fatalf("Stat of an ordinary nested key failed: %v", err)
	}
	r, err := d.Get(context.Background(), "bucket", "nested/dir/object.txt")
	if err != nil {
		t.Fatalf("Get of an ordinary nested key failed: %v", err)
	}
	_ = r.Close()
}
