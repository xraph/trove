package scan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/internal/telemetry"
	"github.com/xraph/trove/internal/telemetrytest"
	"github.com/xraph/trove/observe"
)

const (
	leakKey    = "inbox/alice@example.com/invoice.txt"
	leakThreat = "Win.Test.EICAR_HDB-1"
)

func openScanned(t *testing.T, metrics observe.Recorder, opts ...Option) *trove.Trove {
	t.Helper()
	tr, err := trove.Open(memdriver.New(),
		trove.WithWriteMiddleware(New(opts...)),
		trove.WithMetrics(metrics),
	)
	require.NoError(t, err)
	require.NoError(t, tr.CreateBucket(t.Context(), "b"))
	return tr
}

func TestScanSpanAndCount(t *testing.T) {
	cases := []struct {
		name     string
		provider *mockProvider
		wantErr  error
		result   string
		status   codes.Code
	}{
		{"clean", &mockProvider{result: &Result{Clean: true}}, nil, observe.ScanClean, codes.Unset},
		{"infected", &mockProvider{result: &Result{Threat: leakThreat, Details: leakKey}}, trove.ErrContentBlocked, observe.ScanInfected, codes.Unset},
		{"provider error", &mockProvider{err: errors.New("clamav: scan " + leakKey + ": connection refused")}, nil, observe.ScanError, codes.Error},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, ended := telemetrytest.SpansWithEnds(t)
			metrics := telemetrytest.NewRecorder(t)
			tr := openScanned(t, metrics, WithProvider(tc.provider))

			_, err := tr.Put(t.Context(), "b", leakKey, strings.NewReader("payload"))
			switch {
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			case tc.status == codes.Error:
				require.Error(t, err)
			default:
				require.NoError(t, err)
			}

			// Put returns once the driver has read the pipe to the end, which
			// happens only after the middleware chain closed, so the scan span
			// has ended by now.
			scans := telemetrytest.Named(rec, "scan object")
			require.Len(t, scans, 1)
			s := scans[0]
			assert.Equal(t, trace.SpanKindClient, s.SpanKind())
			v, _ := telemetrytest.Attr(s, "trove.scan.result")
			assert.Equal(t, tc.result, v)
			v, _ = telemetrytest.Attr(s, "trove.object.size")
			assert.Equal(t, "7", v)
			assert.Equal(t, tc.status, s.Status().Code)

			assert.InDelta(t, 1, metrics.Counter(observe.MetricScans, tc.result), 0)

			// A blocked upload fails the middleware span with the sentinel's
			// name, not the scanner's verdict.
			telemetrytest.WaitEnded(t, ended, "apply write middleware")
			mw := telemetrytest.Named(rec, "apply write middleware")[0]
			if tc.wantErr != nil {
				assert.Equal(t, "trove.ErrContentBlocked", mw.Status().Description)
			}

			for _, leak := range []string{leakKey, leakThreat, "alice@example.com"} {
				assert.Empty(t, telemetrytest.Leaks(rec, leak), "%q reached a span", leak)
			}
		})
	}
}

func TestScanSpanIsAChildOfTheMiddlewareSpan(t *testing.T) {
	rec := telemetrytest.Spans(t)
	tr := openScanned(t, nil, WithProvider(&mockProvider{result: &Result{Clean: true}}))

	_, err := tr.Put(t.Context(), "b", "k.txt", strings.NewReader("payload"))
	require.NoError(t, err)

	scan := telemetrytest.Named(rec, "scan object")[0]
	var mwID trace.SpanID
	for _, s := range rec.Started() {
		if s.Name() == "apply write middleware" {
			mwID = s.SpanContext().SpanID()
		}
	}
	assert.Equal(t, mwID, scan.Parent().SpanID())
}

func TestScanSkipsAreCounted(t *testing.T) {
	telemetrytest.Spans(t)
	metrics := telemetrytest.NewRecorder(t)
	provider := &mockProvider{result: &Result{Clean: true}}

	byExt := openScanned(t, metrics, WithProvider(provider), WithSkipExtensions("bin"))
	_, err := byExt.Put(t.Context(), "b", "blob.bin", strings.NewReader("payload"))
	require.NoError(t, err)

	bySize := openScanned(t, metrics, WithProvider(provider), WithMaxSize(3))
	_, err = bySize.Put(t.Context(), "b", "big.txt", strings.NewReader("payload"))
	require.NoError(t, err)

	assert.InDelta(t, 2, metrics.Counter(observe.MetricScans, observe.ScanSkipped), 0)
	assert.InDelta(t, 0, metrics.Counter(observe.MetricScans, observe.ScanClean), 0)
}

// TestNestedTroveWithoutMetricsDoesNotInheritARecorder runs a Put whose
// context already carries another Trove's Recorder, as it would inside that
// Trove's middleware. The inner Trove has no metrics, so its scan must not
// count on the outer Recorder.
func TestNestedTroveWithoutMetricsDoesNotInheritARecorder(t *testing.T) {
	telemetrytest.Spans(t)
	outer := telemetrytest.NewRecorder(t)
	ctx := telemetry.ContextWithRecorder(t.Context(), outer)

	inner := openScanned(t, nil, WithProvider(&mockProvider{result: &Result{Clean: true}}))
	_, err := inner.Put(ctx, "b", "k.txt", strings.NewReader("payload"))
	require.NoError(t, err)

	assert.InDelta(t, 0, outer.Counter(observe.MetricScans, observe.ScanClean), 0)
	assert.Empty(t, outer.Series(observe.MetricScans))
}

// TestScanOutsideTroveRecordsNoMetrics checks the middleware still works on
// a context with no Recorder, as when it is used without a Trove.
func TestScanOutsideTroveRecordsNoMetrics(t *testing.T) {
	telemetrytest.Spans(t)
	s := New(WithProvider(&mockProvider{result: &Result{Clean: true}}))
	buf := &bufCloser{}
	w, err := s.WrapWriter(context.Background(), buf, "k")
	require.NoError(t, err)
	_, err = w.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	assert.True(t, buf.closed)
}
