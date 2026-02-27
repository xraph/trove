package stream

import (
	"sync/atomic"
	"time"
)

// PoolMetrics tracks streaming performance for a Pool.
type PoolMetrics struct {
	ActiveStreams atomic.Int64
	TotalBytes    atomic.Int64
	TotalChunks   atomic.Int64
	FailedStreams atomic.Int64
	RetryCount    atomic.Int64

	// Rolling average throughput (bytes/sec) stored as float64 bits.
	avgThroughput atomic.Uint64

	// P99 chunk latency stored as int64 nanoseconds.
	p99Latency atomic.Int64
}

// RecordBytes adds n bytes to the cumulative total.
func (m *PoolMetrics) RecordBytes(n int64) {
	m.TotalBytes.Add(n)
}

// RecordChunk increments the chunk counter.
func (m *PoolMetrics) RecordChunk() {
	m.TotalChunks.Add(1)
}

// RecordFailure increments the failed stream counter.
func (m *PoolMetrics) RecordFailure() {
	m.FailedStreams.Add(1)
}

// RecordRetry increments the retry counter.
func (m *PoolMetrics) RecordRetry() {
	m.RetryCount.Add(1)
}

// SetAvgThroughput stores the rolling average throughput (bytes/sec).
func (m *PoolMetrics) SetAvgThroughput(bps float64) {
	m.avgThroughput.Store(uint64(bps))
}

// AvgThroughput returns the rolling average throughput (bytes/sec).
func (m *PoolMetrics) AvgThroughput() float64 {
	return float64(m.avgThroughput.Load())
}

// SetP99Latency stores the P99 chunk latency.
func (m *PoolMetrics) SetP99Latency(d time.Duration) {
	m.p99Latency.Store(int64(d))
}

// P99Latency returns the P99 chunk latency.
func (m *PoolMetrics) P99Latency() time.Duration {
	return time.Duration(m.p99Latency.Load())
}

// Snapshot returns a point-in-time copy of all metrics.
func (m *PoolMetrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		ActiveStreams: m.ActiveStreams.Load(),
		TotalBytes:    m.TotalBytes.Load(),
		TotalChunks:   m.TotalChunks.Load(),
		FailedStreams: m.FailedStreams.Load(),
		RetryCount:    m.RetryCount.Load(),
		AvgThroughput: m.AvgThroughput(),
		P99Latency:    m.P99Latency(),
	}
}

// MetricsSnapshot is a serializable point-in-time copy of pool metrics.
type MetricsSnapshot struct {
	ActiveStreams int64         `json:"active_streams"`
	TotalBytes    int64         `json:"total_bytes"`
	TotalChunks   int64         `json:"total_chunks"`
	FailedStreams int64         `json:"failed_streams"`
	RetryCount    int64         `json:"retry_count"`
	AvgThroughput float64       `json:"avg_throughput_bps"`
	P99Latency    time.Duration `json:"p99_latency"`
}

// Metrics tracks per-stream transfer metrics.
type Metrics struct {
	BytesSent atomic.Int64
	BytesRecv atomic.Int64
	Chunks    atomic.Int64
	Retries   atomic.Int64
	StartTime time.Time
}

// NewMetrics creates a Metrics with the start time set to now.
func NewMetrics() *Metrics {
	return &Metrics{StartTime: time.Now()}
}

// Throughput returns the average throughput in bytes/sec since start.
func (sm *Metrics) Throughput() float64 {
	elapsed := time.Since(sm.StartTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	total := sm.BytesSent.Load() + sm.BytesRecv.Load()
	return float64(total) / elapsed
}

// StreamSnapshot returns a point-in-time copy of stream metrics.
func (sm *Metrics) StreamSnapshot() Snapshot {
	return Snapshot{
		BytesSent:  sm.BytesSent.Load(),
		BytesRecv:  sm.BytesRecv.Load(),
		Chunks:     sm.Chunks.Load(),
		Retries:    sm.Retries.Load(),
		Throughput: sm.Throughput(),
		Elapsed:    time.Since(sm.StartTime),
	}
}

// Snapshot is a serializable copy of per-stream metrics.
type Snapshot struct {
	BytesSent  int64         `json:"bytes_sent"`
	BytesRecv  int64         `json:"bytes_recv"`
	Chunks     int64         `json:"chunks"`
	Retries    int64         `json:"retries"`
	Throughput float64       `json:"throughput_bps"`
	Elapsed    time.Duration `json:"elapsed"`
}
