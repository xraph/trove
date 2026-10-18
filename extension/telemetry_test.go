package extension

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/trove/drivers/memdriver"
)

func scrape(t *testing.T, app forge.App) string {
	t.Helper()
	p, ok := app.Metrics().(interface{ PrometheusHandler() http.Handler })
	if !ok {
		t.Fatalf("app metrics %T have no Prometheus handler", app.Metrics())
	}
	rr := httptest.NewRecorder()
	p.PrometheusHandler().ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/metrics", nil))
	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// series returns the scrape lines for name, without HELP and TYPE.
func series(body, name string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, name+"{") || strings.HasPrefix(line, name+" ") {
			out = append(out, line)
		}
	}
	return out
}

func hasSeries(lines []string, labels ...string) bool {
	for _, line := range lines {
		ok := true
		for _, l := range labels {
			if !strings.Contains(line, l) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func newTelemetryApp(t *testing.T) (forge.App, *tracetest.SpanRecorder) {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	app := forge.New(forge.WithAppName("trove-telemetry-test"), forge.WithAppLogger(forge.NewNoopLogger()))
	rec := tracetest.NewSpanRecorder()
	if err := forge.RegisterSpanProcessor(app, rec); err != nil {
		t.Fatalf("RegisterSpanProcessor: %v", err)
	}
	return app, rec
}

const (
	leakBucket = "tenant-acme"
	leakKey    = "invoices/alice@example.com.pdf"
)

func TestExtensionRecordsThroughForge(t *testing.T) {
	app, rec := newTelemetryApp(t)

	ext := New(WithDisableMigrate(), WithConfig(Config{DefaultBucket: leakBucket, EnableCAS: true}))
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tr := ext.Trove()

	ctx, parent := forge.StartSpan(t.Context(), "handle upload")
	if _, err := tr.Put(ctx, leakBucket, leakKey, strings.NewReader("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	obj, err := tr.Get(ctx, leakBucket, leakKey)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = obj.Close()
	if _, err := tr.Head(ctx, leakBucket, "missing"); err == nil {
		t.Fatal("Head of a missing key succeeded")
	}
	if err := tr.CreateBucket(ctx, "cas"); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if _, _, err := tr.CAS().Store(ctx, strings.NewReader("blob")); err != nil {
		t.Fatalf("CAS Store: %v", err)
	}
	if _, err := tr.CAS().GC(ctx); err != nil {
		t.Fatalf("CAS GC: %v", err)
	}
	parent.End()

	// Spans reach forge's provider, under the caller's span.
	var names []string
	for _, s := range rec.Ended() {
		if s.InstrumentationScope().Name != "github.com/xraph/trove" {
			continue
		}
		if s.Parent().SpanID() == parent.SpanContext().SpanID() {
			names = append(names, s.Name())
		}
		for _, v := range []string{leakBucket, leakKey} {
			if strings.Contains(s.Name(), v) || strings.Contains(s.Status().Description, v) {
				t.Errorf("%q reached span %s", v, s.Name())
			}
			for _, kv := range s.Attributes() {
				if strings.Contains(kv.Value.String(), v) {
					t.Errorf("%q reached span %s attribute %s", v, s.Name(), kv.Key)
				}
			}
		}
	}
	if got := strings.Join(names, ","); got != "put object,get object,head object,create bucket,store cas object,collect cas garbage" {
		t.Fatalf("trove spans under the caller = %s", got)
	}

	// Metrics are on the scrape under forge's namespace.
	body := scrape(t, app)
	counts := series(body, "forge_trove_operation_duration_seconds_count")
	for _, want := range [][]string{
		{`driver="mem"`, `operation="put"`, `result="ok"`},
		{`driver="mem"`, `operation="get"`, `result="ok"`},
		{`driver="mem"`, `operation="head"`, `result="not_found"`},
	} {
		if !hasSeries(counts, want...) {
			t.Errorf("no duration series with %v in:\n%s", want, strings.Join(counts, "\n"))
		}
	}
	bytes := series(body, "forge_trove_object_bytes_total")
	if !hasSeries(bytes, `direction="write"`, `driver="mem"`, "} 5") {
		t.Errorf("write bytes missing:\n%s", strings.Join(bytes, "\n"))
	}
	if !hasSeries(bytes, `direction="read"`, `driver="mem"`, "} 5") {
		t.Errorf("read bytes missing:\n%s", strings.Join(bytes, "\n"))
	}
	if !hasSeries(series(body, "forge_trove_cas_stores_total"), `result="stored"`, "} 1") {
		t.Errorf("CAS store count missing:\n%s", strings.Join(series(body, "forge_trove_cas_stores_total"), "\n"))
	}
	// The unlabelled GC counters are on the scrape even when a run frees
	// nothing.
	for _, name := range []string{"forge_trove_cas_gc_deleted_total", "forge_trove_cas_gc_freed_bytes_total"} {
		if got := series(body, name); len(got) != 1 || got[0] != name+" 0" {
			t.Errorf("%s on the scrape = %v", name, got)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "forge_trove_") && (strings.Contains(line, leakBucket) || strings.Contains(line, "bucket=")) {
			t.Errorf("a trove series carries a bucket: %s", line)
		}
	}
}

// TestExtensionStoresShareSeries checks that two stores record into the
// same series: no store label, and no second declaration of the metrics.
func TestExtensionStoresShareSeries(t *testing.T) {
	app, _ := newTelemetryApp(t)

	a, b := memdriver.New(), memdriver.New()
	for _, d := range []*memdriver.MemDriver{a, b} {
		if err := d.Open(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
	}
	ext := New(WithDisableMigrate(), WithFileStore("a", a), WithFileStore("b", b))
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, name := range []string{"a", "b"} {
		tr, err := ext.Manager().Get(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.CreateBucket(t.Context(), "x"); err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Put(t.Context(), "x", "k", strings.NewReader("abc")); err != nil {
			t.Fatal(err)
		}
	}

	counts := series(scrape(t, app), "forge_trove_operation_duration_seconds_count")
	var puts []string
	for _, line := range counts {
		if strings.Contains(line, `operation="put"`) {
			puts = append(puts, line)
		}
	}
	if len(puts) != 1 || !strings.HasSuffix(puts[0], " 2") {
		t.Fatalf("want one put series counting both stores, got:\n%s", strings.Join(puts, "\n"))
	}
	if strings.Contains(puts[0], "store") {
		t.Fatalf("the series carries a store label: %s", puts[0])
	}
}
