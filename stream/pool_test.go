package stream_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/stream"
)

func TestPool_AcquireRelease(t *testing.T) {
	pool := stream.NewPool("test", stream.DefaultPoolConfig())

	s, err := pool.Acquire(context.Background(), stream.DirectionUpload, "bucket", "key")
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, stream.StateIdle, s.State())
	assert.Equal(t, 1, pool.ActiveCount())

	pool.Release(s)
	assert.Equal(t, 0, pool.ActiveCount())
}

func TestPool_ConcurrencyLimit(t *testing.T) {
	cfg := stream.PoolConfig{
		MaxStreams: 2,
		ChunkSize:  1024,
	}
	pool := stream.NewPool("test", cfg)

	// Acquire 2 streams (fills the pool).
	s1, err := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k1")
	require.NoError(t, err)
	s2, err := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k2")
	require.NoError(t, err)

	// Third acquire should block; use a short timeout context.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = pool.Acquire(ctx, stream.DirectionUpload, "b", "k3")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Release one → now the third should succeed.
	pool.Release(s1)
	s3, err := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k3")
	require.NoError(t, err)
	assert.Equal(t, 2, pool.ActiveCount())

	pool.Release(s2)
	pool.Release(s3)
}

func TestPool_Get(t *testing.T) {
	pool := stream.NewPool("test", stream.DefaultPoolConfig())

	s, err := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k")
	require.NoError(t, err)

	found := pool.Get(s.ID)
	assert.NotNil(t, found)
	assert.Equal(t, s.ID.String(), found.ID.String())

	pool.Release(s)
	assert.Nil(t, pool.Get(s.ID))
}

func TestPool_Range(t *testing.T) {
	pool := stream.NewPool("test", stream.DefaultPoolConfig())

	s1, _ := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k1")
	s2, _ := pool.Acquire(context.Background(), stream.DirectionDownload, "b", "k2")

	var ids []string
	pool.Range(func(s *stream.Stream) bool {
		ids = append(ids, s.ID.String())
		return true
	})
	assert.Len(t, ids, 2)

	pool.Release(s1)
	pool.Release(s2)
}

func TestPool_MetricsAccumulate(t *testing.T) {
	pool := stream.NewPool("test", stream.PoolConfig{
		MaxStreams: 4,
		ChunkSize:  256,
	})

	s, err := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k")
	require.NoError(t, err)
	require.NoError(t, s.Start())

	// Write some data.
	buf := s.ChunkPool().Get()
	copy(buf, []byte("hello"))
	_ = s.Write(&stream.Chunk{Index: 0, Data: buf, Size: 5})

	require.NoError(t, s.Complete(&driver.ObjectInfo{Key: "k", Size: 5}))
	pool.Release(s)

	snap := pool.Metrics.Snapshot()
	assert.Equal(t, int64(5), snap.TotalBytes)
	assert.Equal(t, int64(1), snap.TotalChunks)
	assert.Equal(t, int64(0), snap.FailedStreams)
}

func TestPool_FailedStreamMetrics(t *testing.T) {
	pool := stream.NewPool("test", stream.DefaultPoolConfig())

	s, _ := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k")
	require.NoError(t, s.Start())
	s.Fail(assert.AnError)

	pool.Release(s)

	snap := pool.Metrics.Snapshot()
	assert.Equal(t, int64(1), snap.FailedStreams)
}

func TestPool_ConcurrentAcquireRelease(t *testing.T) {
	pool := stream.NewPool("test", stream.PoolConfig{
		MaxStreams: 8,
		ChunkSize:  256,
	})

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			s, err := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k")
			if err != nil {
				return
			}
			_ = s.Start()
			_ = s.Complete(&driver.ObjectInfo{Key: "k"})
			pool.Release(s)
		}()
	}

	wg.Wait()
	assert.Equal(t, 0, pool.ActiveCount())

	snap := pool.Metrics.Snapshot()
	assert.Equal(t, int64(0), snap.ActiveStreams)
}

func TestPool_Close(t *testing.T) {
	pool := stream.NewPool("test", stream.DefaultPoolConfig())

	s, _ := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k")

	// Release the stream first so Close doesn't hang.
	pool.Release(s)

	err := pool.Close()
	require.NoError(t, err)

	// After close, acquire should fail.
	_, err = pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k2")
	assert.Error(t, err)
}

func TestPool_CloseWithActiveStreams(t *testing.T) {
	pool := stream.NewPool("test", stream.PoolConfig{
		MaxStreams: 4,
		ChunkSize:  256,
	})

	s, _ := pool.Acquire(context.Background(), stream.DirectionUpload, "b", "k")

	// Close should cancel active streams. Release in a goroutine to unblock wg.Wait.
	go func() {
		time.Sleep(10 * time.Millisecond)
		pool.Release(s)
	}()

	err := pool.Close()
	require.NoError(t, err)
	assert.True(t, s.State().IsTerminal())
}

func TestPool_DefaultConfig(t *testing.T) {
	cfg := stream.DefaultPoolConfig()
	assert.Equal(t, 16, cfg.MaxStreams)
	assert.Equal(t, int64(8*1024*1024), cfg.ChunkSize)
	assert.Equal(t, stream.BackpressureBlock, cfg.BackpressureMode)
}

func TestPool_Identity(t *testing.T) {
	pool := stream.NewPool("uploads", stream.DefaultPoolConfig())
	assert.Equal(t, "uploads", pool.Name)
	assert.False(t, pool.ID.IsNil())
}
