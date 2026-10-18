package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/observe"
)

func TestDriverLabel(t *testing.T) {
	for _, name := range []string{"mem", "local", "s3", "gcs", "azure", "sftp"} {
		assert.Equal(t, name, DriverLabel(name))
	}
	for _, name := range []string{"", "mock", "S3", "tenant-acme", "file"} {
		assert.Equal(t, observe.DriverOther, DriverLabel(name), name)
	}
	assert.Equal(t, observe.DriverOther, DriverName(nil))
	assert.Equal(t, trace.SpanKindInternal, SpanKind(observe.DriverMem))
	assert.Equal(t, trace.SpanKindInternal, SpanKind(observe.DriverLocal))
	assert.Equal(t, trace.SpanKindClient, SpanKind(observe.DriverS3))
	assert.Equal(t, trace.SpanKindClient, SpanKind(observe.DriverOther))
}

func TestResult(t *testing.T) {
	assert.Equal(t, observe.ResultOK, Result(nil))
	assert.Equal(t, observe.ResultNotFound, Result(fmt.Errorf("x: %w", driver.ErrObjectNotFound)))
	assert.Equal(t, observe.ResultNotFound, Result(driver.ErrBucketNotFound))
	assert.Equal(t, observe.ResultExists, Result(fmt.Errorf("x: %w", driver.ErrBucketExists)))
	assert.Equal(t, observe.ResultError, Result(errors.New("boom")))
}

type pathError struct{ path string }

func (e *pathError) Error() string { return "open " + e.path }

func TestClassNeverQuotesTheMessage(t *testing.T) {
	secret := "bucket/alice@example.com"
	cases := map[string]error{
		"trove.ErrPermissionDenied": fmt.Errorf("s3: %s: %w", secret, driver.ErrPermissionDenied),
		"trove.ErrQuotaExceeded":    fmt.Errorf("s3: %s: %w", secret, driver.ErrQuotaExceeded),
		"context.DeadlineExceeded":  fmt.Errorf("s3: %s: %w", secret, context.DeadlineExceeded),
		"*telemetry.pathError":      fmt.Errorf("wrap %s: %w", secret, &pathError{path: secret}),
		"*errors.errorString":       errors.Join(errors.New(secret), errors.New("second")),
		"io.EOF":                    fmt.Errorf("%s: %w", secret, io.EOF),
	}
	for want, err := range cases {
		got := Class(err, Sentinel{Err: io.EOF, Name: "io.EOF"})
		assert.Equal(t, want, got)
		assert.NotContains(t, got, secret)
	}
}

type countingRecorder struct{ adds int }

func (c *countingRecorder) Add(string, float64, ...string)     { c.adds++ }
func (c *countingRecorder) Observe(string, float64, ...string) {}

func TestRecorderContext(t *testing.T) {
	assert.Nil(t, RecorderFrom(context.Background()))
	assert.Nil(t, RecorderFrom(ContextWithRecorder(context.Background(), nil)))

	// A nil Recorder replaces the one the context carried, so a Trove with
	// no metrics never records on an outer Trove's Recorder.
	outer := &countingRecorder{}
	ctx := ContextWithRecorder(context.Background(), outer)
	assert.Equal(t, observe.Recorder(outer), RecorderFrom(ctx))
	assert.Nil(t, RecorderFrom(ContextWithRecorder(ctx, nil)))

	// A nil Recorder records nothing and does not panic.
	Add(nil, observe.MetricScans, 1, observe.ScanClean)
	Observe(nil, observe.MetricOperationDuration, 1, "mem", "put", "ok")
}
