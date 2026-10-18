// Package observe defines the metrics Trove records and the small interface
// it records them through.
//
// It imports nothing outside the standard library, so the root module stays
// free of any metrics framework. You hand a Recorder to trove.WithMetrics (and
// to cas.WithMetrics when you build a CAS yourself), and it receives every
// measurement by name. Metrics lists the name, kind, label keys and buckets of
// each one, so an adapter can declare them all up front on whatever registry
// you use. The Forge extension does exactly that against the app's metrics
// registry, where a metric named trove_scans_total scrapes as
// forge_trove_scans_total.
//
// Label values are fixed enums, listed below. Trove never puts a bucket, an
// object key, a content hash, a tenant or any error text on a label.
package observe

// Recorder receives Trove's measurements. Every call names a metric from
// Metrics and passes its label values in the order that metric declares
// its label keys.
//
// Trove calls a Recorder on the hot path of every storage operation, from
// many goroutines at once. Implementations must be safe for concurrent use
// and must not block. A name the Recorder does not know should be ignored.
type Recorder interface {
	// Add adds delta, which is never negative, to the counter called name.
	Add(name string, delta float64, labelValues ...string)

	// Observe records value in the histogram called name.
	Observe(name string, value float64, labelValues ...string)
}

// Kind is the type of a metric.
type Kind int

const (
	// KindCounter is a monotonic counter, recorded with Recorder.Add.
	KindCounter Kind = iota + 1
	// KindHistogram is a histogram, recorded with Recorder.Observe.
	KindHistogram
)

// String returns "counter" or "histogram".
func (k Kind) String() string {
	switch k {
	case KindCounter:
		return "counter"
	case KindHistogram:
		return "histogram"
	default:
		return "unknown"
	}
}

// Metric describes one metric Trove records.
type Metric struct {
	// Name is the metric name, such as trove_scans_total.
	Name string
	// Kind says whether it is a counter or a histogram.
	Kind Kind
	// Help is a one-line description.
	Help string
	// Unit is the OTel unit: "s" for durations, "By" for bytes, "" for counts.
	Unit string
	// Labels are the label keys, in the order values are passed.
	Labels []string
	// Buckets are a histogram's upper bounds. Nil for counters.
	Buckets []float64
}

// Metric names.
const (
	// MetricOperationDuration is how long each facade operation took,
	// in seconds. Labels: driver, operation, result.
	MetricOperationDuration = "trove_operation_duration_seconds"
	// MetricObjectBytes counts object bytes through Put and Get. Labels:
	// driver, direction.
	MetricObjectBytes = "trove_object_bytes_total"
	// MetricScans counts content scans by outcome. Label: result.
	MetricScans = "trove_scans_total"
	// MetricCASStores counts CAS Store calls by outcome. Label: result.
	MetricCASStores = "trove_cas_stores_total"
	// MetricCASGCDeleted counts objects CAS garbage collection deleted.
	MetricCASGCDeleted = "trove_cas_gc_deleted_total"
	// MetricCASGCFreedBytes counts bytes CAS garbage collection freed.
	MetricCASGCFreedBytes = "trove_cas_gc_freed_bytes_total"
)

// Label keys.
const (
	LabelDriver    = "driver"
	LabelOperation = "operation"
	LabelResult    = "result"
	LabelDirection = "direction"
)

// Values of the driver label. A driver whose Name is none of the built-in
// ones records as DriverOther, so a custom driver cannot add series.
const (
	DriverMem   = "mem"
	DriverLocal = "local"
	DriverS3    = "s3"
	DriverGCS   = "gcs"
	DriverAzure = "azure"
	DriverSFTP  = "sftp"
	DriverOther = "other"
)

// Values of the operation label, one per facade method.
const (
	OperationPut          = "put"
	OperationGet          = "get"
	OperationDelete       = "delete"
	OperationHead         = "head"
	OperationList         = "list"
	OperationCopy         = "copy"
	OperationCreateBucket = "create_bucket"
	OperationDeleteBucket = "delete_bucket"
	OperationListBuckets  = "list_buckets"
)

// Values of the result label on MetricOperationDuration.
const (
	// ResultOK is a call that succeeded.
	ResultOK = "ok"
	// ResultNotFound is a call that found no such object or bucket. It is
	// an answer, not a failure.
	ResultNotFound = "not_found"
	// ResultExists is a CreateBucket for a bucket that already exists.
	ResultExists = "exists"
	// ResultError is any other failure.
	ResultError = "error"
)

// Values of the direction label on MetricObjectBytes.
const (
	// DirectionWrite is the size the driver reported storing on Put, after
	// write middleware (compressed or encrypted bytes, when those run).
	DirectionWrite = "write"
	// DirectionRead is the stored size of each object Get returned, before
	// read middleware. It is not a count of bytes the caller went on to
	// read.
	DirectionRead = "read"
)

// Values of the result label on MetricScans.
const (
	ScanClean    = "clean"
	ScanInfected = "infected"
	ScanError    = "error"
	// ScanSkipped is an upload the scan middleware passed through unscanned,
	// because of its extension or because it was larger than the maximum.
	ScanSkipped = "skipped"
)

// Values of the result label on MetricCASStores.
const (
	CASStored       = "stored"
	CASDeduplicated = "deduplicated"
	CASError        = "error"
)

// OperationBuckets are the bounds, in seconds, of MetricOperationDuration.
// They span an in-memory lookup (well under a millisecond) to a large
// upload to a remote store (minutes).
var OperationBuckets = []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300}

// Metrics returns every metric Trove records. The slice and everything in it
// are fresh copies, so the caller may keep or change them.
func Metrics() []Metric {
	return []Metric{
		{
			Name:    MetricOperationDuration,
			Kind:    KindHistogram,
			Help:    "Duration of Trove storage operations, by driver, operation and result.",
			Unit:    "s",
			Labels:  []string{LabelDriver, LabelOperation, LabelResult},
			Buckets: append([]float64(nil), OperationBuckets...),
		},
		{
			Name:   MetricObjectBytes,
			Kind:   KindCounter,
			Help:   "Object bytes Trove stored on Put and returned on Get, by driver and direction.",
			Unit:   "By",
			Labels: []string{LabelDriver, LabelDirection},
		},
		{
			Name:   MetricScans,
			Kind:   KindCounter,
			Help:   "Uploads the scan middleware handled, by result.",
			Labels: []string{LabelResult},
		},
		{
			Name:   MetricCASStores,
			Kind:   KindCounter,
			Help:   "CAS Store calls, by result.",
			Labels: []string{LabelResult},
		},
		{
			Name: MetricCASGCDeleted,
			Kind: KindCounter,
			Help: "Unreferenced CAS objects garbage collection deleted.",
		},
		{
			Name: MetricCASGCFreedBytes,
			Kind: KindCounter,
			Help: "Bytes CAS garbage collection freed.",
			Unit: "By",
		},
	}
}
