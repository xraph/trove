// Package cas provides content-addressable storage for Trove.
// Objects are stored under their content hash, enabling automatic
// deduplication, integrity verification, and immutable storage patterns.
package cas

import (
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/xraph/trove/internal"
)

// HashAlgorithm identifies the hash algorithm for CAS addressing.
type HashAlgorithm int

const (
	// AlgSHA256 uses SHA-256 (default, widely compatible).
	AlgSHA256 HashAlgorithm = iota
	// AlgBlake3 uses BLAKE3 (faster, modern).
	AlgBlake3
	// AlgXXHash uses XXHash (non-cryptographic, fastest).
	AlgXXHash
)

// String returns the algorithm name.
func (a HashAlgorithm) String() string {
	switch a {
	case AlgSHA256:
		return "sha256"
	case AlgBlake3:
		return "blake3"
	case AlgXXHash:
		return "xxhash"
	default:
		return "unknown"
	}
}

// toInternal converts to the internal checksum algorithm type.
func (a HashAlgorithm) toInternal() internal.ChecksumAlgorithm {
	switch a {
	case AlgSHA256:
		return internal.AlgSHA256
	case AlgBlake3:
		return internal.AlgBlake3
	case AlgXXHash:
		return internal.AlgXXHash
	default:
		return internal.AlgSHA256
	}
}

// newHash creates a new hash.Hash for the given CAS algorithm.
func newHash(alg HashAlgorithm) (hash.Hash, error) {
	return internal.NewHash(alg.toInternal())
}

// computeHashBytes hashes a byte slice and returns "algorithm:hex" string.
func computeHashBytes(data []byte, alg HashAlgorithm) (string, error) {
	h, err := newHash(alg)
	if err != nil {
		return "", err
	}

	if _, err := h.Write(data); err != nil {
		return "", fmt.Errorf("cas: compute hash: %w", err)
	}

	return fmt.Sprintf("%s:%s", alg, hex.EncodeToString(h.Sum(nil))), nil
}
