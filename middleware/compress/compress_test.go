package compress

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/middleware"
)

func TestCompress_InterfaceCompliance(t *testing.T) {
	c := New()
	var _ middleware.Middleware = c
	var _ middleware.ReadMiddleware = c
	var _ middleware.WriteMiddleware = c
	assert.Equal(t, "compress", c.Name())
	assert.Equal(t, middleware.DirectionReadWrite, c.Direction())
}

func TestCompress_Roundtrip(t *testing.T) {
	c := New(WithMinSize(0)) // compress everything
	ctx := context.Background()

	// Use data large enough to actually compress.
	plaintext := []byte(strings.Repeat("hello world! this is compressible text. ", 100))

	// Compress.
	var compressed bytes.Buffer
	w, err := c.WrapWriter(ctx, &nopWriteCloser{&compressed}, "test.txt")
	require.NoError(t, err)
	_, err = w.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Compressed data should be smaller.
	assert.Less(t, compressed.Len(), len(plaintext))

	// Decompress.
	r, err := c.WrapReader(ctx, io.NopCloser(bytes.NewReader(compressed.Bytes())), nil)
	require.NoError(t, err)

	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, plaintext, decompressed)
}

func TestCompress_SmallData_SkipCompression(t *testing.T) {
	c := New() // default minSize = 1024
	ctx := context.Background()

	plaintext := []byte("small data")

	var output bytes.Buffer
	w, err := c.WrapWriter(ctx, &nopWriteCloser{&output}, "small.txt")
	require.NoError(t, err)
	_, err = w.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Data below minSize should be written uncompressed.
	assert.Equal(t, plaintext, output.Bytes())

	// Reader should still handle uncompressed data.
	r, err := c.WrapReader(ctx, io.NopCloser(bytes.NewReader(output.Bytes())), nil)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decompressed)
}

func TestCompress_SkipExtensions(t *testing.T) {
	c := New(WithMinSize(0))
	ctx := context.Background()

	data := []byte(strings.Repeat("compressible data ", 100))

	for _, ext := range []string{"test.jpg", "test.png", "test.zip", "test.gz", "test.mp4"} {
		var output bytes.Buffer
		w, err := c.WrapWriter(ctx, &nopWriteCloser{&output}, ext)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
		require.NoError(t, w.Close())

		// Skipped extensions should pass data through uncompressed.
		assert.Equal(t, data, output.Bytes(), "expected passthrough for %s", ext)
	}
}

func TestCompress_CustomExclude(t *testing.T) {
	c := New(WithMinSize(0), WithExclude("custom", ".dat"))
	ctx := context.Background()

	data := []byte(strings.Repeat("compressible data ", 100))

	var output bytes.Buffer
	w, err := c.WrapWriter(ctx, &nopWriteCloser{&output}, "test.custom")
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Equal(t, data, output.Bytes(), "custom extension should be skipped")

	output.Reset()
	w, err = c.WrapWriter(ctx, &nopWriteCloser{&output}, "test.dat")
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Equal(t, data, output.Bytes(), ".dat extension should be skipped")
}

func TestCompress_EmptyData(t *testing.T) {
	c := New(WithMinSize(0))
	ctx := context.Background()

	var compressed bytes.Buffer
	w, err := c.WrapWriter(ctx, &nopWriteCloser{&compressed}, "empty.txt")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := c.WrapReader(ctx, io.NopCloser(bytes.NewReader(compressed.Bytes())), nil)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, decompressed)
}

func TestCompress_IncompressibleData(t *testing.T) {
	// When compression makes data larger, original data should be kept.
	c := New(WithMinSize(0))
	ctx := context.Background()

	// Random-like data that won't compress well (short, high entropy).
	plaintext := make([]byte, 32)
	for i := range plaintext {
		plaintext[i] = byte(i * 7)
	}

	var output bytes.Buffer
	w, err := c.WrapWriter(ctx, &nopWriteCloser{&output}, "random.bin")
	require.NoError(t, err)
	_, err = w.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Read back — should handle uncompressed data.
	r, err := c.WrapReader(ctx, io.NopCloser(bytes.NewReader(output.Bytes())), nil)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decompressed)
}

// nopWriteCloser wraps a bytes.Buffer as an io.WriteCloser.
type nopWriteCloser struct {
	buf *bytes.Buffer
}

func (w *nopWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *nopWriteCloser) Close() error                { return nil }
