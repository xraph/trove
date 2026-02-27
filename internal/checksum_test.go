package internal_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/internal"
)

func TestComputeChecksum(t *testing.T) {
	data := "hello world"

	tests := []struct {
		name string
		alg  internal.ChecksumAlgorithm
	}{
		{"SHA256", internal.AlgSHA256},
		{"Blake3", internal.AlgBlake3},
		{"XXHash", internal.AlgXXHash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, err := internal.ComputeChecksum(strings.NewReader(data), tt.alg)
			require.NoError(t, err)
			assert.Equal(t, tt.alg, cs.Algorithm)
			assert.NotEmpty(t, cs.Value)

			// Same input produces same checksum.
			cs2, err := internal.ComputeChecksum(strings.NewReader(data), tt.alg)
			require.NoError(t, err)
			assert.Equal(t, cs.Value, cs2.Value)

			// Different input produces different checksum.
			cs3, err := internal.ComputeChecksum(strings.NewReader("different data"), tt.alg)
			require.NoError(t, err)
			assert.NotEqual(t, cs.Value, cs3.Value)
		})
	}
}

func TestComputeChecksumBytes(t *testing.T) {
	data := []byte("hello world")

	cs, err := internal.ComputeChecksumBytes(data, internal.AlgSHA256)
	require.NoError(t, err)
	assert.Equal(t, internal.AlgSHA256, cs.Algorithm)
	assert.NotEmpty(t, cs.Value)

	// Must match streaming computation.
	cs2, err := internal.ComputeChecksum(bytes.NewReader(data), internal.AlgSHA256)
	require.NoError(t, err)
	assert.Equal(t, cs.Value, cs2.Value)
}

func TestVerifyChecksum(t *testing.T) {
	data := "hello world"

	t.Run("valid checksum", func(t *testing.T) {
		cs, err := internal.ComputeChecksum(strings.NewReader(data), internal.AlgSHA256)
		require.NoError(t, err)

		err = internal.VerifyChecksum(strings.NewReader(data), cs)
		assert.NoError(t, err)
	})

	t.Run("invalid checksum", func(t *testing.T) {
		cs := internal.Checksum{
			Algorithm: internal.AlgSHA256,
			Value:     "0000000000000000000000000000000000000000000000000000000000000000",
		}

		err := internal.VerifyChecksum(strings.NewReader(data), cs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mismatch")
	})
}

func TestNewHash_UnknownAlgorithm(t *testing.T) {
	_, err := internal.NewHash("unknown")
	assert.Error(t, err)
}

func TestComputeChecksum_UnknownAlgorithm(t *testing.T) {
	_, err := internal.ComputeChecksum(strings.NewReader("data"), "unknown")
	assert.Error(t, err)
}
