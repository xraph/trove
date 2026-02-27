package cas

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/drivers/memdriver"
)

func newTestCAS(t *testing.T) *CAS {
	t.Helper()
	drv := memdriver.New()
	require.NoError(t, drv.Open(context.Background(), ""))
	require.NoError(t, drv.CreateBucket(context.Background(), "cas"))

	c := New(drv)
	return c
}

func TestCAS_StoreAndRetrieve(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	data := []byte("hello, CAS!")
	hash, info, err := c.Store(ctx, bytes.NewReader(data))
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotNil(t, info)

	// Retrieve.
	reader, err := c.Retrieve(ctx, hash)
	require.NoError(t, err)
	defer reader.Close()

	retrieved, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

func TestCAS_Deduplication(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	data := []byte("duplicate content")

	hash1, _, err := c.Store(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	hash2, _, err := c.Store(ctx, bytes.NewReader(data))
	require.NoError(t, err)

	// Same data produces same hash.
	assert.Equal(t, hash1, hash2)

	// Ref count should be 2.
	entry, err := c.index.Get(ctx, hash1)
	require.NoError(t, err)
	assert.Equal(t, 2, entry.RefCount)
}

func TestCAS_DifferentContent(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	hash1, _, err := c.Store(ctx, bytes.NewReader([]byte("content A")))
	require.NoError(t, err)

	hash2, _, err := c.Store(ctx, bytes.NewReader([]byte("content B")))
	require.NoError(t, err)

	assert.NotEqual(t, hash1, hash2)
}

func TestCAS_Exists(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	exists, err := c.Exists(ctx, "sha256:nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)

	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("test")))
	require.NoError(t, err)

	exists, err = c.Exists(ctx, hash)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCAS_PinUnpin(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("pinnable")))
	require.NoError(t, err)

	require.NoError(t, c.Pin(ctx, hash))

	entry, err := c.index.Get(ctx, hash)
	require.NoError(t, err)
	assert.True(t, entry.Pinned)

	require.NoError(t, c.Unpin(ctx, hash))

	entry, err = c.index.Get(ctx, hash)
	require.NoError(t, err)
	assert.False(t, entry.Pinned)
}

func TestCAS_GC_RemovesUnreferenced(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("garbage")))
	require.NoError(t, err)

	// Decrement ref to 0 — eligible for GC.
	require.NoError(t, c.index.DecrementRef(ctx, hash))

	entry, err := c.index.Get(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, 0, entry.RefCount)

	// Run GC.
	result, err := c.GC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Scanned)
	assert.Equal(t, 1, result.Deleted)
	assert.Greater(t, result.FreedBytes, int64(0))
	assert.Equal(t, 0, result.Errors)

	// Should no longer exist.
	exists, err := c.Exists(ctx, hash)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCAS_GC_SkipsPinned(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("pinned data")))
	require.NoError(t, err)

	// Decrement ref to 0 but pin it.
	require.NoError(t, c.index.DecrementRef(ctx, hash))
	require.NoError(t, c.Pin(ctx, hash))

	result, err := c.GC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Scanned) // pinned entries aren't listed
	assert.Equal(t, 0, result.Deleted)

	// Should still exist.
	exists, err := c.Exists(ctx, hash)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCAS_GC_SkipsReferenced(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("referenced")))
	require.NoError(t, err)

	// Ref count is 1 — not eligible for GC.
	result, err := c.GC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Scanned)
	assert.Equal(t, 0, result.Deleted)

	// Still retrievable.
	reader, err := c.Retrieve(ctx, hash)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, []byte("referenced"), data)
}

func TestCAS_RetrieveNotFound(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	_, err := c.Retrieve(ctx, "sha256:nonexistent")
	assert.Error(t, err)
}

func TestCAS_Algorithm(t *testing.T) {
	drv := memdriver.New()
	require.NoError(t, drv.Open(context.Background(), ""))
	require.NoError(t, drv.CreateBucket(context.Background(), "cas"))

	c := New(drv, WithAlgorithm(AlgBlake3))
	assert.Equal(t, AlgBlake3, c.Algorithm())

	ctx := context.Background()
	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("blake3 test")))
	require.NoError(t, err)
	assert.Contains(t, hash, "blake3:")
}

func TestCAS_CustomBucket(t *testing.T) {
	drv := memdriver.New()
	require.NoError(t, drv.Open(context.Background(), ""))
	require.NoError(t, drv.CreateBucket(context.Background(), "custom-cas"))

	c := New(drv, WithBucket("custom-cas"))
	ctx := context.Background()

	hash, _, err := c.Store(ctx, bytes.NewReader([]byte("custom bucket")))
	require.NoError(t, err)

	reader, err := c.Retrieve(ctx, hash)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, []byte("custom bucket"), data)
}

func TestCAS_EmptyContent(t *testing.T) {
	c := newTestCAS(t)
	ctx := context.Background()

	hash, _, err := c.Store(ctx, bytes.NewReader(nil))
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	reader, err := c.Retrieve(ctx, hash)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Empty(t, data)
}

func TestMemoryIndex_Operations(t *testing.T) {
	idx := NewMemoryIndex()
	ctx := context.Background()

	// Get nonexistent.
	_, err := idx.Get(ctx, "hash1")
	assert.ErrorIs(t, err, ErrNotFound)

	// Put.
	require.NoError(t, idx.Put(ctx, &Entry{
		Hash:   "hash1",
		Bucket: "cas",
		Key:    "hash1",
		Size:   100,
	}))

	entry, err := idx.Get(ctx, "hash1")
	require.NoError(t, err)
	assert.Equal(t, 1, entry.RefCount)
	assert.Equal(t, int64(100), entry.Size)

	// Increment ref.
	require.NoError(t, idx.IncrementRef(ctx, "hash1"))
	entry, err = idx.Get(ctx, "hash1")
	require.NoError(t, err)
	assert.Equal(t, 2, entry.RefCount)

	// Decrement ref.
	require.NoError(t, idx.DecrementRef(ctx, "hash1"))
	entry, err = idx.Get(ctx, "hash1")
	require.NoError(t, err)
	assert.Equal(t, 1, entry.RefCount)

	// Pin / Unpin.
	require.NoError(t, idx.Pin(ctx, "hash1"))
	entry, err = idx.Get(ctx, "hash1")
	require.NoError(t, err)
	assert.True(t, entry.Pinned)

	require.NoError(t, idx.Unpin(ctx, "hash1"))
	entry, err = idx.Get(ctx, "hash1")
	require.NoError(t, err)
	assert.False(t, entry.Pinned)

	// Delete.
	require.NoError(t, idx.Delete(ctx, "hash1"))
	_, err = idx.Get(ctx, "hash1")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryIndex_ListUnpinned(t *testing.T) {
	idx := NewMemoryIndex()
	ctx := context.Background()

	// Add entries with varying states (Put sets RefCount=1 when 0).
	require.NoError(t, idx.Put(ctx, &Entry{Hash: "a", RefCount: 1}))
	require.NoError(t, idx.Put(ctx, &Entry{Hash: "b", RefCount: 1}))
	require.NoError(t, idx.Put(ctx, &Entry{Hash: "c", RefCount: 1}))
	require.NoError(t, idx.Put(ctx, &Entry{Hash: "d", RefCount: 1}))

	// Decrement all to 0 except "a" stays at 1 initially.
	require.NoError(t, idx.DecrementRef(ctx, "a"))
	require.NoError(t, idx.DecrementRef(ctx, "b"))
	require.NoError(t, idx.DecrementRef(ctx, "c"))
	require.NoError(t, idx.DecrementRef(ctx, "d"))

	// Pin "c".
	require.NoError(t, idx.Pin(ctx, "c"))

	unpinned, err := idx.ListUnpinned(ctx)
	require.NoError(t, err)

	hashes := make(map[string]bool)
	for _, e := range unpinned {
		hashes[e.Hash] = true
	}

	assert.True(t, hashes["a"], "a should be unpinned (ref=0)")
	assert.True(t, hashes["b"], "b should be unpinned (ref=0)")
	assert.False(t, hashes["c"], "c is pinned")
	assert.True(t, hashes["d"], "d should be unpinned (ref=0)")
}
