package trove

import (
	"context"
	"io"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/internal/telemetry"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/observe"
)

// noSize marks an operation that has no object size to report.
const noSize = -1

// rootSentinels name the root's own error values for a span's status.
var rootSentinels = []telemetry.Sentinel{
	{Err: ErrContentBlocked, Name: "trove.ErrContentBlocked"},
	{Err: ErrDriverClosed, Name: "trove.ErrDriverClosed"},
	{Err: ErrChecksumMismatch, Name: "trove.ErrChecksumMismatch"},
	{Err: io.ErrClosedPipe, Name: "io.ErrClosedPipe"},
}

// op is one traced, measured facade call.
type op struct {
	span      trace.Span
	start     time.Time
	driver    string
	operation string
	metrics   observe.Recorder
}

// startOp starts the span for a facade operation served by drv. The span is
// a Client span for a remote driver and an Internal one for mem and local,
// and it names the driver and the operation, never the bucket or the key.
func (t *Trove) startOp(ctx context.Context, drv driver.Driver, operation, spanName string) (context.Context, *op) {
	label := telemetry.DriverName(drv)
	ctx, span := telemetry.Start(ctx, spanName, telemetry.SpanKind(label),
		telemetry.AttrDriver.String(label),
		telemetry.AttrOperation.String(operation),
	)
	return ctx, &op{
		span:      span,
		start:     time.Now(),
		driver:    label,
		operation: operation,
		metrics:   t.metrics,
	}
}

// end ends the span and records the call. size is the object's size as the
// driver reported it, or noSize. A non-empty direction counts size as object
// bytes when the call succeeded.
func (o *op) end(err error, size int64, direction string) {
	result := telemetry.Result(err)
	o.span.SetAttributes(telemetry.AttrResult.String(result))
	if size >= 0 {
		o.span.SetAttributes(telemetry.AttrObjectSize.Int64(size))
	}
	if result == observe.ResultError {
		telemetry.Fail(o.span, err, rootSentinels...)
	}
	o.span.End()

	telemetry.Observe(o.metrics, observe.MetricOperationDuration, time.Since(o.start).Seconds(),
		o.driver, o.operation, result)
	if direction != "" && result == observe.ResultOK && size > 0 {
		telemetry.Add(o.metrics, observe.MetricObjectBytes, float64(size), o.driver, direction)
	}
}

func infoSize(info *driver.ObjectInfo) int64 {
	if info == nil {
		return noSize
	}
	return info.Size
}

func readerSize(obj *driver.ObjectReader) int64 {
	if obj == nil {
		return noSize
	}
	return infoSize(obj.Info)
}

// middlewareNames returns the Name of each middleware in the pipeline, or
// "unnamed" for one that has no Name method.
func middlewareNames(pipeline []middleware.WriteMiddleware) []string {
	names := make([]string, len(pipeline))
	for i, mw := range pipeline {
		names[i] = "unnamed"
		if n, ok := mw.(interface{ Name() string }); ok {
			names[i] = n.Name()
		}
	}
	return names
}
