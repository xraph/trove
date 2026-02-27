// Package stream provides a managed streaming engine for Trove with
// backpressure, chunked transfers, buffer pooling, and concurrency control.
package stream

import (
	"sync"
	"sync/atomic"
)

// Chunk is a unit of data in a stream. Buffers are drawn from a ChunkPool
// and MUST be returned via ChunkPool.Put when no longer needed.
type Chunk struct {
	// Index is the sequential chunk number within the stream (0-based).
	Index int

	// Offset is the byte offset of this chunk within the full object.
	Offset int64

	// Data holds the chunk payload. The backing slice comes from a ChunkPool
	// and must be returned after processing.
	Data []byte

	// Size is the number of valid bytes in Data (may be less than cap(Data)
	// for the final chunk).
	Size int

	// Checksum holds an optional integrity hash for this chunk.
	Checksum Checksum

	// Final marks this as the last chunk in the stream.
	Final bool
}

// Checksum holds an integrity hash for a chunk.
type Checksum struct {
	Algorithm string // "sha256", "blake3", "xxhash"
	Value     []byte
}

// ChunkAck is the backend's acknowledgement of a received chunk.
type ChunkAck struct {
	Index int
	ETag  string
	Err   error
}

// ChunkPool manages a pool of reusable byte buffers for zero-copy chunk
// allocation. It wraps sync.Pool with fixed-size buffers and tracks
// allocation metrics.
type ChunkPool struct {
	pool      sync.Pool
	chunkSize int64
	allocated atomic.Int64
	released  atomic.Int64
}

// NewChunkPool creates a ChunkPool that allocates buffers of the given size.
func NewChunkPool(chunkSize int64) *ChunkPool {
	cp := &ChunkPool{chunkSize: chunkSize}
	cp.pool = sync.Pool{
		New: func() any {
			cp.allocated.Add(1)
			return make([]byte, chunkSize)
		},
	}
	return cp
}

// Get returns a buffer from the pool. The caller MUST return it via Put.
func (cp *ChunkPool) Get() []byte {
	buf, _ := cp.pool.Get().([]byte) //nolint:errcheck // pool always returns []byte
	return buf
}

// Put returns a buffer to the pool for reuse. The buffer is reset to its
// original capacity (zero-length but full cap).
func (cp *ChunkPool) Put(buf []byte) {
	// Only return buffers that match the pool's chunk size to prevent
	// returning short buffers from final chunks.
	if int64(cap(buf)) == cp.chunkSize {
		cp.released.Add(1)
		cp.pool.Put(buf[:cp.chunkSize]) //nolint:staticcheck // pool reuse is intentional
	}
}

// ChunkSize returns the size of buffers allocated by this pool.
func (cp *ChunkPool) ChunkSize() int64 {
	return cp.chunkSize
}

// Stats returns allocation statistics.
func (cp *ChunkPool) Stats() ChunkPoolStats {
	return ChunkPoolStats{
		Allocated: cp.allocated.Load(),
		Released:  cp.released.Load(),
		ChunkSize: cp.chunkSize,
	}
}

// ChunkPoolStats holds chunk pool allocation metrics.
type ChunkPoolStats struct {
	Allocated int64 `json:"allocated"`
	Released  int64 `json:"released"`
	ChunkSize int64 `json:"chunk_size"`
}
