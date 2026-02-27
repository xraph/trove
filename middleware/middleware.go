// Package middleware provides a composable, scope-aware interceptor pipeline
// for Trove's read/write data path. Middleware wraps the data stream — not
// the HTTP request — so it works identically via REST, WebSocket, or Go API.
package middleware

import (
	"context"
	"io"

	"github.com/xraph/trove/driver"
)

// Direction controls which data paths a middleware participates in.
type Direction int

const (
	// DirectionRead indicates the middleware participates in downloads/gets.
	DirectionRead Direction = 1 << iota

	// DirectionWrite indicates the middleware participates in uploads/puts.
	DirectionWrite

	// DirectionReadWrite indicates the middleware participates in both paths.
	DirectionReadWrite = DirectionRead | DirectionWrite
)

// String returns a human-readable direction name.
func (d Direction) String() string {
	switch d {
	case DirectionRead:
		return "read"
	case DirectionWrite:
		return "write"
	case DirectionReadWrite:
		return "readwrite"
	default:
		return "unknown"
	}
}

// Middleware is the base interface every middleware implements.
// A concrete middleware implements ReadMiddleware, WriteMiddleware, or both.
type Middleware interface {
	// Name returns the middleware identifier (e.g., "encrypt", "compress").
	Name() string

	// Direction returns which data paths this middleware participates in.
	Direction() Direction
}

// ReadMiddleware intercepts data being read (downloaded).
type ReadMiddleware interface {
	// WrapReader wraps a reader on the download path. The info parameter
	// provides metadata about the object being read.
	WrapReader(ctx context.Context, r io.ReadCloser, info *driver.ObjectInfo) (io.ReadCloser, error)
}

// WriteMiddleware intercepts data being written (uploaded).
type WriteMiddleware interface {
	// WrapWriter wraps a writer on the upload path. The key parameter
	// identifies the object being written.
	WrapWriter(ctx context.Context, w io.WriteCloser, key string) (io.WriteCloser, error)
}
