// Package telemetrytest records what Trove's telemetry emits, for tests: a
// span recorder installed as the global tracer provider, and an
// observe.Recorder that remembers every measurement and checks it against
// the metric definitions in package observe.
package telemetrytest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/trove/observe"
)

// Spans installs a span recorder as the global tracer provider, and forge's
// default propagators (tracecontext and baggage) as the global propagator,
// for the rest of the test. The previous ones come back when the test ends.
func Spans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec, _ := SpansWithEnds(t)
	return rec
}

// SpansWithEnds is Spans, and also reports the name of each span as it
// ends, so a test can wait with WaitEnded for a span another goroutine
// ends instead of sleeping.
func SpansWithEnds(t *testing.T) (rec *tracetest.SpanRecorder, ends <-chan string) {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	rec = tracetest.NewSpanRecorder()
	n := notifier{SpanProcessor: rec, ch: make(chan string, 256)}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(n))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Errorf("shut down the test tracer provider: %v", err)
		}
	})
	return rec, n.ch
}

// notifier hands each ended span to the recorder, then reports its name.
type notifier struct {
	sdktrace.SpanProcessor
	ch chan string
}

func (n notifier) OnEnd(s sdktrace.ReadOnlySpan) {
	n.SpanProcessor.OnEnd(s)
	select {
	case n.ch <- s.Name():
	default:
	}
}

// WaitEnded waits until a span called name ends, failing the test after ten
// seconds.
func WaitEnded(t *testing.T, ends <-chan string, name string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case got := <-ends:
			if got == name {
				return
			}
		case <-timeout:
			t.Fatalf("span %q never ended", name)
		}
	}
}

// Named returns the ended spans called name, in the order they ended.
func Named(rec *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

// Attr returns the value of the attribute key on s, formatted as a string,
// and whether s has it.
func Attr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.String(), true
		}
	}
	return "", false
}

// Leaks returns every place value appears in what the recorder saw: a span
// name, an attribute, an event name or attribute, or a status description.
// A test that feeds a key, a bucket or a hash through the code asserts this
// is empty.
func Leaks(rec *tracetest.SpanRecorder, value string) []string {
	var out []string
	for _, s := range rec.Ended() {
		if strings.Contains(s.Name(), value) {
			out = append(out, fmt.Sprintf("%s: name", s.Name()))
		}
		for _, kv := range s.Attributes() {
			if strings.Contains(kv.Value.String(), value) {
				out = append(out, fmt.Sprintf("%s: attribute %s", s.Name(), kv.Key))
			}
		}
		for _, ev := range s.Events() {
			if strings.Contains(ev.Name, value) {
				out = append(out, fmt.Sprintf("%s: event %s", s.Name(), ev.Name))
			}
			for _, kv := range ev.Attributes {
				if strings.Contains(kv.Value.String(), value) {
					out = append(out, fmt.Sprintf("%s: event %s attribute %s", s.Name(), ev.Name, kv.Key))
				}
			}
		}
		if strings.Contains(s.Status().Description, value) {
			out = append(out, fmt.Sprintf("%s: status", s.Name()))
		}
	}
	return out
}

// allowed lists every value each label key may take.
var allowed = map[string][]string{
	observe.LabelDriver: {
		observe.DriverMem, observe.DriverLocal, observe.DriverS3, observe.DriverGCS,
		observe.DriverAzure, observe.DriverSFTP, observe.DriverOther,
	},
	observe.LabelOperation: {
		observe.OperationPut, observe.OperationGet, observe.OperationDelete, observe.OperationHead,
		observe.OperationList, observe.OperationCopy, observe.OperationCreateBucket,
		observe.OperationDeleteBucket, observe.OperationListBuckets,
	},
	observe.LabelDirection: {observe.DirectionRead, observe.DirectionWrite},
}

// allowedResults lists the result values per metric, since each metric has
// its own set.
var allowedResults = map[string][]string{
	observe.MetricOperationDuration: {observe.ResultOK, observe.ResultNotFound, observe.ResultExists, observe.ResultError},
	observe.MetricScans:             {observe.ScanClean, observe.ScanInfected, observe.ScanError, observe.ScanSkipped},
	observe.MetricCASStores:         {observe.CASStored, observe.CASDeduplicated, observe.CASError},
}

// Recorder is an observe.Recorder that keeps every measurement. It fails the
// test when a call names a metric observe does not declare, uses the wrong
// method for its kind, passes the wrong number of label values, or passes a
// value outside the documented set.
type Recorder struct {
	t    testing.TB
	defs map[string]observe.Metric

	mu       sync.Mutex
	counters map[string]float64
	observed map[string][]float64
}

// NewRecorder returns an empty Recorder that reports misuse to t.
func NewRecorder(t testing.TB) *Recorder {
	defs := make(map[string]observe.Metric)
	for _, m := range observe.Metrics() {
		defs[m.Name] = m
	}
	return &Recorder{t: t, defs: defs, counters: map[string]float64{}, observed: map[string][]float64{}}
}

// Add implements observe.Recorder.
func (r *Recorder) Add(name string, delta float64, labelValues ...string) {
	k := r.check(name, observe.KindCounter, labelValues)
	if delta < 0 {
		r.t.Errorf("%s: negative delta %v", name, delta)
	}
	r.mu.Lock()
	r.counters[k] += delta
	r.mu.Unlock()
}

// Observe implements observe.Recorder.
func (r *Recorder) Observe(name string, value float64, labelValues ...string) {
	k := r.check(name, observe.KindHistogram, labelValues)
	r.mu.Lock()
	r.observed[k] = append(r.observed[k], value)
	r.mu.Unlock()
}

func (r *Recorder) check(name string, kind observe.Kind, values []string) string {
	def, ok := r.defs[name]
	switch {
	case !ok:
		r.t.Errorf("recorded undeclared metric %s", name)
	case def.Kind != kind:
		r.t.Errorf("%s is a %s, recorded as a %s", name, def.Kind, kind)
	case len(values) != len(def.Labels):
		r.t.Errorf("%s: %d label values for %d keys %v", name, len(values), len(def.Labels), def.Labels)
	default:
		for i, key := range def.Labels {
			set := allowed[key]
			if key == observe.LabelResult {
				set = allowedResults[name]
			}
			if !slices.Contains(set, values[i]) {
				r.t.Errorf("%s: label %s=%q is not a documented value", name, key, values[i])
			}
		}
	}
	return Key(name, values...)
}

// Key is how Recorder files a series: the name, then the label values.
func Key(name string, labelValues ...string) string {
	return strings.Join(append([]string{name}, labelValues...), "|")
}

// Counter returns the total added to the series.
func (r *Recorder) Counter(name string, labelValues ...string) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counters[Key(name, labelValues...)]
}

// Count returns how many values the histogram series observed.
func (r *Recorder) Count(name string, labelValues ...string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.observed[Key(name, labelValues...)])
}

// Series returns the keys of every series recorded under name.
func (r *Recorder) Series(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for k := range r.counters {
		if strings.HasPrefix(k, name+"|") || k == name {
			out = append(out, k)
		}
	}
	for k := range r.observed {
		if strings.HasPrefix(k, name+"|") || k == name {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
