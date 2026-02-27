// Package internal contains core domain logic with zero Forge imports.
package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"

	"github.com/cespare/xxhash/v2"
	"github.com/zeebo/blake3"
)

// ChecksumAlgorithm identifies the hash algorithm used for integrity verification.
type ChecksumAlgorithm string

const (
	AlgSHA256 ChecksumAlgorithm = "sha256"
	AlgBlake3 ChecksumAlgorithm = "blake3"
	AlgXXHash ChecksumAlgorithm = "xxhash"
)

// Checksum holds integrity verification data.
type Checksum struct {
	Algorithm ChecksumAlgorithm `json:"algorithm"`
	Value     string            `json:"value"`
}

// NewHash returns a new hash.Hash for the given algorithm.
func NewHash(alg ChecksumAlgorithm) (hash.Hash, error) {
	switch alg {
	case AlgSHA256:
		return sha256.New(), nil
	case AlgBlake3:
		return blake3.New(), nil
	case AlgXXHash:
		return xxhash.New(), nil
	default:
		return nil, fmt.Errorf("checksum: unknown algorithm %q", alg)
	}
}

// ComputeChecksum reads all data from r and computes a checksum using the
// specified algorithm. The reader is consumed entirely.
func ComputeChecksum(r io.Reader, alg ChecksumAlgorithm) (Checksum, error) {
	h, err := NewHash(alg)
	if err != nil {
		return Checksum{}, err
	}

	if _, err := io.Copy(h, r); err != nil {
		return Checksum{}, fmt.Errorf("checksum: compute %s: %w", alg, err)
	}

	return Checksum{
		Algorithm: alg,
		Value:     hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// ComputeChecksumBytes computes a checksum of the given byte slice.
func ComputeChecksumBytes(data []byte, alg ChecksumAlgorithm) (Checksum, error) {
	h, err := NewHash(alg)
	if err != nil {
		return Checksum{}, err
	}

	if _, err := h.Write(data); err != nil {
		return Checksum{}, fmt.Errorf("checksum: compute %s: %w", alg, err)
	}

	return Checksum{
		Algorithm: alg,
		Value:     hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// VerifyChecksum reads all data from r and verifies it matches the expected checksum.
func VerifyChecksum(r io.Reader, expected Checksum) error {
	actual, err := ComputeChecksum(r, expected.Algorithm)
	if err != nil {
		return err
	}

	if actual.Value != expected.Value {
		return fmt.Errorf("checksum: %s mismatch: expected %s, got %s",
			expected.Algorithm, expected.Value, actual.Value)
	}

	return nil
}
