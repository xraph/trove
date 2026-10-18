package gcsdriver

import (
	"context"
	"crypto/rand"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// untraced prepares ctx for a call into the storage library, which would
// otherwise put bucket and object names on spans and send the caller's
// baggage to Google.
//
// Open already passes option.WithTelemetryDisabled, which keeps the library
// from wrapping its transport in otelhttp: no client span recording url.full
// (the bucket and object path), and no traceparent or baggage header on the
// request. That option does not reach the library's own spans. It opens one
// per call (cloud.google.com/go/storage.Object.Attrs and the like) from the
// global tracer provider whatever the client options say, and when a call
// fails it sets the span's status to Google's error message and records the
// error as an exception event. Those messages quote the bucket and object
// ("No such object: bucket/key") and sometimes the caller's account.
//
// So the library gets a span context that is valid but not sampled, and a
// parent-based sampler (forge's default and OpenTelemetry's) decides not to
// sample the spans started under it. The caller's span is reused when there
// is one, so cancellation, values and the trace ID carry over; a call with no
// span gets a random unsampled one. Baggage is dropped as well: none of it is
// Google's. trove's own span for the operation, started by the Trove facade
// without names, stays the client span you see.
//
// Not sampled is not the same as not recorded, and two cases still record
// the library's spans, error text included:
//
//   - A sampler that ignores the parent (always_on, or a bare traceidratio)
//     samples them, and they are exported like any other span.
//   - forge v1.12.9's sampler turns a Drop into RecordOnly while any span
//     processor wants unsampled spans, and the dashboard's trace collector
//     does whenever the dashboard extension is collecting. The spans are then
//     recorded and handed to every processor. Batch exporters skip unsampled
//     spans, so nothing goes out over OTLP, but the dashboard's Traces view
//     stores them, with Google's message (bucket, object, sometimes the
//     account) as the status and an exception event.
//
// Neither has a fix here. The library starts its spans from the global
// tracer provider, not from anything the driver can hand it.
func untraced(ctx context.Context) context.Context {
	ctx = baggage.ContextWithoutBaggage(ctx)

	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		var cfg trace.SpanContextConfig
		if _, err := rand.Read(cfg.TraceID[:]); err != nil {
			return ctx
		}
		if _, err := rand.Read(cfg.SpanID[:]); err != nil {
			return ctx
		}
		sc = trace.NewSpanContext(cfg)
	}

	return trace.ContextWithSpanContext(ctx, sc.WithTraceFlags(sc.TraceFlags().WithSampled(false)))
}
