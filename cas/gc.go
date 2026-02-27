package cas

import (
	"context"
	"fmt"
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
func (c *CAS) gc(ctx context.Context) (*GCResult, error) {
	entries, err := c.index.ListUnpinned(ctx)
	if err != nil {
		return nil, fmt.Errorf("cas: gc list unpinned: %w", err)
	}

	result := &GCResult{Scanned: len(entries)}

	for _, entry := range entries {
		// Delete the object from storage.
		if err := c.store.Delete(ctx, entry.Bucket, entry.Key); err != nil {
			result.Errors++
			continue
		}

		// Remove from index.
		if err := c.index.Delete(ctx, entry.Hash); err != nil {
			result.Errors++
			continue
		}

		result.Deleted++
		result.FreedBytes += entry.Size
	}

	return result, nil
}
