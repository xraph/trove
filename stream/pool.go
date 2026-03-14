package stream

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/xraph/trove/id"
)

// PoolConfig configures a streaming pool.
type PoolConfig struct {
	// MaxStreams is the maximum number of concurrent streams.
	MaxStreams int `json:"max_streams" yaml:"max_streams"`

	// MaxBandwidth limits total throughput in bytes/sec (0 = unlimited).
	MaxBandwidth int64 `json:"max_bandwidth" yaml:"max_bandwidth"`

	// ChunkSize is the default chunk size for streams in this pool.
	ChunkSize int64 `json:"chunk_size" yaml:"chunk_size"`

	// BufferCount is the initial number of buffers in the chunk pool.
	BufferCount int `json:"buffer_count" yaml:"buffer_count"`

	// StreamTimeout is the maximum lifetime of a single stream.
	StreamTimeout time.Duration `json:"stream_timeout" yaml:"stream_timeout"`

	// IdleTimeout closes a stream that has been idle for this duration.
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`

	// BackpressureMode is the default backpressure strategy for streams.
	BackpressureMode Backpressure `json:"backpressure" yaml:"backpressure"`
}

// DefaultPoolConfig returns a PoolConfig with sensible defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxStreams:       16,
		ChunkSize:        8 * 1024 * 1024, // 8MB
		BufferCount:      32,
		StreamTimeout:    30 * time.Minute,
		IdleTimeout:      5 * time.Minute,
		BackpressureMode: BackpressureBlock,
	}
}

// Pool manages a set of concurrent streams with shared resources,
// concurrency limits, and bandwidth throttling.
type Pool struct {
	ID      id.ID
	Name    string
	Config  PoolConfig
	Metrics *PoolMetrics

	chunkPool *ChunkPool
	sem       chan struct{} // concurrency semaphore
	active    sync.Map      // id.ID → *Stream

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewPool creates a new streaming pool.
func NewPool(name string, cfg PoolConfig) *Pool {
	if cfg.MaxStreams <= 0 {
		cfg.MaxStreams = DefaultPoolConfig().MaxStreams
	}
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = DefaultPoolConfig().ChunkSize
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is stored in Pool.cancel and called in Pool.Close()

	return &Pool{
		ID:        id.NewPoolID(),
		Name:      name,
		Config:    cfg,
		Metrics:   &PoolMetrics{},
		chunkPool: NewChunkPool(cfg.ChunkSize),
		sem:       make(chan struct{}, cfg.MaxStreams),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Acquire reserves a concurrency slot and creates a new stream within
// the pool. Blocks until a slot is available or the context is cancelled.
func (p *Pool) Acquire(ctx context.Context, dir Direction, bucket, key string, opts ...Option) (*Stream, error) {
	// Check pool is alive.
	select {
	case <-p.ctx.Done():
		return nil, errors.New("stream: pool is closed")
	default:
	}

	// Acquire concurrency slot.
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, errors.New("stream: pool is closed")
	}

	// Apply pool-level defaults.
	poolOpts := make([]Option, 0, 2+len(opts))
	poolOpts = append(poolOpts,
		WithChunkSize(p.Config.ChunkSize),
		WithBackpressure(p.Config.BackpressureMode),
	)
	poolOpts = append(poolOpts, opts...)

	// Build the stream with the pool's context as parent so pool shutdown
	// cancels all streams.
	streamCtx, streamCancel := context.WithCancel(p.ctx)
	// Apply stream timeout if configured.
	if p.Config.StreamTimeout > 0 {
		streamCtx, streamCancel = context.WithTimeout(p.ctx, p.Config.StreamTimeout)
	}

	s := NewStream(streamCtx, dir, bucket, key, p.chunkPool, poolOpts...)

	// Override the stream's cancel so Release can use it.
	// Since NewStream creates its own derived context, we need to wrap.
	origCancel := s.cancel
	s.cancel = func() {
		origCancel()
		streamCancel()
	}

	p.active.Store(s.ID, s)
	p.Metrics.ActiveStreams.Add(1)
	p.wg.Add(1)

	return s, nil
}

// Release returns a stream's concurrency slot to the pool.
// The stream is removed from the active set.
func (p *Pool) Release(s *Stream) {
	if _, loaded := p.active.LoadAndDelete(s.ID); !loaded {
		return // already released
	}

	p.Metrics.ActiveStreams.Add(-1)

	// Accumulate stream metrics into pool metrics.
	sm := s.Metrics()
	p.Metrics.RecordBytes(sm.BytesSent.Load() + sm.BytesRecv.Load())
	p.Metrics.TotalChunks.Add(sm.Chunks.Load())
	if s.State() == StateFailed {
		p.Metrics.RecordFailure()
	}

	// Return concurrency slot.
	select {
	case <-p.sem:
	default:
	}

	p.wg.Done()
}

// Get retrieves an active stream by ID.
func (p *Pool) Get(streamID id.ID) *Stream {
	v, ok := p.active.Load(streamID)
	if !ok {
		return nil
	}
	return v.(*Stream) //nolint:errcheck // type is always *Stream
}

// ActiveCount returns the number of currently active streams.
func (p *Pool) ActiveCount() int {
	return int(p.Metrics.ActiveStreams.Load())
}

// Range iterates over all active streams. The callback should not block.
func (p *Pool) Range(fn func(s *Stream) bool) {
	p.active.Range(func(_, v any) bool {
		return fn(v.(*Stream)) //nolint:errcheck // type is always *Stream
	})
}

// ChunkPool returns the pool's shared chunk buffer pool.
func (p *Pool) ChunkPool() *ChunkPool {
	return p.chunkPool
}

// Close gracefully shuts down the pool. It cancels the pool context,
// waits for all active streams to be released, and returns.
func (p *Pool) Close() error {
	p.cancel()

	// Cancel all active streams.
	p.active.Range(func(_, v any) bool {
		v.(*Stream).Close() //nolint:errcheck // best-effort close during shutdown
		return true
	})

	// Wait for all streams to be released (with timeout).
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		return errors.New("stream: pool close timed out waiting for active streams")
	}

	return nil
}
