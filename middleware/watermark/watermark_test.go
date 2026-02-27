package watermark

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
)

// bufReadCloser wraps bytes as an io.ReadCloser.
type bufReadCloser struct {
	*bytes.Reader
	closed bool
}

func newBufReadCloser(data []byte) *bufReadCloser {
	return &bufReadCloser{Reader: bytes.NewReader(data)}
}

func (b *bufReadCloser) Close() error {
	b.closed = true
	return nil
}

func TestName(t *testing.T) {
	w := New()
	assert.Equal(t, "watermark", w.Name())
}

func TestDirection(t *testing.T) {
	w := New()
	assert.Equal(t, middleware.DirectionRead, w.Direction())
}

func TestInterfaceCompliance(_ *testing.T) {
	var _ middleware.Middleware = New()
	var _ middleware.ReadMiddleware = New()
}

func TestPassthroughNonMatchingType(t *testing.T) {
	w := New(WithText("test-watermark"))

	data := []byte("plain text content")
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "text/plain"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	// Should return the original reader (passthrough).
	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestPassthroughNoText(t *testing.T) {
	w := New() // no text set

	data := makePNG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/png"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestPassthroughEmptyText(t *testing.T) {
	w := New(WithText(""))

	data := makePNG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/png"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestPNGWatermark(t *testing.T) {
	w := New(WithText("owned-by-test"))

	data := makePNG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/png"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)

	// Result should be larger (has extra tEXt chunk).
	assert.Greater(t, len(got), len(data))

	// Should still start with PNG signature.
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, got[:8])

	// Should contain the watermark text.
	assert.Contains(t, string(got), "owned-by-test")

	// Should contain the tEXt chunk type.
	assert.Contains(t, string(got), "tEXt")
}

func TestJPEGWatermark(t *testing.T) {
	w := New(WithText("jpeg-watermark"))

	data := makeJPEG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/jpeg"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)

	// Should start with SOI.
	assert.Equal(t, byte(0xFF), got[0])
	assert.Equal(t, byte(0xD8), got[1])

	// Next should be COM marker.
	assert.Equal(t, byte(0xFF), got[2])
	assert.Equal(t, byte(0xFE), got[3])

	// Should contain the watermark text.
	assert.Contains(t, string(got), "jpeg-watermark")
}

func TestDynamicText(t *testing.T) {
	w := New(WithTextFunc(func(_ context.Context) string {
		return "dynamic-watermark-123"
	}))

	data := makePNG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/png"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)

	assert.Contains(t, string(got), "dynamic-watermark-123")
}

func TestCustomTypes(t *testing.T) {
	w := New(
		WithText("custom-type-watermark"),
		WithTypes("image/png"), // only PNG
	)

	// JPEG should be passthrough since we only set PNG.
	data := makeJPEG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/jpeg"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestSkipTypes(t *testing.T) {
	w := New(
		WithText("test"),
		WithSkipTypes("image/png"),
	)

	data := makePNG()
	reader := newBufReadCloser(data)
	info := &driver.ObjectInfo{ContentType: "image/png"}

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, info)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestNilInfo(t *testing.T) {
	w := New(WithText("test"))

	data := []byte("some data")
	reader := newBufReadCloser(data)

	ctx := context.Background()
	result, err := w.WrapReader(ctx, reader, nil)
	require.NoError(t, err)

	got, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

// --- Test helpers ---

// makePNG creates a minimal valid PNG file.
func makePNG() []byte {
	var buf bytes.Buffer

	// PNG signature.
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	// IHDR chunk (minimal): 13 bytes of data.
	ihdr := make([]byte, 13)                 // width(4) + height(4) + bit_depth(1) + color_type(1) + compression(1) + filter(1) + interlace(1)
	binary.BigEndian.PutUint32(ihdr[0:4], 1) // width = 1
	binary.BigEndian.PutUint32(ihdr[4:8], 1) // height = 1
	ihdr[8] = 8                              // bit depth
	ihdr[9] = 2                              // color type (RGB)

	writeChunk(&buf, "IHDR", ihdr)

	// IDAT chunk (minimal).
	writeChunk(&buf, "IDAT", []byte{0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x01, 0x01, 0x01, 0x00})

	// IEND chunk.
	writeChunk(&buf, "IEND", nil)

	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	buf.Write(lenBuf)
	buf.WriteString(chunkType)
	buf.Write(data)

	crcData := append([]byte(chunkType), data...)
	crcVal := crc32.ChecksumIEEE(crcData)
	crcBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBuf, crcVal)
	buf.Write(crcBuf)
}

// makeJPEG creates a minimal valid JPEG file.
func makeJPEG() []byte {
	var buf bytes.Buffer
	// SOI
	buf.WriteByte(0xFF)
	buf.WriteByte(0xD8)
	// APP0 (JFIF) marker - minimal
	buf.WriteByte(0xFF)
	buf.WriteByte(0xE0)
	buf.Write([]byte{0x00, 0x10}) // length = 16
	buf.WriteString("JFIF\x00")
	buf.Write([]byte{0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00})
	// EOI
	buf.WriteByte(0xFF)
	buf.WriteByte(0xD9)
	return buf.Bytes()
}
