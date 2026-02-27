package stream_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/stream"
)

func TestChunkPool_GetPut(t *testing.T) {
	const chunkSize int64 = 1024
	pool := stream.NewChunkPool(chunkSize)

	buf := pool.Get()
	require.NotNil(t, buf)
	assert.Equal(t, int(chunkSize), len(buf))
	assert.Equal(t, int(chunkSize), cap(buf))

	// Return the buffer.
	pool.Put(buf)

	stats := pool.Stats()
	assert.Equal(t, chunkSize, stats.ChunkSize)
	assert.GreaterOrEqual(t, stats.Allocated, int64(1))
	assert.GreaterOrEqual(t, stats.Released, int64(1))
}

func TestChunkPool_Reuse(t *testing.T) {
	pool := stream.NewChunkPool(512)

	// Get and return a buffer, then get again — should reuse.
	buf1 := pool.Get()
	pool.Put(buf1)

	buf2 := pool.Get()
	require.NotNil(t, buf2)
	assert.Equal(t, 512, len(buf2))
}

func TestChunkPool_IgnoresWrongSize(t *testing.T) {
	pool := stream.NewChunkPool(1024)

	// A buffer with a different capacity should not be returned to the pool.
	wrongBuf := make([]byte, 256)
	pool.Put(wrongBuf)

	stats := pool.Stats()
	assert.Equal(t, int64(0), stats.Released, "wrong-size buffer should not increment released counter")
}

func TestChunkPool_ConcurrentAccess(t *testing.T) {
	pool := stream.NewChunkPool(4096)
	const goroutines = 50

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				buf := pool.Get()
				buf[0] = byte(j)
				pool.Put(buf)
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	stats := pool.Stats()
	assert.GreaterOrEqual(t, stats.Allocated, int64(1))
}
