package stream

import (
	"context"
	"errors"
	"sync"
)

// Backpressure identifies the backpressure strategy used when a stream's
// consumer cannot keep up with the producer.
type Backpressure int

const (
	// BackpressureBlock blocks the sender until the consumer catches up.
	BackpressureBlock Backpressure = iota

	// BackpressureDrop drops the oldest unacknowledged chunks.
	BackpressureDrop

	// BackpressureBuffer spills chunks to a temporary buffer when
	// the in-memory channel fills.
	BackpressureBuffer

	// BackpressureAdaptive dynamically adjusts chunk size based on
	// measured throughput vs. consumption rate.
	BackpressureAdaptive
)

// String returns the strategy name.
func (b Backpressure) String() string {
	switch b {
	case BackpressureBlock:
		return "block"
	case BackpressureDrop:
		return "drop"
	case BackpressureBuffer:
		return "buffer"
	case BackpressureAdaptive:
		return "adaptive"
	default:
		return "unknown"
	}
}

// ErrBackpressure is returned when a non-blocking send would block.
var ErrBackpressure = errors.New("stream: backpressure limit reached")

// BackpressureHandler defines how a stream reacts when its chunk channel
// is full. Each strategy implements Send differently.
type BackpressureHandler interface {
	// Send delivers a chunk to the target channel, applying the strategy
	// when the channel is full. Returns ErrBackpressure or context errors.
	Send(ctx context.Context, ch chan *Chunk, chunk *Chunk) error
}

// --- Block Strategy ---

type blockHandler struct{}

// NewBlockHandler creates a handler that blocks until the channel has room
// or the context is cancelled.
func NewBlockHandler() BackpressureHandler {
	return &blockHandler{}
}

func (h *blockHandler) Send(ctx context.Context, ch chan *Chunk, chunk *Chunk) error {
	select {
	case ch <- chunk:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- Drop Strategy ---

type dropHandler struct {
	pool *ChunkPool
}

// NewDropHandler creates a handler that drops the oldest chunk in the
// channel when full, returning its buffer to the pool.
func NewDropHandler(pool *ChunkPool) BackpressureHandler {
	return &dropHandler{pool: pool}
}

func (h *dropHandler) Send(ctx context.Context, ch chan *Chunk, chunk *Chunk) error {
	select {
	case ch <- chunk:
		return nil
	default:
		// Channel full — drain the oldest chunk and retry.
		select {
		case old := <-ch:
			if h.pool != nil {
				h.pool.Put(old.Data)
			}
		default:
		}
		// Try once more; if still full, return error.
		select {
		case ch <- chunk:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return ErrBackpressure
		}
	}
}

// --- Buffer Strategy ---

type bufferHandler struct {
	mu       sync.Mutex
	overflow []*Chunk
	maxSize  int
}

// NewBufferHandler creates a handler that spills chunks to an internal
// slice when the channel is full, up to maxOverflow extra chunks.
func NewBufferHandler(maxOverflow int) BackpressureHandler {
	return &bufferHandler{maxSize: maxOverflow}
}

func (h *bufferHandler) Send(_ context.Context, ch chan *Chunk, chunk *Chunk) error {
	// First try to drain any overflow into the channel.
	h.drain(ch)

	select {
	case ch <- chunk:
		return nil
	default:
		// Channel full — buffer the chunk.
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.overflow) >= h.maxSize {
			return ErrBackpressure
		}
		h.overflow = append(h.overflow, chunk)
		return nil
	}
}

func (h *bufferHandler) drain(ch chan *Chunk) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for len(h.overflow) > 0 {
		select {
		case ch <- h.overflow[0]:
			h.overflow = h.overflow[1:]
		default:
			return
		}
	}
}

// Overflow returns the number of chunks currently in the overflow buffer.
func (h *bufferHandler) Overflow() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.overflow)
}

// --- Adaptive Strategy ---

type adaptiveHandler struct {
	blockHandler
}

// NewAdaptiveHandler creates a handler that blocks (like BackpressureBlock)
// but signals that chunk sizes should be adjusted. Future phases will
// integrate throughput-based auto-tuning.
func NewAdaptiveHandler() BackpressureHandler {
	return &adaptiveHandler{}
}

// NewBackpressureHandler constructs the appropriate handler for the strategy.
func NewBackpressureHandler(mode Backpressure, pool *ChunkPool) BackpressureHandler {
	switch mode {
	case BackpressureDrop:
		return NewDropHandler(pool)
	case BackpressureBuffer:
		return NewBufferHandler(256) // default overflow capacity
	case BackpressureAdaptive:
		return NewAdaptiveHandler()
	default:
		return NewBlockHandler()
	}
}
