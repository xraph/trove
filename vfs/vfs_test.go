package vfs_test

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/vfs"
)

func setup(t *testing.T) (*vfs.FS, *trove.Trove) {
	t.Helper()
	drv := memdriver.New()
	require.NoError(t, drv.Open(context.Background(), ""))

	store, err := trove.Open(drv)
	require.NoError(t, err)
	require.NoError(t, store.CreateBucket(context.Background(), "test"))

	return vfs.New(store, "test"), store
}

func putFile(t *testing.T, store *trove.Trove, key, content string) {
	t.Helper()
	ctx := context.Background()
	_, err := store.Put(ctx, "test", key, strings.NewReader(content))
	require.NoError(t, err)
}

func TestVFS_CreateAndOpen(t *testing.T) {
	v, _ := setup(t)
	ctx := context.Background()

	// Create a file.
	f, err := v.Create(ctx, "hello.txt")
	require.NoError(t, err)
	_, err = f.Write([]byte("hello, VFS!"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Open and read.
	f, err = v.Open(ctx, "hello.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	assert.Equal(t, "hello, VFS!", string(data))
}

func TestVFS_Stat_File(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "doc.txt", "some content")

	info, err := v.Stat(ctx, "doc.txt")
	require.NoError(t, err)
	assert.Equal(t, "doc.txt", info.Name())
	assert.False(t, info.IsDir())
	assert.Equal(t, int64(12), info.Size())
}

func TestVFS_Stat_Directory(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "docs/readme.md", "# readme")

	info, err := v.Stat(ctx, "docs")
	require.NoError(t, err)
	assert.Equal(t, "docs", info.Name())
	assert.True(t, info.IsDir())
}

func TestVFS_Stat_NotFound(t *testing.T) {
	v, _ := setup(t)
	ctx := context.Background()

	_, err := v.Stat(ctx, "nonexistent")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestVFS_ReadDir(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()

	putFile(t, store, "a.txt", "a")
	putFile(t, store, "b.txt", "b")
	putFile(t, store, "sub/c.txt", "c")

	entries, err := v.ReadDir(ctx, "")
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = e.IsDir()
	}

	assert.False(t, names["a.txt"], "a.txt should be a file")
	assert.False(t, names["b.txt"], "b.txt should be a file")
	assert.True(t, names["sub"], "sub should be a directory")
}

func TestVFS_ReadDir_Subdirectory(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()

	putFile(t, store, "docs/a.md", "a")
	putFile(t, store, "docs/b.md", "b")

	entries, err := v.ReadDir(ctx, "docs")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestVFS_Walk(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()

	putFile(t, store, "a.txt", "a")
	putFile(t, store, "dir/b.txt", "b")
	putFile(t, store, "dir/sub/c.txt", "c")

	var paths []string
	err := v.Walk(ctx, "", func(path string, _ *vfs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	require.NoError(t, err)

	assert.Contains(t, paths, "") // root
	assert.Contains(t, paths, "a.txt")
	assert.Contains(t, paths, "dir")
	assert.Contains(t, paths, "dir/b.txt")
	assert.Contains(t, paths, "dir/sub")
	assert.Contains(t, paths, "dir/sub/c.txt")
}

func TestVFS_Walk_SkipDir(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()

	putFile(t, store, "a.txt", "a")
	putFile(t, store, "skip/b.txt", "b")
	putFile(t, store, "keep/c.txt", "c")

	var paths []string
	err := v.Walk(ctx, "", func(path string, info *vfs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() && info.Name() == "skip" {
			return fs.SkipDir
		}
		paths = append(paths, path)
		return nil
	})
	require.NoError(t, err)

	assert.NotContains(t, paths, "skip/b.txt")
	assert.Contains(t, paths, "keep/c.txt")
}

func TestVFS_Remove(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "delete-me.txt", "gone")

	require.NoError(t, v.Remove(ctx, "delete-me.txt"))

	_, err := v.Stat(ctx, "delete-me.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestVFS_RemoveAll(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "dir/a.txt", "a")
	putFile(t, store, "dir/b.txt", "b")
	putFile(t, store, "dir/sub/c.txt", "c")

	require.NoError(t, v.RemoveAll(ctx, "dir"))

	_, err := v.Stat(ctx, "dir")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestVFS_Rename(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "old.txt", "content")

	require.NoError(t, v.Rename(ctx, "old.txt", "new.txt"))

	// Old should be gone.
	_, err := v.Stat(ctx, "old.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	// New should exist with same content.
	f, err := v.Open(ctx, "new.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	f.Close()
	assert.Equal(t, "content", string(data))
}

func TestVFS_Mkdir(t *testing.T) {
	v, _ := setup(t)
	ctx := context.Background()

	require.NoError(t, v.Mkdir(ctx, "newdir"))

	info, err := v.Stat(ctx, "newdir")
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestVFS_Metadata(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "meta.txt", "data")

	require.NoError(t, v.SetMetadata(ctx, "meta.txt", map[string]string{"author": "test"}))

	meta, err := v.GetMetadata(ctx, "meta.txt")
	require.NoError(t, err)
	assert.Equal(t, "test", meta["author"])
}

func TestVFS_IOFS(t *testing.T) {
	v, store := setup(t)
	ctx := context.Background()
	putFile(t, store, "hello.txt", "world")
	putFile(t, store, "dir/nested.txt", "nested")

	iofs := vfs.NewIOFS(ctx, v)

	// Open a file.
	f, err := iofs.Open("hello.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	f.Close()
	assert.Equal(t, "world", string(data))

	// Open root directory.
	dir, err := iofs.Open(".")
	require.NoError(t, err)

	rdf, ok := dir.(fs.ReadDirFile)
	require.True(t, ok)

	entries, err := rdf.ReadDir(-1)
	require.NoError(t, err)
	dir.Close()

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	assert.Contains(t, names, "hello.txt")
	assert.Contains(t, names, "dir")
}

func TestVFS_IOFS_InvalidPath(t *testing.T) {
	v, _ := setup(t)
	ctx := context.Background()

	iofs := vfs.NewIOFS(ctx, v)

	_, err := iofs.Open("../invalid")
	assert.Error(t, err)
}
