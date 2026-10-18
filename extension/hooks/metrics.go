package hooks

import (
	"github.com/xraph/forge"

	"github.com/xraph/trove/observe"
)

// Metrics records Trove's metrics on a Forge app's metrics registry. It
// implements observe.Recorder, so you hand it to trove.WithMetrics (the
// extension does that for every store it opens) or to cas.WithMetrics.
//
// Every metric in observe.Metrics is declared once, up front, with its
// label keys. Forge exports each under its namespace prefix:
//
//	trove_operation_duration_seconds   forge_trove_operation_duration_seconds
//	trove_object_bytes_total           forge_trove_object_bytes_total
//	trove_scans_total                  forge_trove_scans_total
//	trove_cas_stores_total             forge_trove_cas_stores_total
//	trove_cas_gc_deleted_total         forge_trove_cas_gc_deleted_total
//	trove_cas_gc_freed_bytes_total     forge_trove_cas_gc_freed_bytes_total
//
// No metric carries a bucket, a key, a store name or a tenant. Package
// observe lists every label value.
type Metrics struct {
	counters   map[string]*forge.Vec[forge.Counter]
	histograms map[string]*forge.Vec[forge.Histogram]
}

var _ observe.Recorder = (*Metrics)(nil)

// NewMetrics declares Trove's metrics on m. Call it once per app and share
// the result across stores: a second call declares the same names again. A
// nil m returns nil, which records nothing.
func NewMetrics(m forge.Metrics) *Metrics {
	if m == nil {
		return nil
	}

	out := &Metrics{
		counters:   make(map[string]*forge.Vec[forge.Counter]),
		histograms: make(map[string]*forge.Vec[forge.Histogram]),
	}
	for _, def := range observe.Metrics() {
		switch def.Kind {
		case observe.KindCounter:
			out.counters[def.Name] = forge.CounterVec(m, def.Name, def.Labels...)
		case observe.KindHistogram:
			out.histograms[def.Name] = forge.HistogramVec(m, def.Name, forge.HistogramOpts{
				Buckets: def.Buckets,
				Help:    def.Help,
				Unit:    def.Unit,
			}, def.Labels...)
		}
	}
	return out
}

// Add implements observe.Recorder.
func (m *Metrics) Add(name string, delta float64, labelValues ...string) {
	if m == nil {
		return
	}
	if v, ok := m.counters[name]; ok {
		v.With(labelValues...).Add(delta)
	}
}

// Observe implements observe.Recorder.
func (m *Metrics) Observe(name string, value float64, labelValues ...string) {
	if m == nil {
		return
	}
	if v, ok := m.histograms[name]; ok {
		v.With(labelValues...).Observe(value)
	}
}
