package dashboard

import (
	"context"

	"github.com/xraph/trove/extension/model"
	"github.com/xraph/trove/extension/store"
)

// StorageStats holds aggregated counts for the overview page.
type StorageStats struct {
	TotalBuckets  int
	TotalObjects  int
	TotalSize     int64
	ActiveUploads int
	CASEntries    int
}

// fetchStorageStats returns aggregated storage statistics.
func fetchStorageStats(ctx context.Context, s *store.Store) StorageStats {
	var stats StorageStats

	buckets, err := s.ListBuckets(ctx, "")
	if err == nil {
		stats.TotalBuckets = len(buckets)

		for _, b := range buckets {
			objects, oErr := s.ListObjects(ctx, b.ID)
			if oErr == nil {
				stats.TotalObjects += len(objects)
				for _, o := range objects {
					stats.TotalSize += o.Size
				}
			}
		}
	}

	uploads, err := s.ListUploads(ctx)
	if err == nil {
		for _, u := range uploads {
			if u.Status == model.UploadStatusPending || u.Status == model.UploadStatusActive {
				stats.ActiveUploads++
			}
		}
	}

	cas, err := s.ListCASEntries(ctx)
	if err == nil {
		stats.CASEntries = len(cas)
	}

	return stats
}

// fetchBuckets returns all buckets.
func fetchBuckets(ctx context.Context, s *store.Store) ([]*model.Bucket, error) {
	return s.ListBuckets(ctx, "")
}

// fetchBucket returns a bucket by ID.
func fetchBucket(ctx context.Context, s *store.Store, id string) (*model.Bucket, error) {
	return s.GetBucket(ctx, id)
}

// fetchObjects returns objects in a bucket with optional filters.
func fetchObjects(ctx context.Context, s *store.Store, bucketID string, opts ...store.ListOption) ([]*model.Object, error) {
	return s.ListObjects(ctx, bucketID, opts...)
}

// fetchAllObjects returns objects across all buckets.
func fetchAllObjects(ctx context.Context, s *store.Store, limit int) ([]*model.Object, error) {
	return s.ListAllObjects(ctx, limit)
}

// fetchObject returns an object by ID.
func fetchObject(ctx context.Context, s *store.Store, id string) (*model.Object, error) {
	return s.GetObject(ctx, id)
}

// fetchRecentObjects returns the most recently created objects.
func fetchRecentObjects(ctx context.Context, s *store.Store, limit int) ([]*model.Object, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.ListAllObjects(ctx, limit)
}

// fetchUploads returns all upload sessions.
func fetchUploads(ctx context.Context, s *store.Store) ([]*model.UploadSession, error) {
	return s.ListUploads(ctx)
}

// fetchUpload returns an upload session by ID.
func fetchUpload(ctx context.Context, s *store.Store, id string) (*model.UploadSession, error) {
	return s.GetUploadSession(ctx, id)
}

// fetchCASEntries returns all CAS entries.
func fetchCASEntries(ctx context.Context, s *store.Store) ([]*model.CASEntry, error) {
	return s.ListCASEntries(ctx)
}

// fetchQuotas returns all quotas.
func fetchQuotas(ctx context.Context, s *store.Store) ([]*model.Quota, error) {
	return s.ListQuotas(ctx)
}

// fetchBucketObjectCount returns the number of objects in a bucket.
func fetchBucketObjectCount(ctx context.Context, s *store.Store, bucketID string) int {
	objects, err := s.ListObjects(ctx, bucketID)
	if err != nil {
		return 0
	}
	return len(objects)
}

// fetchBucketTotalSize returns the total size of objects in a bucket.
func fetchBucketTotalSize(ctx context.Context, s *store.Store, bucketID string) int64 {
	objects, err := s.ListObjects(ctx, bucketID)
	if err != nil {
		return 0
	}
	var total int64
	for _, o := range objects {
		total += o.Size
	}
	return total
}
