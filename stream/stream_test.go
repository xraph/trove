package stream_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/stream"
)

func newTestStream(t *testing.T, opts ...stream.Option) (*stream.Stream, *stream.ChunkPool) {
	t.Helper()
	pool := stream.NewChunkPool(1024)
	s := stream.NewStream(context.Background(), stream.DirectionUpload, "test-bucket", "test-key", pool, opts...)
	t.Cleanup(func() { s.Close() })
	return s, pool
}

func TestStream_InitialState(t *testing.T) {
	s, _ := newTestStream(t)

	assert.Equal(t, stream.StateIdle, s.State())
	assert.False(t, s.State().IsTerminal())
	assert.Nil(t, s.Err())
	assert.Equal(t, "test-bucket", s.Bucket)
	assert.Equal(t, "test-key", s.Key)
	assert.Equal(t, stream.DirectionUpload, s.Direction)
	assert.False(t, s.ID.IsNil())
}

func TestStream_StateTransitions(t *testing.T) {
	s, _ := newTestStream(t)

	// idle → active
	require.NoError(t, s.Start())
	assert.Equal(t, stream.StateActive, s.State())

	// active → paused
	require.NoError(t, s.Control(stream.ControlMsg{Type: stream.CtrlPause}))
	assert.Equal(t, stream.StatePaused, s.State())

	// paused → active
	require.NoError(t, s.Control(stream.ControlMsg{Type: stream.CtrlResume}))
	assert.Equal(t, stream.StateActive, s.State())

	// active → completing → completed
	require.NoError(t, s.Complete(&driver.ObjectInfo{Key: "test-key", Size: 100}))
	assert.Equal(t, stream.StateCompleted, s.State())
	assert.True(t, s.State().IsTerminal())
}

func TestStream_CannotStartFromTerminal(t *testing.T) {
	s, _ := newTestStream(t)
	require.NoError(t, s.Start())
	require.NoError(t, s.Complete(&driver.ObjectInfo{Key: "test-key"}))

	err := s.Start()
	assert.Error(t, err)
}

func TestStream_Cancel(t *testing.T) {
	s, _ := newTestStream(t)
	require.NoError(t, s.Start())

	require.NoError(t, s.Control(stream.ControlMsg{Type: stream.CtrlCancel}))
	assert.Equal(t, stream.StateCancelled, s.State())
	assert.True(t, s.State().IsTerminal())
}

func TestStream_Fail(t *testing.T) {
	s, _ := newTestStream(t)
	require.NoError(t, s.Start())

	testErr := assert.AnError
	s.Fail(testErr)

	assert.Equal(t, stream.StateFailed, s.State())
	assert.ErrorIs(t, s.Err(), testErr)
}

func TestStream_WriteRead(t *testing.T) {
	s, pool := newTestStream(t, stream.WithChannelSize(4))
	require.NoError(t, s.Start())

	// Write a chunk.
	buf := pool.Get()
	copy(buf, []byte("hello"))
	err := s.Write(&stream.Chunk{
		Index: 0,
		Data:  buf,
		Size:  5,
	})
	require.NoError(t, err)

	// Read the chunk back.
	chunk, err := s.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, chunk.Index)
	assert.Equal(t, 5, chunk.Size)
	assert.Equal(t, "hello", string(chunk.Data[:chunk.Size]))
}

func TestStream_WriteToNonActive(t *testing.T) {
	s, pool := newTestStream(t)

	buf := pool.Get()
	err := s.Write(&stream.Chunk{Data: buf, Size: 1})
	assert.Error(t, err, "write to idle stream should fail")
}

func TestStream_WriteToPaused(t *testing.T) {
	s, pool := newTestStream(t)
	require.NoError(t, s.Start())
	require.NoError(t, s.Control(stream.ControlMsg{Type: stream.CtrlPause}))

	buf := pool.Get()
	err := s.Write(&stream.Chunk{Data: buf, Size: 1})
	assert.Error(t, err, "write to paused stream should fail")
}

func TestStream_Offset(t *testing.T) {
	s, pool := newTestStream(t)
	require.NoError(t, s.Start())

	buf := pool.Get()
	copy(buf, []byte("data"))
	_ = s.Write(&stream.Chunk{Index: 0, Data: buf, Size: 4})

	buf2 := pool.Get()
	copy(buf2, []byte("more"))
	_ = s.Write(&stream.Chunk{Index: 1, Data: buf2, Size: 4})

	assert.Equal(t, int64(8), s.Offset())
}

func TestStream_Seek(t *testing.T) {
	s, _ := newTestStream(t)
	require.NoError(t, s.Start())

	require.NoError(t, s.Control(stream.ControlMsg{Type: stream.CtrlSeek, Offset: 1024}))
	assert.Equal(t, int64(1024), s.Offset())
}

func TestStream_ProgressHook(t *testing.T) {
	var lastProgress stream.Progress
	s, pool := newTestStream(t,
		stream.WithChannelSize(4),
		stream.WithOnProgress(func(p stream.Progress) {
			lastProgress = p
		}),
	)
	s.SetTotalSize(100)
	require.NoError(t, s.Start())

	buf := pool.Get()
	copy(buf, make([]byte, 50))
	_ = s.Write(&stream.Chunk{Index: 0, Data: buf, Size: 50})

	assert.Equal(t, int64(50), lastProgress.BytesSent)
	assert.Equal(t, int64(100), lastProgress.TotalSize)
	assert.Equal(t, 50, lastProgress.Percent)
}

func TestStream_CompleteHook(t *testing.T) {
	var called atomic.Bool
	s, _ := newTestStream(t, stream.WithOnComplete(func(_ *driver.ObjectInfo) {
		called.Store(true)
	}))
	require.NoError(t, s.Start())
	require.NoError(t, s.Complete(&driver.ObjectInfo{Key: "k"}))

	assert.True(t, called.Load())
}

func TestStream_ErrorHook(t *testing.T) {
	var hookErr error
	s, _ := newTestStream(t, stream.WithOnError(func(err error) {
		hookErr = err
	}))
	require.NoError(t, s.Start())

	s.Fail(assert.AnError)
	assert.ErrorIs(t, hookErr, assert.AnError)
}

func TestStream_Resumable(t *testing.T) {
	s, _ := newTestStream(t, stream.WithResumable())
	assert.True(t, s.Resumable())
}

func TestStream_Metrics(t *testing.T) {
	s, pool := newTestStream(t, stream.WithChannelSize(4))
	require.NoError(t, s.Start())

	buf := pool.Get()
	_ = s.Write(&stream.Chunk{Index: 0, Data: buf, Size: 100})

	m := s.Metrics()
	assert.Equal(t, int64(100), m.BytesSent.Load())
	assert.Equal(t, int64(1), m.Chunks.Load())
}

func TestStream_CloseIsIdempotent(t *testing.T) {
	s, _ := newTestStream(t)

	// Close multiple times should not panic.
	s.Close()
	s.Close()
	s.Close()

	assert.True(t, s.State().IsTerminal())
}

func TestDirection_String(t *testing.T) {
	assert.Equal(t, "upload", stream.DirectionUpload.String())
	assert.Equal(t, "download", stream.DirectionDownload.String())
	assert.Equal(t, "bidi", stream.DirectionBiDi.String())
}

func TestStreamState_IsTerminal(t *testing.T) {
	assert.False(t, stream.StateIdle.IsTerminal())
	assert.False(t, stream.StateActive.IsTerminal())
	assert.False(t, stream.StatePaused.IsTerminal())
	assert.False(t, stream.StateCompleting.IsTerminal())
	assert.True(t, stream.StateCompleted.IsTerminal())
	assert.True(t, stream.StateFailed.IsTerminal())
	assert.True(t, stream.StateCancelled.IsTerminal())
}
