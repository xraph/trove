package trove_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/internal/telemetrytest"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/middleware/compress"
	"github.com/xraph/trove/observe"
	"github.com/xraph/trove/trovetest"
)

// Values that must never reach a span: a tenant-named bucket and a key that
// carries an email address.
const (
	secretBucket = "tenant-alice-co"
	secretKey    = "users/alice@example.com/passport.pdf"
)

// baggageSpy wraps a driver and remembers whether any call it served saw
// baggage on its context, and whether every call ran under a valid span.
type baggageSpy struct {
	driver.Driver

	mu          sync.Mutex
	sawBaggage  bool
	sawNoParent bool
}

func (s *baggageSpy) note(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if baggage.FromContext(ctx).Len() > 0 {
		s.sawBaggage = true
	}
	if !trace.SpanContextFromContext(ctx).IsValid() {
		s.sawNoParent = true
	}
}

func (s *baggageSpy) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	s.note(ctx)
	return s.Driver.Put(ctx, bucket, key, r, opts...)
}

func (s *baggageSpy) Get(ctx context.Context, bucket, key string, opts ...driver.GetOption) (*driver.ObjectReader, error) {
	s.note(ctx)
	return s.Driver.Get(ctx, bucket, key, opts...)
}

func (s *baggageSpy) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	s.note(ctx)
	return s.Driver.Head(ctx, bucket, key)
}

func (s *baggageSpy) Delete(ctx context.Context, bucket, key string, opts ...driver.DeleteOption) error {
	s.note(ctx)
	return s.Driver.Delete(ctx, bucket, key, opts...)
}

func (s *baggageSpy) CreateBucket(ctx context.Context, name string, opts ...driver.BucketOption) error {
	s.note(ctx)
	return s.Driver.CreateBucket(ctx, name, opts...)
}

func withBaggage(t *testing.T) context.Context {
	t.Helper()
	m, err := baggage.NewMemberRaw("user.email", "alice@example.com")
	require.NoError(t, err)
	b, err := baggage.New(m)
	require.NoError(t, err)
	return baggage.ContextWithBaggage(context.Background(), b)
}

func TestFacadeSpansAndMetrics(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)

	spy := &baggageSpy{Driver: memdriver.New()}
	tr, err := trove.Open(spy, trove.WithMetrics(metrics))
	require.NoError(t, err)

	parentCtx, parent := otel.Tracer("test").Start(withBaggage(t), "handle request")

	require.NoError(t, tr.CreateBucket(parentCtx, secretBucket))
	assert.ErrorIs(t, tr.CreateBucket(parentCtx, secretBucket), trove.ErrBucketExists)

	_, err = tr.Put(parentCtx, secretBucket, secretKey, strings.NewReader("hello"))
	require.NoError(t, err)

	obj, err := tr.Get(parentCtx, secretBucket, secretKey)
	require.NoError(t, err)
	require.NoError(t, obj.Close())

	_, err = tr.Head(parentCtx, secretBucket, secretKey)
	require.NoError(t, err)

	_, err = tr.Head(parentCtx, secretBucket, "missing/"+secretKey)
	assert.ErrorIs(t, err, trove.ErrObjectNotFound)

	_, err = tr.Copy(parentCtx, secretBucket, secretKey, secretBucket, secretKey+".bak")
	require.NoError(t, err)

	it, err := tr.List(parentCtx, secretBucket)
	require.NoError(t, err)
	require.NoError(t, it.Close())

	require.NoError(t, tr.Delete(parentCtx, secretBucket, secretKey))

	_, err = tr.ListBuckets(parentCtx)
	require.NoError(t, err)
	parent.End()

	// The driver never saw the caller's baggage, and always ran under the
	// operation's span.
	assert.False(t, spy.sawBaggage, "a driver call carried the caller's baggage")
	assert.False(t, spy.sawNoParent, "a driver call had no span")

	want := map[string]string{
		"create bucket": observe.OperationCreateBucket,
		"put object":    observe.OperationPut,
		"get object":    observe.OperationGet,
		"head object":   observe.OperationHead,
		"copy object":   observe.OperationCopy,
		"list objects":  observe.OperationList,
		"delete object": observe.OperationDelete,
		"list buckets":  observe.OperationListBuckets,
	}
	for name, operation := range want {
		spans := telemetrytest.Named(rec, name)
		require.NotEmpty(t, spans, "no %q span", name)
		for _, s := range spans {
			assert.Equal(t, trace.SpanKindInternal, s.SpanKind(), "%s kind", name)
			assert.Equal(t, parent.SpanContext().SpanID(), s.Parent().SpanID(), "%s is not a child of the caller", name)
			assert.Equal(t, "github.com/xraph/trove", s.InstrumentationScope().Name)
			v, _ := telemetrytest.Attr(s, "trove.driver")
			assert.Equal(t, observe.DriverMem, v, name)
			v, _ = telemetrytest.Attr(s, "trove.operation")
			assert.Equal(t, operation, v, name)
		}
	}

	put := telemetrytest.Named(rec, "put object")[0]
	size, ok := telemetrytest.Attr(put, "trove.object.size")
	assert.True(t, ok)
	assert.Equal(t, "5", size)

	// A missing object and an existing bucket are answers, not failures.
	heads := telemetrytest.Named(rec, "head object")
	require.Len(t, heads, 2)
	result, _ := telemetrytest.Attr(heads[1], "trove.result")
	assert.Equal(t, observe.ResultNotFound, result)
	assert.Equal(t, codes.Unset, heads[1].Status().Code)
	creates := telemetrytest.Named(rec, "create bucket")
	require.Len(t, creates, 2)
	result, _ = telemetrytest.Attr(creates[1], "trove.result")
	assert.Equal(t, observe.ResultExists, result)
	assert.Equal(t, codes.Unset, creates[1].Status().Code)

	for _, v := range []string{secretBucket, secretKey, "alice@example.com"} {
		assert.Empty(t, telemetrytest.Leaks(rec, v), "%q reached a span", v)
	}

	// Metrics: one duration per call, labelled by driver, operation and
	// result, and bytes through Put and Get.
	d := observe.MetricOperationDuration
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationPut, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationGet, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationHead, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationHead, observe.ResultNotFound))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationCreateBucket, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationCreateBucket, observe.ResultExists))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationCopy, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationList, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationDelete, observe.ResultOK))
	assert.Equal(t, 1, metrics.Count(d, observe.DriverMem, observe.OperationListBuckets, observe.ResultOK))
	assert.InDelta(t, 5, metrics.Counter(observe.MetricObjectBytes, observe.DriverMem, observe.DirectionWrite), 0)
	assert.InDelta(t, 5, metrics.Counter(observe.MetricObjectBytes, observe.DriverMem, observe.DirectionRead), 0)
}

func TestFacadeFailureRecordsOnlyTheClass(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)

	// A remote-style driver whose errors quote the bucket and key, the way
	// the real drivers' do.
	drv := &trovetest.MockDriver{
		NameFunc: func() string { return "s3" },
		HeadFunc: func(_ context.Context, bucket, key string) (*driver.ObjectInfo, error) {
			return nil, fmt.Errorf("s3driver: head %q in %q: %w", key, bucket, driver.ErrPermissionDenied)
		},
		GetFunc: func(_ context.Context, bucket, key string, _ ...driver.GetOption) (*driver.ObjectReader, error) {
			return nil, fmt.Errorf("s3driver: get %q in %q: connection reset", key, bucket)
		},
	}
	tr, err := trove.Open(drv, trove.WithMetrics(metrics))
	require.NoError(t, err)

	_, err = tr.Head(t.Context(), secretBucket, secretKey)
	require.ErrorIs(t, err, trove.ErrPermissionDenied)
	_, err = tr.Get(t.Context(), secretBucket, secretKey)
	require.Error(t, err)

	head := telemetrytest.Named(rec, "head object")[0]
	assert.Equal(t, trace.SpanKindClient, head.SpanKind())
	assert.Equal(t, codes.Error, head.Status().Code)
	assert.Equal(t, "trove.ErrPermissionDenied", head.Status().Description)
	require.Len(t, head.Events(), 1)
	assert.Equal(t, "exception", head.Events()[0].Name)
	require.Len(t, head.Events()[0].Attributes, 1)
	assert.Equal(t, "exception.type", string(head.Events()[0].Attributes[0].Key))
	assert.Equal(t, "trove.ErrPermissionDenied", head.Events()[0].Attributes[0].Value.AsString())

	get := telemetrytest.Named(rec, "get object")[0]
	assert.Equal(t, "*errors.errorString", get.Status().Description)

	for _, v := range []string{secretBucket, secretKey} {
		assert.Empty(t, telemetrytest.Leaks(rec, v), "%q reached a span", v)
	}

	assert.Equal(t, 1, metrics.Count(observe.MetricOperationDuration, observe.DriverS3, observe.OperationHead, observe.ResultError))
	assert.Equal(t, 1, metrics.Count(observe.MetricOperationDuration, observe.DriverS3, observe.OperationGet, observe.ResultError))
}

func TestDriverLabelIsBounded(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)

	drv := &trovetest.MockDriver{NameFunc: func() string { return "acme-" + secretBucket }}
	tr, err := trove.Open(drv, trove.WithMetrics(metrics))
	require.NoError(t, err)
	_, err = tr.Put(t.Context(), secretBucket, secretKey, strings.NewReader("x"))
	require.NoError(t, err)

	put := telemetrytest.Named(rec, "put object")[0]
	assert.Equal(t, trace.SpanKindClient, put.SpanKind())
	v, _ := telemetrytest.Attr(put, "trove.driver")
	assert.Equal(t, observe.DriverOther, v)
	assert.Equal(t, 1, metrics.Count(observe.MetricOperationDuration, observe.DriverOther, observe.OperationPut, observe.ResultOK))
	assert.Empty(t, telemetrytest.Leaks(rec, secretBucket))
}

// passthrough is a write middleware that hands the driver's pipe straight
// to the copy, so the pipeline writes as fast as the driver reads.
type passthrough struct{}

func (passthrough) Name() string                    { return "passthrough" }
func (passthrough) Direction() middleware.Direction { return middleware.DirectionWrite }
func (passthrough) WrapWriter(_ context.Context, w io.WriteCloser, _ string) (io.WriteCloser, error) {
	return w, nil
}

func TestWriteMiddlewareSpan(t *testing.T) {
	rec, ended := telemetrytest.SpansWithEnds(t)

	drv := memdriver.New()
	tr, err := trove.Open(drv, trove.WithMiddleware(compress.New()))
	require.NoError(t, err)
	require.NoError(t, tr.CreateBucket(t.Context(), "b"))
	_, err = tr.Put(t.Context(), "b", secretKey, strings.NewReader(strings.Repeat("a", 4096)))
	require.NoError(t, err)

	telemetrytest.WaitEnded(t, ended, "apply write middleware")
	mw := telemetrytest.Named(rec, "apply write middleware")
	require.Len(t, mw, 1)
	put := telemetrytest.Named(rec, "put object")[0]
	assert.Equal(t, put.SpanContext().SpanID(), mw[0].Parent().SpanID())
	count, _ := telemetrytest.Attr(mw[0], "trove.middleware.count")
	assert.Equal(t, "1", count)
	names, _ := telemetrytest.Attr(mw[0], "trove.middleware.names")
	assert.Equal(t, `["compress"]`, names)
	assert.Equal(t, codes.Unset, mw[0].Status().Code)
}

// TestWritePipelineEndsWhenTheDriverStopsReading checks that a driver that
// fails without draining the reader no longer strands the pipeline
// goroutine: its span ends once Put returns.
func TestWritePipelineEndsWhenTheDriverStopsReading(t *testing.T) {
	rec, ended := telemetrytest.SpansWithEnds(t)

	drv := &trovetest.MockDriver{
		PutFunc: func(_ context.Context, _, _ string, r io.Reader, _ ...driver.PutOption) (*driver.ObjectInfo, error) {
			buf := make([]byte, 1)
			if _, err := r.Read(buf); err != nil {
				return nil, err
			}
			return nil, driver.ErrQuotaExceeded
		},
	}
	tr, err := trove.Open(drv, trove.WithWriteMiddleware(passthrough{}))
	require.NoError(t, err)

	// The pipeline writes all of it in one call, and the driver reads one
	// byte before it gives up.
	_, err = tr.Put(t.Context(), "b", "k", strings.NewReader(strings.Repeat("abcdefghij", 1<<16)))
	require.ErrorIs(t, err, trove.ErrQuotaExceeded)

	telemetrytest.WaitEnded(t, ended, "apply write middleware")
	mw := telemetrytest.Named(rec, "apply write middleware")[0]
	assert.Equal(t, codes.Error, mw.Status().Code)
	assert.Equal(t, "io.ErrClosedPipe", mw.Status().Description)
}

func TestCASSpansAndMetrics(t *testing.T) {
	rec := telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)

	spy := &baggageSpy{Driver: memdriver.New()}
	// WithMetrics after WithCAS: the CAS still records on it.
	tr, err := trove.Open(spy, trove.WithCAS(0), trove.WithMetrics(metrics))
	require.NoError(t, err)
	require.NoError(t, tr.CreateBucket(t.Context(), "cas"))

	c := tr.CAS()
	hash, _, err := c.Store(withBaggage(t), strings.NewReader("same content"))
	require.NoError(t, err)
	_, _, err = c.Store(t.Context(), strings.NewReader("same content"))
	require.NoError(t, err)

	stores := telemetrytest.Named(rec, "store cas object")
	require.Len(t, stores, 2)
	for i, wantDedup := range []string{"false", "true"} {
		v, _ := telemetrytest.Attr(stores[i], "trove.cas.deduplicated")
		assert.Equal(t, wantDedup, v)
		v, _ = telemetrytest.Attr(stores[i], "trove.cas.algorithm")
		assert.Equal(t, "sha256", v)
		v, _ = telemetrytest.Attr(stores[i], "trove.object.size")
		assert.Equal(t, "12", v)
		assert.Equal(t, trace.SpanKindInternal, stores[i].SpanKind())
	}
	assert.Empty(t, telemetrytest.Leaks(rec, hash), "the content hash reached a span")
	assert.InDelta(t, 1, metrics.Counter(observe.MetricCASStores, observe.CASStored), 0)
	assert.InDelta(t, 1, metrics.Counter(observe.MetricCASStores, observe.CASDeduplicated), 0)
	assert.False(t, spy.sawBaggage, "the CAS put carried the caller's baggage")
	assert.False(t, spy.sawNoParent, "the CAS put ran without a span")
}

// TestNoRecorderRecordsNothing checks the default: no WithMetrics, no
// panics, spans still recorded.
func TestNoRecorderRecordsNothing(t *testing.T) {
	rec := telemetrytest.Spans(t)
	tr, err := trove.Open(memdriver.New(), trove.WithCAS(0))
	require.NoError(t, err)
	require.NoError(t, tr.CreateBucket(t.Context(), "cas"))
	_, _, err = tr.CAS().Store(t.Context(), strings.NewReader("x"))
	require.NoError(t, err)
	assert.Len(t, telemetrytest.Named(rec, "store cas object"), 1)
}
