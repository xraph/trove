package cas

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/internal/telemetrytest"
	"github.com/xraph/trove/observe"
)

// failingDelete is a driver whose Delete fails, quoting the bucket and key.
type failingDelete struct{ driver.Driver }

func (failingDelete) Delete(_ context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	return errors.New("delete " + bucket + "/" + key + ": refused")
}

func TestGCSpanAndMetrics(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)
	ctx := t.Context()

	drv := memdriver.New()
	require.NoError(t, drv.CreateBucket(ctx, "cas"))
	idx := NewMemoryIndex()
	c := New(drv, WithIndex(idx), WithMetrics(metrics))

	hashes := make([]string, 0, 2)
	for _, content := range []string{"first", "second blob"} {
		hash, _, err := c.Store(ctx, strings.NewReader(content))
		require.NoError(t, err)
		require.NoError(t, idx.DecrementRef(ctx, hash))
		hashes = append(hashes, hash)
	}

	res, err := c.GC(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, res.Deleted)

	spans := telemetrytest.Named(rec, "collect cas garbage")
	require.Len(t, spans, 1)
	for key, want := range map[string]string{
		"trove.cas.gc.scanned":     "2",
		"trove.cas.gc.deleted":     "2",
		"trove.cas.gc.freed_bytes": "16",
		"trove.cas.gc.errors":      "0",
		"trove.cas.algorithm":      "sha256",
		"trove.driver":             "mem",
	} {
		v, _ := telemetrytest.Attr(spans[0], key)
		assert.Equal(t, want, v, key)
	}
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
	assert.InDelta(t, 2, metrics.Counter(observe.MetricCASGCDeleted), 0)
	assert.InDelta(t, 16, metrics.Counter(observe.MetricCASGCFreedBytes), 0)
	assert.InDelta(t, 2, metrics.Counter(observe.MetricCASStores, observe.CASStored), 0)
	for _, h := range hashes {
		assert.Empty(t, telemetrytest.Leaks(rec, h), "a content hash reached a span")
	}
}

func TestGCPartialFailureIsCountedNotFailed(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)
	ctx := t.Context()

	mem := memdriver.New()
	require.NoError(t, mem.CreateBucket(ctx, "cas"))
	idx := NewMemoryIndex()
	c := New(failingDelete{mem}, WithIndex(idx), WithMetrics(metrics))

	hash, _, err := c.Store(ctx, strings.NewReader("stuck"))
	require.NoError(t, err)
	require.NoError(t, idx.DecrementRef(ctx, hash))

	res, err := c.GC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Errors)

	span := telemetrytest.Named(rec, "collect cas garbage")[0]
	v, _ := telemetrytest.Attr(span, "trove.cas.gc.errors")
	assert.Equal(t, "1", v)
	assert.Equal(t, codes.Unset, span.Status().Code)
	assert.InDelta(t, 0, metrics.Counter(observe.MetricCASGCDeleted), 0)
	assert.Empty(t, telemetrytest.Leaks(rec, hash))
}

// failingIndex fails every lookup and write, quoting the hash.
type failingIndex struct{ Index }

func (failingIndex) Get(_ context.Context, hash string) (*Entry, error) {
	return nil, errors.New("index get " + hash + ": down")
}

func (failingIndex) Put(_ context.Context, e *Entry) error {
	return errors.New("index put " + e.Hash + ": down")
}

func TestStoreFailureRecordsOnlyTheClass(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)
	ctx := t.Context()

	mem := memdriver.New()
	require.NoError(t, mem.CreateBucket(ctx, "cas"))
	c := New(mem, WithIndex(failingIndex{NewMemoryIndex()}), WithMetrics(metrics))

	_, _, err := c.Store(ctx, strings.NewReader("content"))
	require.Error(t, err)

	span := telemetrytest.Named(rec, "store cas object")[0]
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "*errors.errorString", span.Status().Description)
	assert.InDelta(t, 1, metrics.Counter(observe.MetricCASStores, observe.CASError), 0)

	hash, err := computeHashBytes([]byte("content"), AlgSHA256)
	require.NoError(t, err)
	assert.Empty(t, telemetrytest.Leaks(rec, hash))
}
