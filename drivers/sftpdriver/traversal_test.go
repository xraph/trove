package sftpdriver

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// The SFTP driver resolves buckets and keys against a remote base path, so
// the same containment rule as localdriver applies: a key stays inside its
// bucket, a bucket stays inside the base path.
//
// The driver's operations cannot be exercised without a live server (see
// integration_test.go, behind the `integration` build tag), so these tests
// target safeJoin and objectPath directly — every path the driver builds
// goes through one of them — and a source assertion covers the wiring.

const testBasePath = "/data/storage"

func TestSafeJoin_RejectsTraversal(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
	}{
		// Keys escaping their bucket.
		{"parent", []string{"tenant-a", "../x"}},
		{"escapes the bucket into the base path", []string{"tenant-a", "a/../../x"}},
		{"escapes the base path", []string{"tenant-a", "../../etc/passwd"}},
		{"reaches a sibling bucket", []string{"tenant-a", "../tenant-b/secret.txt"}},
		{"trailing climb", []string{"tenant-a", "a/b/../../../etc/passwd"}},
		{"bare parent", []string{"tenant-a", ".."}},
		{"names the bucket itself", []string{"tenant-a", "."}},
		{"absolute key", []string{"tenant-a", "/etc/passwd"}},
		{"absolute key with climb", []string{"tenant-a", "/../../etc/passwd"}},
		{"backslash rooted key", []string{"tenant-a", `\windows\system32`}},
		{"empty key", []string{"tenant-a", ""}},
		{"nul byte in key", []string{"tenant-a", "a\x00b"}},

		// Buckets escaping the base path.
		{"bucket parent", []string{"../evil"}},
		{"bucket bare parent", []string{".."}},
		{"bucket names the base path", []string{"."}},
		{"bucket deep climb", []string{"a/../../evil"}},
		{"absolute bucket", []string{"/tmp"}},
		{"empty bucket", []string{""}},
		{"nul byte in bucket", []string{"a\x00b"}},

		// Both segments tainted.
		{"tainted bucket and key", []string{"../evil", "../../etc/passwd"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeJoin(testBasePath, tc.parts...)

			require.Error(t, err, "safeJoin accepted %q", tc.parts)
			require.Empty(t, got, "safeJoin returned a path alongside its error")
		})
	}
}

// TestSafeJoin_AllowsLegitimatePaths is the control: a guard that rejected
// everything would satisfy the traversal table above while breaking the
// driver outright.
func TestSafeJoin_AllowsLegitimatePaths(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{"bucket only", []string{"tenant-a"}, "/data/storage/tenant-a"},
		{"flat key", []string{"tenant-a", "file.txt"}, "/data/storage/tenant-a/file.txt"},
		{"nested key", []string{"tenant-a", "a/b/c.txt"}, "/data/storage/tenant-a/a/b/c.txt"},
		{"interior parent that stays inside", []string{"tenant-a", "a/../b.txt"}, "/data/storage/tenant-a/b.txt"},
		{"deep interior parent", []string{"tenant-a", "a/b/../c/d.txt"}, "/data/storage/tenant-a/a/c/d.txt"},
		{"dotted name", []string{"tenant-a", "..hidden.txt"}, "/data/storage/tenant-a/..hidden.txt"},
		{"name containing dots", []string{"tenant-a", "v1..2/file.txt"}, "/data/storage/tenant-a/v1..2/file.txt"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeJoin(testBasePath, tc.parts...)

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestSafeJoin_BasePathVariants covers base paths whose shape could break the
// prefix comparison: a trailing slash would otherwise produce "//", and a root
// base path makes the separator and the base path the same string.
func TestSafeJoin_BasePathVariants(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{"plain", "/data/storage", "/data/storage/tenant-a/f.txt"},
		{"trailing slash", "/data/storage/", "/data/storage/tenant-a/f.txt"},
		{"unclean", "/data//storage/./", "/data/storage/tenant-a/f.txt"},
		{"root", "/", "/tenant-a/f.txt"},
		{"relative", "data", "data/tenant-a/f.txt"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeJoin(tc.base, "tenant-a", "f.txt")
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			// The same base path must still refuse an escape.
			_, err = safeJoin(tc.base, "tenant-a", "../../../etc/passwd")
			require.Error(t, err)
		})
	}
}

func TestObjectPath_RejectsTraversal(t *testing.T) {
	cfg := &sftpConfig{BasePath: testBasePath}
	d := New()

	tests := []struct {
		name   string
		bucket string
		key    string
	}{
		{"traversal key", "tenant-a", "../../etc/passwd"},
		{"cross-bucket key", "tenant-a", "../tenant-b/secret.txt"},
		{"absolute key", "tenant-a", "/etc/passwd"},
		{"traversal bucket", "../evil", "f.txt"},
		{"empty key", "tenant-a", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.objectPath(cfg, tc.bucket, tc.key)

			require.Error(t, err)
			require.Empty(t, got)
		})
	}

	t.Run("legitimate key", func(t *testing.T) {
		got, err := d.objectPath(cfg, "tenant-a", "a/b.txt")
		require.NoError(t, err)
		require.Equal(t, "/data/storage/tenant-a/a/b.txt", got)
	})
}

// TestNoUnguardedBasePathJoins asserts the wiring rather than the guard. The
// driver's operations need a live SFTP server, so nothing else here would
// notice a new operation — or a future edit to an existing one — that built a
// remote path with a bare path.Join instead of safeJoin. That is precisely
// the regression this change exists to fix, and it is cheap to detect at the
// source level.
func TestNoUnguardedBasePathJoins(t *testing.T) {
	src, err := os.ReadFile("sftp.go")
	require.NoError(t, err)

	// safeJoin itself is the one legitimate caller of path.Join on a base
	// path, and it takes its base as a parameter rather than off the config.
	unguarded := regexp.MustCompile(`path\.Join\([^)]*BasePath`)
	matches := unguarded.FindAllString(string(src), -1)

	require.Empty(t, matches,
		"remote path built with a bare path.Join; route it through safeJoin so the bucket and key are contained")
}
