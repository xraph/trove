package cas

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/trove/internal/telemetry"
	"github.com/xraph/trove/observe"
)

// GCResult reports the outcome of a garbage collection run.
type GCResult struct {
	// Scanned is the number of entries examined.
	Scanned int `json:"scanned"`

	// Deleted is the number of unreferenced objects removed.
	Deleted int `json:"deleted"`

	// FreedBytes is the total bytes freed.
	FreedBytes int64 `json:"freed_bytes"`

	// Errors is the number of entries that failed to delete.
	Errors int `json:"errors"`
}

// gc performs garbage collection, removing unreferenced and unpinned objects.
//
// It records one "collect cas garbage" span with the counts from GCResult,
// as a child of whatever span ctx carries (the dashboard request or the
// job that asked for it). A run that fails to delete some entries is not a
// failed span: the span and the result carry the count. Only a failure to
// list the candidates marks it failed.
func (c *CAS) gc(ctx context.Context) (result *GCResult, err error) {
	ctx, span := telemetry.Start(ctx, telemetry.SpanCollectCASGarbage, trace.SpanKindInternal,
		telemetry.AttrCASAlgorithm.String(c.algorithm.String()),
		telemetry.AttrDriver.String(telemetry.DriverName(c.store)),
	)
	defer func() {
		if err != nil {
			telemetry.Fail(span, err)
		}
		if result != nil {
			span.SetAttributes(
				telemetry.AttrGCScanned.Int(result.Scanned),
				telemetry.AttrGCDeleted.Int(result.Deleted),
				telemetry.AttrGCFreedBytes.Int64(result.FreedBytes),
				telemetry.AttrGCErrors.Int(result.Errors),
			)
			telemetry.Add(c.metrics, observe.MetricCASGCDeleted, float64(result.Deleted))
			telemetry.Add(c.metrics, observe.MetricCASGCFreedBytes, float64(result.FreedBytes))
		}
		span.End()
	}()

	entries, err := c.index.ListUnpinned(ctx)
	if err != nil {
		return nil, fmt.Errorf("cas: gc list unpinned: %w", err)
	}

	result = &GCResult{Scanned: len(entries)}
	driverCtx := telemetry.DriverContext(ctx)

	for _, entry := range entries {
		// Delete the object from storage.
		if delErr := c.store.Delete(driverCtx, entry.Bucket, entry.Key); delErr != nil {
			result.Errors++
			continue
		}

		// Remove from index.
		if delErr := c.index.Delete(ctx, entry.Hash); delErr != nil {
			result.Errors++
			continue
		}

		result.Deleted++
		result.FreedBytes += entry.Size
	}

	return result, nil
}
