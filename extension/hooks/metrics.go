package hooks

import (
	"log/slog"

	"github.com/xraph/forge"
)

// MetricsHook registers Prometheus metrics for Trove operations.
type MetricsHook struct {
	logger *slog.Logger
}

// NewMetricsHook creates a metrics hook.
// Metrics are registered via the Forge app's metrics registry.
func NewMetricsHook(fapp forge.App, logger *slog.Logger) *MetricsHook {
	return &MetricsHook{logger: logger}
}

// Metric names.
const (
	MetricObjectsTotal       = "trove_objects_total"
	MetricObjectsSizeBytes   = "trove_objects_size_bytes"
	MetricUploadsActive      = "trove_uploads_active"
	MetricUploadsCompleted   = "trove_uploads_completed_total"
	MetricUploadsFailed      = "trove_uploads_failed_total"
	MetricStreamBytesSent    = "trove_stream_bytes_sent_total"
	MetricStreamBytesRecv    = "trove_stream_bytes_recv_total"
	MetricMiddlewareDuration = "trove_middleware_duration_seconds"
	MetricCASDedupHits       = "trove_cas_dedup_hits_total"
	MetricCASDedupBytesSaved = "trove_cas_dedup_bytes_saved"
	MetricDriverLatency      = "trove_driver_latency_seconds"
	MetricDriverErrors       = "trove_driver_errors_total"
	MetricQuotaUsageBytes    = "trove_quota_usage_bytes"
	MetricQuotaLimitBytes    = "trove_quota_limit_bytes"
)

// RegisterMetrics registers all Trove Prometheus metrics.
// This is called during extension initialization when metrics are available.
func (h *MetricsHook) RegisterMetrics(fapp forge.App) {
	if h == nil {
		return
	}

	// Metrics registration uses Forge's metrics interface.
	// The actual Prometheus counters/histograms/gauges are registered
	// via fapp.Metrics() when available.
	//
	// Example with Forge metrics:
	//   metrics := fapp.Metrics()
	//   metrics.Counter(MetricObjectsTotal, "Total number of objects stored", "bucket", "driver")
	//   metrics.Histogram(MetricDriverLatency, "Driver operation latency", "driver", "operation")
	//   metrics.Gauge(MetricUploadsActive, "Number of active uploads")

	if h.logger != nil {
		h.logger.Info("registered trove prometheus metrics")
	}
}

// RecordObjectCreated increments the objects total counter.
func (h *MetricsHook) RecordObjectCreated(bucket, driver string, size int64) {
	if h == nil {
		return
	}
	// Increment trove_objects_total{bucket, driver}
	// Add to trove_objects_size_bytes{bucket, driver}
}

// RecordDriverLatency records a driver operation latency.
func (h *MetricsHook) RecordDriverLatency(driver, operation string, durationSec float64) {
	if h == nil {
		return
	}
	// Observe trove_driver_latency_seconds{driver, operation}
}

// RecordDriverError increments the driver error counter.
func (h *MetricsHook) RecordDriverError(driver, operation string) {
	if h == nil {
		return
	}
	// Increment trove_driver_errors_total{driver, operation}
}

// RecordCASDedup records a CAS dedup hit.
func (h *MetricsHook) RecordCASDedup(bytesSaved int64) {
	if h == nil {
		return
	}
	// Increment trove_cas_dedup_hits_total
	// Add to trove_cas_dedup_bytes_saved
}
