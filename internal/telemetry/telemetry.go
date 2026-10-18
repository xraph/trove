// Package telemetry is the tracing and metrics plumbing Trove's packages
// share: the tracer every span comes from, span names and attribute keys,
// how a failed span is marked, and how a measurement reaches the caller's
// observe.Recorder.
//
// Spans go to the global OpenTelemetry tracer provider, which forge.New
// installs. Metrics go to the Recorder the caller hands in, because the root
// module depends on no metrics framework and forge installs no global
// MeterProvider for otel.Meter to reach.
//
// Nothing here puts an object key, a bucket name, a content hash, content, a
// signed URL, a DSN, a credential or error text on a span or a metric.
// Attributes are counts, sizes, booleans and enum strings. Metric labels are
// the fixed enums in package observe.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/observe"
)

// InstrumentationName is the tracer name on every span Trove starts.
const InstrumentationName = "github.com/xraph/trove"

// Span names. Each is a verb and an object, with no IDs, keys or bucket
// names in it.
const (
	SpanPutObject            = "put object"
	SpanGetObject            = "get object"
	SpanDeleteObject         = "delete object"
	SpanHeadObject           = "head object"
	SpanListObjects          = "list objects"
	SpanCopyObject           = "copy object"
	SpanCreateBucket         = "create bucket"
	SpanDeleteBucket         = "delete bucket"
	SpanListBuckets          = "list buckets"
	SpanApplyWriteMiddleware = "apply write middleware"
	SpanScanObject           = "scan object"
	SpanStoreCASObject       = "store cas object"
	SpanCollectCASGarbage    = "collect cas garbage"
)

// Attribute keys. Every one is namespaced under trove.
const (
	// AttrDriver is the driver that served the call, one of the observe
	// Driver values.
	AttrDriver = attribute.Key("trove.driver")
	// AttrOperation is the facade operation, one of the observe Operation
	// values.
	AttrOperation = attribute.Key("trove.operation")
	// AttrResult is ok, not_found, exists or error.
	AttrResult = attribute.Key("trove.result")
	// AttrObjectSize is the object's size in bytes as the driver reported
	// it.
	AttrObjectSize = attribute.Key("trove.object.size")

	// AttrMiddlewareCount is how many write middleware ran.
	AttrMiddlewareCount = attribute.Key("trove.middleware.count")
	// AttrMiddlewareNames are their Name values, in the order data passes
	// through them. Names are identifiers chosen in code (compress, encrypt,
	// scan), never user input.
	AttrMiddlewareNames = attribute.Key("trove.middleware.names")

	// AttrScanResult is clean, infected or error.
	AttrScanResult = attribute.Key("trove.scan.result")

	// AttrCASAlgorithm is the content hash algorithm: sha256, blake3 or
	// xxhash. The hash itself is never recorded.
	AttrCASAlgorithm = attribute.Key("trove.cas.algorithm")
	// AttrCASDeduplicated is whether identical content was already stored.
	AttrCASDeduplicated = attribute.Key("trove.cas.deduplicated")

	AttrGCScanned    = attribute.Key("trove.cas.gc.scanned")
	AttrGCDeleted    = attribute.Key("trove.cas.gc.deleted")
	AttrGCFreedBytes = attribute.Key("trove.cas.gc.freed_bytes")
	AttrGCErrors     = attribute.Key("trove.cas.gc.errors")
)

// Tracer returns Trove's tracer from the global provider.
//
// It is looked up on every call rather than held in a package variable. A
// tracer taken from the global before a provider is installed only follows
// the first provider ever installed, so tests that swap providers, and apps
// that install theirs late, would otherwise lose spans.
func Tracer() trace.Tracer {
	return otel.Tracer(InstrumentationName)
}

// Start starts a span of the given kind.
func Start(ctx context.Context, name string, kind trace.SpanKind, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithSpanKind(kind), trace.WithAttributes(attrs...))
}

// DriverContext returns ctx for a call into a storage driver: everything ctx
// carries except baggage. forge's server middleware extracts the client's
// baggage into the request context, and a driver whose client propagates
// trace context would forward it to the storage provider.
func DriverContext(ctx context.Context) context.Context {
	return baggage.ContextWithoutBaggage(ctx)
}

// DriverLabel maps a driver's Name to a driver label value. Anything that is
// not a built-in driver is DriverOther, so a custom or misnamed driver can
// never add series.
func DriverLabel(name string) string {
	switch name {
	case observe.DriverMem, observe.DriverLocal, observe.DriverS3, observe.DriverGCS,
		observe.DriverAzure, observe.DriverSFTP:
		return name
	default:
		return observe.DriverOther
	}
}

// DriverName returns the driver label for drv, or DriverOther when drv is
// nil.
func DriverName(drv driver.Driver) string {
	if drv == nil {
		return observe.DriverOther
	}
	return DriverLabel(drv.Name())
}

// SpanKind is Internal for the drivers that stay in the process (mem and
// local) and Client for every other, since a custom driver almost always
// talks to a remote store.
func SpanKind(driverLabel string) trace.SpanKind {
	switch driverLabel {
	case observe.DriverMem, observe.DriverLocal:
		return trace.SpanKindInternal
	default:
		return trace.SpanKindClient
	}
}

// Result is the result label for err: ok, not_found for a missing object or
// bucket, exists for a bucket that already exists, and error otherwise.
// not_found and exists are answers, not failures, so their spans keep an
// unset status.
func Result(err error) string {
	switch {
	case err == nil:
		return observe.ResultOK
	case errors.Is(err, driver.ErrNotFound):
		return observe.ResultNotFound
	case errors.Is(err, driver.ErrBucketExists):
		return observe.ResultExists
	default:
		return observe.ResultError
	}
}

// Sentinel names an error value for Class.
type Sentinel struct {
	Err  error
	Name string
}

// driverSentinels are the classifications every driver wraps. The root
// re-exports each under the same identity, so they are named as trove.*.
var driverSentinels = []Sentinel{
	{driver.ErrObjectNotFound, "trove.ErrObjectNotFound"},
	{driver.ErrBucketNotFound, "trove.ErrBucketNotFound"},
	{driver.ErrNotFound, "trove.ErrNotFound"},
	{driver.ErrBucketExists, "trove.ErrBucketExists"},
	{driver.ErrPermissionDenied, "trove.ErrPermissionDenied"},
	{driver.ErrQuotaExceeded, "trove.ErrQuotaExceeded"},
	{driver.ErrInvalidPath, "trove.ErrInvalidPath"},
}

// Class names err for a span's status and its exception.type: the Name of
// the first sentinel in known that err matches, then a driver sentinel, then
// a context error by its package-qualified name, and anything else by the Go
// type of the innermost error in its chain.
//
// It never returns the error's message. Driver errors quote the bucket and
// key (and backend errors can quote accounts and URLs), and none of that may
// reach a span.
func Class(err error, known ...Sentinel) string {
	for _, s := range known {
		if errors.Is(err, s.Err) {
			return s.Name
		}
	}
	for _, s := range driverSentinels {
		if errors.Is(err, s.Err) {
			return s.Name
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "context.DeadlineExceeded"
	case errors.Is(err, context.Canceled):
		return "context.Canceled"
	}
	return fmt.Sprintf("%T", innermost(err))
}

// innermost follows err's chain to its end. Where an error wraps several
// (errors.Join, or fmt.Errorf with more than one %w) it follows the first.
func innermost(err error) error {
	for {
		switch u := err.(type) { //nolint:errorlint // walking the chain one link at a time is the point
		case interface{ Unwrap() error }:
			next := u.Unwrap()
			if next == nil {
				return err
			}
			err = next
		case interface{ Unwrap() []error }:
			errs := u.Unwrap()
			if len(errs) == 0 || errs[0] == nil {
				return err
			}
			err = errs[0]
		default:
			return err
		}
	}
}

// Fail marks span as failed: an error status whose description is the
// error's class, and an exception event carrying only exception.type. It
// never records the error's message. A nil err does nothing.
func Fail(span trace.Span, err error, known ...Sentinel) {
	if err == nil {
		return
	}
	class := Class(err, known...)
	span.AddEvent(semconv.ExceptionEventName, trace.WithAttributes(semconv.ExceptionType(class)))
	span.SetStatus(codes.Error, class)
}

// Add adds delta to the counter name on r. A nil r records nothing.
func Add(r observe.Recorder, name string, delta float64, labelValues ...string) {
	if r == nil {
		return
	}
	r.Add(name, delta, labelValues...)
}

// Observe records value in the histogram name on r. A nil r records nothing.
func Observe(r observe.Recorder, name string, value float64, labelValues ...string) {
	if r == nil {
		return
	}
	r.Observe(name, value, labelValues...)
}

type recorderKey struct{}

// ContextWithRecorder returns ctx carrying r. The facade puts its Recorder
// on the context it hands write middleware, so middleware that records (the
// scan middleware) measures on the same Recorder as the Trove that runs it.
//
// A nil r is stored too, so it replaces whatever ctx carried: a Trove with no
// Recorder, called from inside another Trove's middleware, must not record
// on the outer one's.
func ContextWithRecorder(ctx context.Context, r observe.Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

// RecorderFrom returns the Recorder ctx carries, or nil.
func RecorderFrom(ctx context.Context) observe.Recorder {
	r, ok := ctx.Value(recorderKey{}).(observe.Recorder)
	if !ok {
		return nil
	}
	return r
}
