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

// installTracing makes a recording provider with the given sampler, and
// forge's default propagators, global for the test. A nil sampler keeps the
// SDK default, ParentBased(AlwaysSample), which is what forge's default
// sampler does for a sampled parent and an unsampled one.
func installTracing(t *testing.T, sampler sdktrace.Sampler) *tracetest.SpanRecorder {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	rec := tracetest.NewSpanRecorder()
	opts := []sdktrace.TracerProviderOption{sdktrace.WithSpanProcessor(rec)}
	if sampler != nil {
		opts = append(opts, sdktrace.WithSampler(sampler))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

// driveGCS opens the driver against a fake GCS server and runs a successful
// upload and failing lookups, reads and deletes, once under a caller span and
// once with no span, both carrying baggage. It returns the fake, which
// remembers each request's headers.
func driveGCS(t *testing.T) *fakeGCS {
	t.Helper()
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
	defer parent.End()

	for _, ctx := range []context.Context{parentCtx, base} {
		_, err := drv.Put(ctx, leakBucket, leakKey, strings.NewReader("hello"))
		require.NoError(t, err)

		_, err = drv.Head(ctx, leakBucket, leakKey)
		require.Error(t, err)

		_, err = drv.Get(ctx, leakBucket, leakKey)
		require.Error(t, err)

		require.Error(t, drv.Delete(ctx, leakBucket, leakKey))
	}
	return fake
}

// librarySpans returns the recorded spans the storage library started.
func librarySpans(rec *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.InstrumentationScope().Name != "test" {
			out = append(out, s)
		}
	}
	return out
}

// leaks reports whether any recorded span names the bucket, the object or
// the account anywhere: name, attributes, events or status.
func leaks(rec *tracetest.SpanRecorder) []string {
	var out []string
	for _, s := range rec.Ended() {
		for _, v := range []string{leakBucket, leakKey, leakEmail} {
			if strings.Contains(s.Name(), v) || strings.Contains(s.Status().Description, v) {
				out = append(out, s.Name()+": "+v)
			}
			for _, kv := range s.Attributes() {
				if strings.Contains(kv.Value.String(), v) {
					out = append(out, s.Name()+" attribute "+string(kv.Key)+": "+v)
				}
			}
			for _, ev := range s.Events() {
				for _, kv := range ev.Attributes {
					if strings.Contains(kv.Value.String(), v) {
						out = append(out, s.Name()+" event "+ev.Name+": "+v)
					}
				}
			}
		}
	}
	return out
}

// TestLibraryTelemetryCarriesNoObjectPaths checks that under a parent-based
// sampler the storage library records no span at all, and that no request
// carries trace context or the caller's baggage to Google.
func TestLibraryTelemetryCarriesNoObjectPaths(t *testing.T) {
	rec := installTracing(t, nil)
	fake := driveGCS(t)

	headers := fake.seen()
	require.NotEmpty(t, headers)
	for i, h := range headers {
		assert.Empty(t, h.Get("Baggage"), "request %d carried baggage", i)
		assert.Empty(t, h.Get("Traceparent"), "request %d carried a traceparent", i)
	}

	for _, s := range librarySpans(rec) {
		t.Errorf("span %q from %s was recorded", s.Name(), s.InstrumentationScope().Name)
	}
	assert.Empty(t, leaks(rec))
}

// TestLibrarySpansUnderSamplersThatRecordThem is the control for the test
// above, so it can't pass by the library simply opening no spans. Under a
// sampler that ignores the parent the library's spans are sampled, and they
// carry Google's error text. Under a sampler that records what it would
// drop, as forge's does while the dashboard collects traces, they are
// recorded too, unsampled. This is the residual the driver can't close: the
// library starts its spans from the global provider. Requests still carry
// no trace context or baggage in either case.
func TestLibrarySpansUnderSamplersThatRecordThem(t *testing.T) {
	cases := []struct {
		name    string
		sampler sdktrace.Sampler
		sampled bool
	}{
		{"always_on", sdktrace.AlwaysSample(), true},
		{"record only, like forge with the dashboard collecting", recordOnly{sdktrace.ParentBased(sdktrace.AlwaysSample())}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := installTracing(t, tc.sampler)
			fake := driveGCS(t)

			lib := librarySpans(rec)
			require.NotEmpty(t, lib, "the storage library opened no spans")
			for _, s := range lib {
				assert.Equal(t, tc.sampled, s.SpanContext().IsSampled(), s.Name())
			}
			assert.NotEmpty(t, leaks(rec), "the library's failed spans no longer quote Google's error")

			for i, h := range fake.seen() {
				assert.Empty(t, h.Get("Baggage"), "request %d carried baggage", i)
				assert.Empty(t, h.Get("Traceparent"), "request %d carried a traceparent", i)
			}
		})
	}
}

// recordOnly turns its inner sampler's Drop into RecordOnly, which is what
// forge v1.12.9's sampler does while a span processor wants unsampled spans.
type recordOnly struct{ inner sdktrace.Sampler }

func (r recordOnly) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	res := r.inner.ShouldSample(p)
	if res.Decision == sdktrace.Drop {
		res.Decision = sdktrace.RecordOnly
	}
	return res
}

func (r recordOnly) Description() string { return "recordOnly{" + r.inner.Description() + "}" }

func TestUntracedKeepsTheCallerTrace(t *testing.T) {
	installTracing(t, nil)

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
