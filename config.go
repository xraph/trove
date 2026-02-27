package trove

import "time"

// ChecksumAlgorithm identifies the hash algorithm used for integrity verification.
type ChecksumAlgorithm int

const (
	// SHA256 uses the SHA-256 hash algorithm (default).
	SHA256 ChecksumAlgorithm = iota
	// Blake3 uses the BLAKE3 hash algorithm (faster, modern).
	Blake3
	// XXHash uses the XXHash algorithm (non-cryptographic, fastest).
	XXHash
)

// String returns the algorithm name.
func (a ChecksumAlgorithm) String() string {
	switch a {
	case SHA256:
		return "sha256"
	case Blake3:
		return "blake3"
	case XXHash:
		return "xxhash"
	default:
		return "unknown"
	}
}

// RetryStrategy identifies the retry backoff strategy.
type RetryStrategy int

const (
	// RetryNone disables retries.
	RetryNone RetryStrategy = iota
	// RetryFixed uses a fixed delay between retries.
	RetryFixed
	// RetryExponential uses exponential backoff.
	RetryExponential
)

// RetryPolicy configures retry behavior for storage operations.
type RetryPolicy struct {
	// Strategy is the backoff strategy.
	Strategy RetryStrategy

	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int

	// BaseDelay is the initial delay between retries.
	BaseDelay time.Duration

	// MaxDelay is the maximum delay between retries (for exponential backoff).
	MaxDelay time.Duration
}

// Config holds the configuration for a Trove instance.
type Config struct {
	// DefaultBucket is the bucket used when none is specified.
	DefaultBucket string

	// ChunkSize is the size in bytes for streaming chunks.
	ChunkSize int64

	// PoolSize is the maximum number of concurrent streams.
	PoolSize int

	// ChecksumAlgorithm is the hash algorithm for integrity verification.
	ChecksumAlgorithm ChecksumAlgorithm

	// StreamBufferSize is the buffer size in bytes for stream operations.
	StreamBufferSize int

	// Retry configures retry behavior for storage operations.
	Retry RetryPolicy
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ChunkSize:         8 * 1024 * 1024, // 8MB
		PoolSize:          16,
		ChecksumAlgorithm: SHA256,
		StreamBufferSize:  32 * 1024, // 32KB
		Retry: RetryPolicy{
			Strategy:    RetryExponential,
			MaxAttempts: 3,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    10 * time.Second,
		},
	}
}
