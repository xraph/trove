// Package scan provides content scanning middleware for Trove.
// It scans uploaded content for threats using a pluggable Provider
// interface. When a threat is detected, the upload is blocked with
// trove.ErrContentBlocked.
package scan

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/xraph/trove"
	"github.com/xraph/trove/middleware"
)

// Compile-time interface checks.
var (
	_ middleware.Middleware      = (*Scan)(nil)
	_ middleware.WriteMiddleware = (*Scan)(nil)
)

// Provider defines the interface for content scanning backends.
type Provider interface {
	// Scan inspects the content and returns a result indicating whether
	// the content is clean or contains a threat.
	Scan(ctx context.Context, r io.Reader) (*Result, error)
}

// Result holds the outcome of a content scan.
type Result struct {
	// Clean is true if no threats were found.
	Clean bool

	// Threat is the name/identifier of the detected threat (empty if clean).
	Threat string

	// Details provides additional information about the scan.
	Details string
}

// OnDetectFunc is a callback invoked when a threat is detected.
type OnDetectFunc func(ctx context.Context, key string, result *Result)

// Option configures the Scan middleware.
type Option func(*Scan)

// WithProvider sets the scan provider.
func WithProvider(p Provider) Option {
	return func(s *Scan) { s.provider = p }
}

// WithOnDetect sets a callback for when threats are detected.
func WithOnDetect(fn OnDetectFunc) Option {
	return func(s *Scan) { s.onDetect = fn }
}

// WithMaxSize sets the maximum size of content to scan.
// Content larger than this is passed through without scanning.
// Default: 25MB.
func WithMaxSize(size int64) Option {
	return func(s *Scan) { s.maxSize = size }
}

// WithSkipExtensions sets file extensions to skip scanning.
func WithSkipExtensions(exts ...string) Option {
	return func(s *Scan) {
		for _, ext := range exts {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			s.skipExt[ext] = true
		}
	}
}

// Scan provides content scanning middleware for uploads.
type Scan struct {
	provider Provider
	onDetect OnDetectFunc
	maxSize  int64
	skipExt  map[string]bool
}

// New creates a new scan middleware with the given options.
func New(opts ...Option) *Scan {
	s := &Scan{
		maxSize: 25 * 1024 * 1024, // 25MB default
		skipExt: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name returns the middleware identifier.
func (s *Scan) Name() string { return "scan" }

// Direction returns Write since scanning only applies to uploads.
func (s *Scan) Direction() middleware.Direction { return middleware.DirectionWrite }

// WrapWriter wraps a writer to scan content before it reaches the driver.
// Data is buffered, scanned on Close(), and blocked with ErrContentBlocked
// if a threat is detected.
func (s *Scan) WrapWriter(ctx context.Context, w io.WriteCloser, key string) (io.WriteCloser, error) {
	if s.provider == nil {
		return w, nil
	}

	// Skip based on file extension.
	if s.shouldSkip(key) {
		return w, nil
	}

	return &scanWriter{
		ctx:   ctx,
		inner: w,
		scan:  s,
		key:   key,
		buf:   &bytes.Buffer{},
	}, nil
}

func (s *Scan) shouldSkip(key string) bool {
	ext := strings.ToLower(filepath.Ext(key))
	return s.skipExt[ext]
}

// scanWriter buffers data and scans on Close.
type scanWriter struct {
	ctx   context.Context
	inner io.WriteCloser
	scan  *Scan
	key   string
	buf   *bytes.Buffer
}

func (w *scanWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *scanWriter) Close() error {
	data := w.buf.Bytes()

	// Skip scanning if content exceeds max size.
	if w.scan.maxSize > 0 && int64(len(data)) > w.scan.maxSize {
		if _, err := w.inner.Write(data); err != nil {
			return err
		}
		return w.inner.Close()
	}

	// Scan the buffered content.
	result, err := w.scan.provider.Scan(w.ctx, bytes.NewReader(data))
	if err != nil {
		return err
	}

	if !result.Clean {
		// Invoke callback if set.
		if w.scan.onDetect != nil {
			w.scan.onDetect(w.ctx, w.key, result)
		}
		return trove.ErrContentBlocked
	}

	// Content is clean — write through.
	if _, err := w.inner.Write(data); err != nil {
		return err
	}
	return w.inner.Close()
}
