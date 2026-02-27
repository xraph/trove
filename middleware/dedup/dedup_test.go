package dedup

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/middleware"
)

func TestDedup_InterfaceCompliance(t *testing.T) {
	d := New()
	var _ middleware.Middleware = d
	var _ middleware.WriteMiddleware = d
	assert.Equal(t, "dedup", d.Name())
	assert.Equal(t, middleware.DirectionWrite, d.Direction())
}

func TestDedup_FirstWrite(t *testing.T) {
	d := New()
	ctx := context.Background()

	data := []byte("unique content")
	var output bytes.Buffer
	w, err := d.WrapWriter(ctx, &nopWriteCloser{&output}, "file1.txt")
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Data should be written through.
	assert.Equal(t, data, output.Bytes())
}

func TestDedup_DuplicateDetection(t *testing.T) {
	var dupCount atomic.Int32
	d := New(WithOnDuplicate(func(_ context.Context, _, _ string) {
		dupCount.Add(1)
	}))
	ctx := context.Background()

	data := []byte("duplicate content")

	// First write.
	var out1 bytes.Buffer
	w, err := d.WrapWriter(ctx, &nopWriteCloser{&out1}, "file1.txt")
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	assert.Equal(t, int32(0), dupCount.Load())

	// Second write with same content — should trigger callback.
	var out2 bytes.Buffer
	w, err = d.WrapWriter(ctx, &nopWriteCloser{&out2}, "file2.txt")
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	assert.Equal(t, int32(1), dupCount.Load())

	// Data still written through (dedup middleware reports, doesn't block).
	assert.Equal(t, data, out2.Bytes())
}

func TestDedup_DifferentContent(t *testing.T) {
	var dupCount atomic.Int32
	d := New(WithOnDuplicate(func(_ context.Context, _, _ string) {
		dupCount.Add(1)
	}))
	ctx := context.Background()

	// Write two different pieces of content.
	for i, content := range []string{"content A", "content B"} {
		var output bytes.Buffer
		w, err := d.WrapWriter(ctx, &nopWriteCloser{&output}, "file"+string(rune('1'+i))+".txt")
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}

	assert.Equal(t, int32(0), dupCount.Load())
}

func TestDedup_MemoryStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Initially empty.
	exists, err := store.Exists(ctx, "hash1")
	require.NoError(t, err)
	assert.False(t, exists)

	// Record a hash.
	require.NoError(t, store.Record(ctx, "hash1", "key1"))

	// Now exists.
	exists, err = store.Exists(ctx, "hash1")
	require.NoError(t, err)
	assert.True(t, exists)

	// Different hash still not found.
	exists, err = store.Exists(ctx, "hash2")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestDedup_EmptyContent(t *testing.T) {
	d := New()
	ctx := context.Background()

	var output bytes.Buffer
	w, err := d.WrapWriter(ctx, &nopWriteCloser{&output}, "empty.txt")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Empty(t, output.Bytes())
}

func TestDedup_CustomStore(t *testing.T) {
	store := NewMemoryStore()
	d := New(WithStore(store))
	ctx := context.Background()

	data := []byte("tracked content")
	var output bytes.Buffer
	w, err := d.WrapWriter(ctx, &nopWriteCloser{&output}, "tracked.txt")
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Verify the custom store has the hash recorded.
	store.mu.RLock()
	assert.Len(t, store.hashes, 1)
	store.mu.RUnlock()
}

// nopWriteCloser wraps a writer as an io.WriteCloser.
type nopWriteCloser struct {
	buf io.Writer
}

func (w *nopWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *nopWriteCloser) Close() error                { return nil }
