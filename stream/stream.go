package stream

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/id"
)

// Direction indicates the data flow of a stream.
type Direction int

const (
	// DirectionUpload streams data from client to storage.
	DirectionUpload Direction = iota
	// DirectionDownload streams data from storage to client.
	DirectionDownload
	// DirectionBiDi allows simultaneous read and write (sync, mirroring).
	DirectionBiDi
)

// String returns a human-readable direction name.
func (d Direction) String() string {
	switch d {
	case DirectionUpload:
		return "upload"
	case DirectionDownload:
		return "download"
	case DirectionBiDi:
		return "bidi"
	default:
		return "unknown"
	}
}

// State represents the lifecycle state of a Stream.
type State string

const (
	StateIdle       State = "idle"
	StateActive     State = "active"
	StatePaused     State = "paused"
	StateCompleting State = "completing"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
	StateCancelled  State = "cancelled"
)

// IsTerminal reports whether this state is a final state.
func (s State) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

// ControlType identifies a stream control command.
type ControlType int

const (
	CtrlPause  ControlType = iota // Pause the stream.
	CtrlResume                    // Resume a paused stream.
	CtrlCancel                    // Cancel the stream.
	CtrlSeek                      // Seek to a byte offset.
)

// ControlMsg carries a control command for a stream.
type ControlMsg struct {
	Type   ControlType
	Offset int64 // For CtrlSeek.
}

// --- Hooks ---

// Progress holds a point-in-time progress report for a stream.
type Progress struct {
	StreamID  id.ID
	BytesSent int64
	BytesRecv int64
	TotalSize int64
	Percent   int
	Speed     float64 // bytes/sec
}

// ProgressHook is called periodically with transfer progress.
type ProgressHook func(Progress)

// ChunkHook is called after each chunk is sent/acknowledged.
type ChunkHook func(*Chunk, ChunkAck)

// CompleteHook is called when a stream completes successfully.
type CompleteHook func(*driver.ObjectInfo)

// ErrorHook is called when a stream encounters an error.
type ErrorHook func(error)

// --- Stream Options ---

// Option configures a Stream.
type Option func(*streamConfig)

type streamConfig struct {
	chunkSize    int64
	resumable    bool
	poolName     string
	channelSize  int
	backpressure Backpressure

	onProgress []ProgressHook
	onChunk    []ChunkHook
	onComplete []CompleteHook
	onError    []ErrorHook
}

func defaultStreamConfig() streamConfig {
	return streamConfig{
		chunkSize:    8 * 1024 * 1024, // 8MB
		channelSize:  16,
		backpressure: BackpressureBlock,
	}
}

// WithChunkSize sets the chunk size for this stream.
func WithChunkSize(size int64) Option {
	return func(c *streamConfig) { c.chunkSize = size }
}

// WithResumable enables resumable upload/download with offset tracking.
func WithResumable() Option {
	return func(c *streamConfig) { c.resumable = true }
}

// WithPool assigns this stream to a named pool.
func WithPool(name string) Option {
	return func(c *streamConfig) { c.poolName = name }
}

// WithChannelSize sets the chunk channel buffer size.
func WithChannelSize(n int) Option {
	return func(c *streamConfig) { c.channelSize = n }
}

// WithBackpressure sets the backpressure strategy for this stream.
func WithBackpressure(mode Backpressure) Option {
	return func(c *streamConfig) { c.backpressure = mode }
}

// WithOnProgress registers a progress callback.
func WithOnProgress(fn ProgressHook) Option {
	return func(c *streamConfig) { c.onProgress = append(c.onProgress, fn) }
}

// WithOnChunk registers a chunk callback.
func WithOnChunk(fn ChunkHook) Option {
	return func(c *streamConfig) { c.onChunk = append(c.onChunk, fn) }
}

// WithOnComplete registers a completion callback.
func WithOnComplete(fn CompleteHook) Option {
	return func(c *streamConfig) { c.onComplete = append(c.onComplete, fn) }
}

// WithOnError registers an error callback.
func WithOnError(fn ErrorHook) Option {
	return func(c *streamConfig) { c.onError = append(c.onError, fn) }
}

// --- Stream ---

// Stream represents a single managed data channel for transferring an
// object between a client and storage. It provides chunked transfer,
// backpressure, resumability, and lifecycle hooks.
type Stream struct {
	ID        id.ID
	Direction Direction
	Bucket    string
	Key       string

	config    streamConfig
	chunkPool *ChunkPool
	bp        BackpressureHandler
	metrics   *Metrics

	// State machine.
	state atomic.Value // State
	err   atomic.Value // error

	// Offset tracking for resumable transfers.
	offset    atomic.Int64
	totalSize int64

	// Channels.
	chunks  chan *Chunk
	ack     chan ChunkAck
	control chan ControlMsg

	// Lifecycle.
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once // guards Close
}

// NewStream creates a new stream in the Idle state.
func NewStream(
	ctx context.Context,
	dir Direction,
	bucket, key string,
	chunkPool *ChunkPool,
	opts ...Option,
) *Stream {
	cfg := defaultStreamConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, cancel := context.WithCancel(ctx)

	s := &Stream{
		ID:        id.NewStreamID(),
		Direction: dir,
		Bucket:    bucket,
		Key:       key,
		config:    cfg,
		chunkPool: chunkPool,
		metrics:   NewMetrics(),
		totalSize: -1, // unknown until set
		chunks:    make(chan *Chunk, cfg.channelSize),
		ack:       make(chan ChunkAck, cfg.channelSize),
		control:   make(chan ControlMsg, 4),
		ctx:       ctx,
		cancel:    cancel,
	}

	s.state.Store(StateIdle)
	s.bp = NewBackpressureHandler(cfg.backpressure, chunkPool)

	return s
}

// SetTotalSize sets the expected total size of the transfer. This enables
// percentage-based progress reporting.
func (s *Stream) SetTotalSize(n int64) {
	s.totalSize = n
}

// --- State Machine ---

// State returns the current stream state.
func (s *Stream) State() State {
	v, _ := s.state.Load().(State) //nolint:errcheck // type is guaranteed
	return v
}

// Err returns the last error, if any.
func (s *Stream) Err() error {
	v := s.err.Load()
	if v == nil {
		return nil
	}
	e, _ := v.(error) //nolint:errcheck // type is guaranteed
	return e
}

// transition moves the stream to a new state. Returns false if the
// transition is invalid (e.g., from a terminal state).
func (s *Stream) transition(to State) bool {
	current := s.State()
	if current.IsTerminal() {
		return false
	}

	switch to {
	case StateActive:
		if current != StateIdle && current != StatePaused {
			return false
		}
	case StatePaused:
		if current != StateActive {
			return false
		}
	case StateCompleting:
		if current != StateActive {
			return false
		}
	case StateCompleted:
		if current != StateCompleting {
			return false
		}
	case StateFailed, StateCancelled:
		// Can always fail or cancel from non-terminal states.
	default:
		return false
	}

	s.state.Store(to)
	return true
}

// Start activates the stream (idle → active).
func (s *Stream) Start() error {
	if !s.transition(StateActive) {
		return errors.New("stream: cannot start from state " + string(s.State()))
	}
	return nil
}

// --- Data Flow ---

// Write sends a chunk into the stream. Applies the configured backpressure
// strategy if the chunk channel is full.
func (s *Stream) Write(chunk *Chunk) error {
	state := s.State()
	if state.IsTerminal() {
		return errors.New("stream: write to closed stream")
	}
	if state == StatePaused {
		return errors.New("stream: write to paused stream")
	}
	if state != StateActive {
		return errors.New("stream: stream not active")
	}

	if err := s.bp.Send(s.ctx, s.chunks, chunk); err != nil {
		return err
	}

	s.metrics.BytesSent.Add(int64(chunk.Size))
	s.metrics.Chunks.Add(1)
	s.offset.Add(int64(chunk.Size))

	// Fire chunk hooks.
	for _, fn := range s.config.onChunk {
		fn(chunk, ChunkAck{Index: chunk.Index})
	}

	// Fire progress hooks.
	s.fireProgress()

	return nil
}

// Read receives the next chunk from the stream. Blocks until a chunk is
// available or the context is cancelled.
func (s *Stream) Read(ctx context.Context) (*Chunk, error) {
	select {
	case chunk, ok := <-s.chunks:
		if !ok {
			return nil, io.EOF
		}
		s.metrics.BytesRecv.Add(int64(chunk.Size))
		return chunk, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Ack sends an acknowledgement for a processed chunk.
func (s *Stream) Ack(ack ChunkAck) {
	select {
	case s.ack <- ack:
	default:
	}
}

// ReadAck receives the next ack. Blocks until available or context cancelled.
func (s *Stream) ReadAck(ctx context.Context) (ChunkAck, error) {
	select {
	case ack, ok := <-s.ack:
		if !ok {
			return ChunkAck{}, io.EOF
		}
		return ack, nil
	case <-ctx.Done():
		return ChunkAck{}, ctx.Err()
	case <-s.ctx.Done():
		return ChunkAck{}, s.ctx.Err()
	}
}

// --- Control ---

// Control sends a control command to the stream.
func (s *Stream) Control(msg ControlMsg) error {
	if s.State().IsTerminal() {
		return errors.New("stream: cannot control terminated stream")
	}

	switch msg.Type {
	case CtrlPause:
		if !s.transition(StatePaused) {
			return errors.New("stream: cannot pause from state " + string(s.State()))
		}
	case CtrlResume:
		if !s.transition(StateActive) {
			return errors.New("stream: cannot resume from state " + string(s.State()))
		}
	case CtrlCancel:
		s.fail(StateCancelled, context.Canceled)
		return nil
	case CtrlSeek:
		s.offset.Store(msg.Offset)
	}

	select {
	case s.control <- msg:
	default:
	}
	return nil
}

// ControlChan returns the read-only control channel for consuming
// control messages in the stream's processing loop.
func (s *Stream) ControlChan() <-chan ControlMsg {
	return s.control
}

// --- Completion ---

// Complete transitions the stream through completing → completed. It
// closes the chunk channel to signal EOF to the reader and fires completion hooks.
func (s *Stream) Complete(info *driver.ObjectInfo) error {
	if !s.transition(StateCompleting) {
		return errors.New("stream: cannot complete from state " + string(s.State()))
	}
	close(s.chunks)
	if !s.transition(StateCompleted) {
		return errors.New("stream: internal state error on completion")
	}
	for _, fn := range s.config.onComplete {
		fn(info)
	}
	return nil
}

// Fail transitions the stream to the failed state with an error.
func (s *Stream) Fail(err error) {
	s.fail(StateFailed, err)
}

func (s *Stream) fail(state State, err error) {
	if s.State().IsTerminal() {
		return
	}
	s.err.Store(err)
	s.state.Store(state)
	s.cancel()
	for _, fn := range s.config.onError {
		fn(err)
	}
}

// Close releases all stream resources. Safe to call multiple times.
func (s *Stream) Close() {
	s.once.Do(func() {
		if !s.State().IsTerminal() {
			s.fail(StateCancelled, context.Canceled)
		}
	})
}

// Done returns a channel that is closed when the stream's context is
// cancelled (either by completion, failure, or explicit cancel).
func (s *Stream) Done() <-chan struct{} {
	return s.ctx.Done()
}

// Offset returns the current byte offset of the transfer.
func (s *Stream) Offset() int64 {
	return s.offset.Load()
}

// Metrics returns the stream's transfer metrics.
func (s *Stream) Metrics() *Metrics {
	return s.metrics
}

// ChunkPool returns the chunk pool used by this stream.
func (s *Stream) ChunkPool() *ChunkPool {
	return s.chunkPool
}

// Config returns the stream's backpressure mode.
func (s *Stream) BackpressureMode() Backpressure {
	return s.config.backpressure
}

// Resumable reports whether this stream supports resume from offset.
func (s *Stream) Resumable() bool {
	return s.config.resumable
}

// --- internal helpers ---

func (s *Stream) fireProgress() {
	if len(s.config.onProgress) == 0 {
		return
	}

	sent := s.metrics.BytesSent.Load()
	recv := s.metrics.BytesRecv.Load()

	p := Progress{
		StreamID:  s.ID,
		BytesSent: sent,
		BytesRecv: recv,
		TotalSize: s.totalSize,
		Speed:     s.metrics.Throughput(),
	}

	if s.totalSize > 0 {
		total := sent + recv
		p.Percent = int(total * 100 / s.totalSize)
		if p.Percent > 100 {
			p.Percent = 100
		}
	}

	for _, fn := range s.config.onProgress {
		fn(p)
	}
}
