// Package watermark provides invisible metadata watermarking middleware for Trove.
// It embeds watermark text into downloaded content by injecting metadata chunks
// into supported file formats (PNG tEXt, JPEG COM, PDF info dictionary).
// No image processing libraries are required — only byte-level manipulation.
package watermark

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"strings"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
)

// Compile-time interface checks.
var (
	_ middleware.Middleware     = (*Watermark)(nil)
	_ middleware.ReadMiddleware = (*Watermark)(nil)
)

// TextFunc generates watermark text dynamically per-request.
type TextFunc func(ctx context.Context) string

// Option configures the Watermark middleware.
type Option func(*Watermark)

// WithText sets a static watermark text.
func WithText(text string) Option {
	return func(w *Watermark) {
		w.textFn = func(_ context.Context) string { return text }
	}
}

// WithTextFunc sets a dynamic watermark text function.
func WithTextFunc(fn TextFunc) Option {
	return func(w *Watermark) { w.textFn = fn }
}

// WithTypes sets content type prefixes that should be watermarked.
// Default: "image/png", "image/jpeg".
func WithTypes(prefixes ...string) Option {
	return func(w *Watermark) {
		w.types = prefixes
	}
}

// WithSkipTypes sets content type prefixes to skip watermarking.
func WithSkipTypes(prefixes ...string) Option {
	return func(w *Watermark) {
		w.skipTypes = prefixes
	}
}

// Watermark provides invisible metadata watermarking middleware.
type Watermark struct {
	textFn    TextFunc
	types     []string
	skipTypes []string
}

// New creates a new watermark middleware with the given options.
func New(opts ...Option) *Watermark {
	w := &Watermark{
		types: []string{"image/png", "image/jpeg"},
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Name returns the middleware identifier.
func (w *Watermark) Name() string { return "watermark" }

// Direction returns Read since watermarking only applies to downloads.
func (w *Watermark) Direction() middleware.Direction { return middleware.DirectionRead }

// WrapReader wraps a reader to inject watermark metadata into the content
// on download. Only matching content types are watermarked.
func (w *Watermark) WrapReader(ctx context.Context, r io.ReadCloser, info *driver.ObjectInfo) (io.ReadCloser, error) {
	if w.textFn == nil {
		return r, nil
	}

	text := w.textFn(ctx)
	if text == "" {
		return r, nil
	}

	// Check content type match.
	if info == nil || !w.matchesType(info.ContentType) {
		return r, nil
	}

	return &watermarkReader{
		inner:       r,
		text:        text,
		contentType: info.ContentType,
	}, nil
}

func (w *Watermark) matchesType(contentType string) bool {
	ct := strings.ToLower(contentType)

	// Check skip types first.
	for _, prefix := range w.skipTypes {
		if strings.HasPrefix(ct, strings.ToLower(prefix)) {
			return false
		}
	}

	// Check allowed types.
	for _, prefix := range w.types {
		if strings.HasPrefix(ct, strings.ToLower(prefix)) {
			return true
		}
	}

	return false
}

// watermarkReader buffers content, injects watermark, then serves.
type watermarkReader struct {
	inner       io.ReadCloser
	text        string
	contentType string
	buf         []byte
	readPos     int
	done        bool
}

func (r *watermarkReader) Read(p []byte) (int, error) {
	if !r.done {
		if err := r.inject(); err != nil {
			return 0, err
		}
		r.done = true
	}

	if r.readPos >= len(r.buf) {
		return 0, io.EOF
	}

	n := copy(p, r.buf[r.readPos:])
	r.readPos += n
	return n, nil
}

func (r *watermarkReader) inject() error {
	data, err := io.ReadAll(r.inner)
	if err != nil {
		return err
	}

	ct := strings.ToLower(r.contentType)

	switch {
	case strings.HasPrefix(ct, "image/png"):
		r.buf = injectPNG(data, r.text)
	case strings.HasPrefix(ct, "image/jpeg"):
		r.buf = injectJPEG(data, r.text)
	default:
		r.buf = data
	}

	return nil
}

func (r *watermarkReader) Close() error {
	return r.inner.Close()
}

// injectPNG inserts a tEXt chunk into a PNG file.
// PNG structure: 8-byte signature, then chunks (length + type + data + CRC).
// We insert a tEXt chunk after the IHDR chunk (first chunk after signature).
func injectPNG(data []byte, text string) []byte {
	// PNG signature is 8 bytes.
	if len(data) < 8 {
		return data
	}

	// Verify PNG signature.
	pngSig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !bytes.Equal(data[:8], pngSig) {
		return data
	}

	// Build tEXt chunk: keyword "Comment" + null separator + text.
	keyword := "Comment"
	chunkData := append([]byte(keyword), 0)
	chunkData = append(chunkData, []byte(text)...)

	// tEXt chunk: [4-byte length][4-byte type "tEXt"][data][4-byte CRC]
	var chunk bytes.Buffer
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(chunkData))) //nolint:gosec // chunk data fits in uint32
	chunk.Write(lenBuf)
	chunk.WriteString("tEXt")
	chunk.Write(chunkData)

	// CRC covers type + data.
	crcData := append([]byte("tEXt"), chunkData...)
	crcVal := crc32.ChecksumIEEE(crcData)
	crcBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBuf, crcVal)
	chunk.Write(crcBuf)

	// Find the end of the IHDR chunk (first chunk after signature).
	// Each chunk: 4-byte length + 4-byte type + data + 4-byte CRC.
	pos := 8 // after signature
	if pos+8 > len(data) {
		return data
	}

	ihdrLen := binary.BigEndian.Uint32(data[pos : pos+4])
	// IHDR chunk: 4 (length) + 4 (type) + ihdrLen (data) + 4 (CRC)
	ihdrEnd := pos + 4 + 4 + int(ihdrLen) + 4
	if ihdrEnd > len(data) {
		return data
	}

	// Insert tEXt chunk right after IHDR.
	result := make([]byte, 0, len(data)+chunk.Len())
	result = append(result, data[:ihdrEnd]...)
	result = append(result, chunk.Bytes()...)
	result = append(result, data[ihdrEnd:]...)

	return result
}

// injectJPEG inserts a COM (comment) marker segment into a JPEG file.
// JPEG structure: SOI (0xFFD8) then marker segments.
// We insert a COM segment right after SOI.
func injectJPEG(data []byte, text string) []byte {
	// JPEG starts with SOI: 0xFF 0xD8.
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return data
	}

	// Build COM segment: 0xFF 0xFE + 2-byte length + text.
	commentData := []byte(text)
	segLen := uint16(len(commentData) + 2) //nolint:gosec // comment data fits in uint16

	var segment bytes.Buffer
	segment.WriteByte(0xFF)
	segment.WriteByte(0xFE) // COM marker
	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, segLen)
	segment.Write(lenBuf)
	segment.Write(commentData)

	// Insert after SOI (2 bytes).
	result := make([]byte, 0, len(data)+segment.Len())
	result = append(result, data[:2]...) // SOI
	result = append(result, segment.Bytes()...)
	result = append(result, data[2:]...)

	return result
}
