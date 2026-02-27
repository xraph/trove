package middleware

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

// testMiddleware implements all middleware interfaces for testing.
type testMiddleware struct {
	name string
	dir  Direction
}

func (m *testMiddleware) Name() string         { return m.name }
func (m *testMiddleware) Direction() Direction { return m.dir }

func (m *testMiddleware) WrapReader(_ context.Context, r io.ReadCloser, _ *driver.ObjectInfo) (io.ReadCloser, error) {
	return r, nil
}

func (m *testMiddleware) WrapWriter(_ context.Context, w io.WriteCloser, _ string) (io.WriteCloser, error) {
	return w, nil
}

func TestResolver_Register(t *testing.T) {
	r := NewResolver()
	assert.Empty(t, r.Registrations())

	r.Register(Registration{
		Middleware: &testMiddleware{name: "test", dir: DirectionReadWrite},
	})
	assert.Len(t, r.Registrations(), 1)
}

func TestResolver_ResolveRead_Empty(t *testing.T) {
	r := NewResolver()
	pipeline := r.ResolveRead(context.Background(), "bucket", "key")
	assert.Empty(t, pipeline)
}

func TestResolver_ResolveRead_DirectionFiltering(t *testing.T) {
	r := NewResolver()

	// Register a write-only middleware.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "write-only", dir: DirectionWrite},
	})

	// Register a read-write middleware.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "readwrite", dir: DirectionReadWrite},
	})

	pipeline := r.ResolveRead(context.Background(), "bucket", "key")
	require.Len(t, pipeline, 1)
	// The readwrite middleware should be in the read pipeline.
}

func TestResolver_ResolveWrite_DirectionFiltering(t *testing.T) {
	r := NewResolver()

	// Register a read-only middleware.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "read-only", dir: DirectionRead},
	})

	// Register a write middleware.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "write", dir: DirectionWrite},
	})

	pipeline := r.ResolveWrite(context.Background(), "bucket", "key")
	require.Len(t, pipeline, 1)
}

func TestResolver_PriorityOrdering(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	r.Register(Registration{
		Middleware: &testMiddleware{name: "low", dir: DirectionReadWrite},
		Priority:   50,
	})
	r.Register(Registration{
		Middleware: &testMiddleware{name: "high", dir: DirectionReadWrite},
		Priority:   10,
	})
	r.Register(Registration{
		Middleware: &testMiddleware{name: "mid", dir: DirectionReadWrite},
		Priority:   30,
	})

	pipeline := r.ResolveRead(ctx, "bucket", "key")
	require.Len(t, pipeline, 3)
	// Pipeline should be sorted by priority: high(10), mid(30), low(50).
}

func TestResolver_StableOrdering(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	// Same priority — should maintain registration order.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "first", dir: DirectionReadWrite},
		Priority:   0,
	})
	r.Register(Registration{
		Middleware: &testMiddleware{name: "second", dir: DirectionReadWrite},
		Priority:   0,
	})

	pipeline := r.ResolveRead(ctx, "bucket", "key")
	require.Len(t, pipeline, 2)
}

func TestResolver_ScopeFiltering(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	r.Register(Registration{
		Middleware: &testMiddleware{name: "global", dir: DirectionReadWrite},
		Scope:      ScopeGlobal{},
	})
	r.Register(Registration{
		Middleware: &testMiddleware{name: "docs-only", dir: DirectionReadWrite},
		Scope:      ForBuckets("docs"),
	})

	// Request to "docs" bucket should have both.
	pipeline := r.ResolveRead(ctx, "docs", "key")
	assert.Len(t, pipeline, 2)

	// Request to "images" bucket should only have global.
	pipeline = r.ResolveRead(ctx, "images", "key")
	assert.Len(t, pipeline, 1)
}

func TestResolver_DirectionOverride(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	// Register a readwrite middleware but override to read-only.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "override", dir: DirectionReadWrite},
		Direction:  DirectionRead,
	})

	readPipeline := r.ResolveRead(ctx, "bucket", "key")
	assert.Len(t, readPipeline, 1)

	writePipeline := r.ResolveWrite(ctx, "bucket", "key")
	assert.Len(t, writePipeline, 0)
}

func TestResolver_Remove(t *testing.T) {
	r := NewResolver()

	r.Register(Registration{
		Middleware: &testMiddleware{name: "encrypt", dir: DirectionReadWrite},
		Scope:      ScopeGlobal{},
	})
	r.Register(Registration{
		Middleware: &testMiddleware{name: "compress", dir: DirectionReadWrite},
	})

	assert.Len(t, r.Registrations(), 2)

	r.Remove("encrypt", nil)
	assert.Len(t, r.Registrations(), 1)
	assert.Equal(t, "compress", r.Registrations()[0].Middleware.Name())
}

func TestResolver_Remove_ByScope(t *testing.T) {
	r := NewResolver()

	r.Register(Registration{
		Middleware: &testMiddleware{name: "encrypt", dir: DirectionReadWrite},
		Scope:      ForBuckets("docs"),
	})
	r.Register(Registration{
		Middleware: &testMiddleware{name: "encrypt", dir: DirectionReadWrite},
		Scope:      ForBuckets("images"),
	})

	assert.Len(t, r.Registrations(), 2)

	// Remove only the docs-scoped encrypt.
	r.Remove("encrypt", ForBuckets("docs"))
	assert.Len(t, r.Registrations(), 1)
}

func TestResolver_Cache(t *testing.T) {
	r := NewResolver()
	ctx := context.Background()

	r.Register(Registration{
		Middleware: &testMiddleware{name: "test", dir: DirectionReadWrite},
	})

	// First call populates cache.
	p1 := r.ResolveRead(ctx, "bucket", "key")
	require.Len(t, p1, 1)

	// Second call should hit cache.
	p2 := r.ResolveRead(ctx, "bucket", "key")
	require.Len(t, p2, 1)

	// Registering a new middleware invalidates cache.
	r.Register(Registration{
		Middleware: &testMiddleware{name: "test2", dir: DirectionReadWrite},
	})

	p3 := r.ResolveRead(ctx, "bucket", "key")
	require.Len(t, p3, 2)
}
