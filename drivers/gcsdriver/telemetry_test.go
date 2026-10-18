package gcsdriver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	leakBucket = "tenant-acme-bucket"
	leakKey    = "reports/alice-salary.pdf"
	leakEmail  = "alice@example.com"
)

// fakeGCS answers uploads with an object resource and every other call with
// a 403 whose message quotes the bucket, the object and an account, the way
// Google's does. It remembers the trace headers each request carried.
type fakeGCS struct {
	mu      sync.Mutex
	headers []http.Header
}

func (f *fakeGCS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.headers = append(f.headers, r.Header.Clone())
	f.mu.Unlock()

	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/upload/") {
		fmt.Fprintf(w, `{"bucket":%q,"name":%q,"size":"5","contentType":"text/plain","etag":"e1"}`, leakBucket, leakKey)
		return
	}

	msg := fmt.Sprintf("%s does not have storage.objects.get access to the Google Cloud Storage object %s/%s.", leakEmail, leakBucket, leakKey)
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"error":{"code":403,"message":%q,"errors":[{"message":%q,"domain":"global","reason":"forbidden"}]}}`, msg, msg)
}

func (f *fakeGCS) seen() []http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]http.Header(nil), f.headers...)
}

// installTracing makes a recording, parent-based provider and forge's
// default propagators global for the test, which is what a Forge app has.
func installTracing(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

// TestLibraryTelemetryCarriesNoObjectPaths drives the driver through a
// successful upload and failing reads, deletes and lookups, with and
// without a caller span, and checks that the storage library neither
// records a span that names the bucket, the object or the account, nor
// sends trace context or the caller's baggage to Google.
func TestLibraryTelemetryCarriesNoObjectPaths(t *testing.T) {
	rec := installTracing(t)

	fake := &fakeGCS{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	drv := New()
	require.NoError(t, drv.Open(context.Background(), "gcs://proj/"+leakBucket+"?endpoint="+srv.URL+"/storage/v1/"))
	t.Cleanup(func() { _ = drv.Close(context.Background()) })

	member, err := baggage.NewMemberRaw("user.email", leakEmail)
	require.NoError(t, err)
	bag, err := baggage.New(member)
	require.NoError(t, err)
	base := baggage.ContextWithBaggage(context.Background(), bag)

	parentCtx, parent := otel.Tracer("test").Start(base, "handle request")

	for _, ctx := range []context.Context{parentCtx, base} {
		_, err := drv.Put(ctx, leakBucket, leakKey, strings.NewReader("hello"))
		require.NoError(t, err)

		_, err = drv.Head(ctx, leakBucket, leakKey)
		require.Error(t, err)

		_, err = drv.Get(ctx, leakBucket, leakKey)
		require.Error(t, err)

		require.Error(t, drv.Delete(ctx, leakBucket, leakKey))
	}
	parent.End()

	headers := fake.seen()
	require.NotEmpty(t, headers)
	for i, h := range headers {
		assert.Empty(t, h.Get("Baggage"), "request %d carried baggage", i)
		assert.Empty(t, h.Get("Traceparent"), "request %d carried a traceparent", i)
	}

	for _, s := range rec.Ended() {
		assert.Equal(t, "test", s.InstrumentationScope().Name, "span %q from %s was recorded", s.Name(), s.InstrumentationScope().Name)
		for _, v := range []string{leakBucket, leakKey, leakEmail} {
			assert.NotContains(t, s.Name(), v)
			assert.NotContains(t, s.Status().Description, v)
			for _, kv := range s.Attributes() {
				assert.NotContains(t, kv.Value.String(), v, "span %q attribute %s", s.Name(), kv.Key)
			}
			for _, ev := range s.Events() {
				for _, kv := range ev.Attributes {
					assert.NotContains(t, kv.Value.String(), v, "span %q event %s attribute %s", s.Name(), ev.Name, kv.Key)
				}
			}
		}
	}
}

func TestUntracedKeepsTheCallerTrace(t *testing.T) {
	installTracing(t)

	ctx, span := otel.Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	sc := spanContextOf(untraced(ctx))
	assert.Equal(t, span.SpanContext().TraceID(), sc.TraceID())
	assert.Equal(t, span.SpanContext().SpanID(), sc.SpanID())
	assert.False(t, sc.IsSampled())

	bare := spanContextOf(untraced(context.Background()))
	assert.True(t, bare.IsValid())
	assert.False(t, bare.IsSampled())
}

func spanContextOf(ctx context.Context) trace.SpanContext {
	return trace.SpanContextFromContext(ctx)
}
