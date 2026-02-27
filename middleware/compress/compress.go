// Package compress provides Zstd compression middleware for Trove.
// It compresses data on write and decompresses on read, with configurable
// minimum size thresholds and skip lists for already-compressed formats.
package compress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
)

// Compile-time interface checks.
var (
	_ middleware.Middleware      = (*Compress)(nil)
	_ middleware.ReadMiddleware  = (*Compress)(nil)
	_ middleware.WriteMiddleware = (*Compress)(nil)
)

// defaultSkipExtensions are file extensions that are already compressed.
var defaultSkipExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".mp4":  true,
	".mp3":  true,
	".avi":  true,
	".mkv":  true,
	".zip":  true,
	".gz":   true,
	".bz2":  true,
	".xz":   true,
	".zst":  true,
	".br":   true,
	".rar":  true,
	".7z":   true,
}

// magic bytes for zstd compressed data.
var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// Option configures the Compress middleware.
type Option func(*Compress)

// WithMinSize sets the minimum object size to compress.
// Objects smaller than this are passed through uncompressed.
func WithMinSize(size int64) Option {
	return func(c *Compress) { c.minSize = size }
}

// WithExclude adds file extensions to skip (already compressed formats).
func WithExclude(extensions ...string) Option {
	return func(c *Compress) {
		for _, ext := range extensions {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			c.skipExt[ext] = true
		}
	}
}

// Compress provides Zstd compression middleware.
type Compress struct {
	minSize int64
	skipExt map[string]bool
}

// New creates a new compression middleware.
func New(opts ...Option) *Compress {
	c := &Compress{
		minSize: 1024, // default: skip files < 1KB
		skipExt: make(map[string]bool),
	}
	for k, v := range defaultSkipExtensions {
		c.skipExt[k] = v
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the middleware identifier.
func (c *Compress) Name() string { return "compress" }

// Direction returns ReadWrite since compression participates in both paths.
func (c *Compress) Direction() middleware.Direction { return middleware.DirectionReadWrite }

// WrapWriter wraps a writer to compress data before writing.
// Data is buffered, then compressed and written on Close.
func (c *Compress) WrapWriter(_ context.Context, w io.WriteCloser, key string) (io.WriteCloser, error) {
	if c.shouldSkip(key) {
		return w, nil
	}
	return &compressWriter{
		inner:   w,
		minSize: c.minSize,
		buf:     &bytes.Buffer{},
	}, nil
}

// WrapReader wraps a reader to decompress data on read.
// It detects the zstd magic bytes to determine if decompression is needed.
func (c *Compress) WrapReader(_ context.Context, r io.ReadCloser, _ *driver.ObjectInfo) (io.ReadCloser, error) {
	return &decompressReader{inner: r}, nil
}

func (c *Compress) shouldSkip(key string) bool {
	ext := strings.ToLower(filepath.Ext(key))
	return c.skipExt[ext]
}

// compressWriter buffers data, then compresses on Close.
type compressWriter struct {
	inner   io.WriteCloser
	minSize int64
	buf     *bytes.Buffer
}

func (w *compressWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *compressWriter) Close() error {
	data := w.buf.Bytes()

	// Skip compression for small data.
	if int64(len(data)) < w.minSize {
		if _, err := w.inner.Write(data); err != nil {
			return err
		}
		return w.inner.Close()
	}

	// Compress with zstd.
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return fmt.Errorf("compress: new encoder: %w", err)
	}
	defer encoder.Close()

	compressed := encoder.EncodeAll(data, nil)

	// Only use compressed version if it's actually smaller.
	if len(compressed) >= len(data) {
		if _, err := w.inner.Write(data); err != nil {
			return err
		}
		return w.inner.Close()
	}

	if _, err := w.inner.Write(compressed); err != nil {
		return fmt.Errorf("compress: write: %w", err)
	}
	return w.inner.Close()
}

// decompressReader detects zstd magic bytes and decompresses if present.
type decompressReader struct {
	inner   io.ReadCloser
	reader  io.Reader
	decoded bool
}

func (r *decompressReader) Read(p []byte) (int, error) {
	if !r.decoded {
		if err := r.init(); err != nil {
			return 0, err
		}
	}
	return r.reader.Read(p)
}

func (r *decompressReader) init() error {
	data, err := io.ReadAll(r.inner)
	if err != nil {
		return fmt.Errorf("compress: read: %w", err)
	}
	r.decoded = true

	// Check for zstd magic bytes.
	if len(data) >= 4 && bytes.Equal(data[:4], zstdMagic) {
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return fmt.Errorf("compress: new decoder: %w", err)
		}
		defer decoder.Close()

		decompressed, err := decoder.DecodeAll(data, nil)
		if err != nil {
			return fmt.Errorf("compress: decompress: %w", err)
		}
		r.reader = bytes.NewReader(decompressed)
	} else {
		// Not compressed — pass through.
		r.reader = bytes.NewReader(data)
	}

	return nil
}

func (r *decompressReader) Close() error {
	return r.inner.Close()
}
