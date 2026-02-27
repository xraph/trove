// Package store provides the Grove ORM-based store for Trove metadata.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/trove/extension/model"
)

// Store wraps a Grove DB for Trove metadata CRUD operations.
type Store struct {
	db *grove.DB
	pg *pgdriver.PgDB
}

// New creates a new Store from a Grove DB.
func New(db *grove.DB) *Store {
	s := &Store{db: db}
	// Try to unwrap PostgreSQL driver for direct access.
	if pg, ok := tryUnwrapPg(db); ok {
		s.pg = pg
	}
	return s
}

// DB returns the underlying Grove DB.
func (s *Store) DB() *grove.DB { return s.db }

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

// Close closes the store.
func (s *Store) Close() error { return s.db.Close() }

// --- Bucket Operations ---

// CreateBucket inserts a new bucket record.
func (s *Store) CreateBucket(ctx context.Context, b *model.Bucket) error {
	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	return s.insert(ctx, b)
}

// GetBucket retrieves a bucket by ID.
func (s *Store) GetBucket(ctx context.Context, id string) (*model.Bucket, error) {
	b := new(model.Bucket)
	if err := s.findByPK(ctx, b, id); err != nil {
		return nil, fmt.Errorf("store: get bucket %q: %w", id, err)
	}
	return b, nil
}

// GetBucketByName retrieves a bucket by name.
func (s *Store) GetBucketByName(ctx context.Context, name string) (*model.Bucket, error) {
	b := new(model.Bucket)
	if err := s.findByField(ctx, b, "name", name); err != nil {
		return nil, fmt.Errorf("store: get bucket by name %q: %w", name, err)
	}
	return b, nil
}

// ListBuckets returns all buckets, optionally filtered by tenant.
func (s *Store) ListBuckets(ctx context.Context, tenantKey string) ([]*model.Bucket, error) {
	var buckets []*model.Bucket
	if err := s.listByTenant(ctx, &buckets, "trove_buckets", tenantKey); err != nil {
		return nil, fmt.Errorf("store: list buckets: %w", err)
	}
	return buckets, nil
}

// DeleteBucket removes a bucket by ID.
func (s *Store) DeleteBucket(ctx context.Context, id string) error {
	return s.deleteByPK(ctx, &model.Bucket{}, id)
}

// --- Object Operations ---

// CreateObject inserts a new object record.
func (s *Store) CreateObject(ctx context.Context, o *model.Object) error {
	now := time.Now().UTC()
	o.CreatedAt = now
	o.UpdatedAt = now
	return s.insert(ctx, o)
}

// GetObject retrieves an object by ID.
func (s *Store) GetObject(ctx context.Context, id string) (*model.Object, error) {
	o := new(model.Object)
	if err := s.findByPK(ctx, o, id); err != nil {
		return nil, fmt.Errorf("store: get object %q: %w", id, err)
	}
	return o, nil
}

// GetObjectByKey retrieves an object by bucket ID and key.
func (s *Store) GetObjectByKey(ctx context.Context, bucketID, key string) (*model.Object, error) {
	o := new(model.Object)
	if err := s.findByFields(ctx, o, map[string]any{
		"bucket_id":  bucketID,
		"key":        key,
		"deleted_at": nil,
	}); err != nil {
		return nil, fmt.Errorf("store: get object by key %q/%q: %w", bucketID, key, err)
	}
	return o, nil
}

// ListObjects returns objects in a bucket, optionally filtered.
func (s *Store) ListObjects(ctx context.Context, bucketID string, opts ...ListOption) ([]*model.Object, error) {
	cfg := applyListOptions(opts)
	var objects []*model.Object
	if err := s.listObjects(ctx, &objects, bucketID, cfg); err != nil {
		return nil, fmt.Errorf("store: list objects: %w", err)
	}
	return objects, nil
}

// UpdateObject updates an object record.
func (s *Store) UpdateObject(ctx context.Context, o *model.Object) error {
	o.UpdatedAt = time.Now().UTC()
	return s.update(ctx, o)
}

// SoftDeleteObject marks an object as deleted.
func (s *Store) SoftDeleteObject(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return s.updateFields(ctx, &model.Object{}, id, map[string]any{
		"deleted_at": now,
		"updated_at": now,
	})
}

// --- Upload Session Operations ---

// CreateUploadSession inserts a new upload session.
func (s *Store) CreateUploadSession(ctx context.Context, u *model.UploadSession) error {
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	return s.insert(ctx, u)
}

// GetUploadSession retrieves an upload session by ID.
func (s *Store) GetUploadSession(ctx context.Context, id string) (*model.UploadSession, error) {
	u := new(model.UploadSession)
	if err := s.findByPK(ctx, u, id); err != nil {
		return nil, fmt.Errorf("store: get upload session %q: %w", id, err)
	}
	return u, nil
}

// UpdateUploadSession updates an upload session.
func (s *Store) UpdateUploadSession(ctx context.Context, u *model.UploadSession) error {
	u.UpdatedAt = time.Now().UTC()
	return s.update(ctx, u)
}

// ListExpiredUploads returns upload sessions past their expiry time.
func (s *Store) ListExpiredUploads(ctx context.Context) ([]*model.UploadSession, error) {
	var uploads []*model.UploadSession
	if err := s.listExpired(ctx, &uploads, "trove_upload_sessions", time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("store: list expired uploads: %w", err)
	}
	return uploads, nil
}

// --- CAS Index Operations ---

// PutCASEntry upserts a CAS entry.
func (s *Store) PutCASEntry(ctx context.Context, entry *model.CASEntry) error {
	entry.CreatedAt = time.Now().UTC()
	return s.upsert(ctx, entry, "hash")
}

// GetCASEntry retrieves a CAS entry by hash.
func (s *Store) GetCASEntry(ctx context.Context, hash string) (*model.CASEntry, error) {
	e := new(model.CASEntry)
	if err := s.findByPK(ctx, e, hash); err != nil {
		return nil, fmt.Errorf("store: get CAS entry %q: %w", hash, err)
	}
	return e, nil
}

// ListUnpinnedCAS returns CAS entries eligible for GC.
func (s *Store) ListUnpinnedCAS(ctx context.Context) ([]*model.CASEntry, error) {
	var entries []*model.CASEntry
	if err := s.listUnpinned(ctx, &entries); err != nil {
		return nil, fmt.Errorf("store: list unpinned CAS: %w", err)
	}
	return entries, nil
}

// IncrementCASRef increments the ref count for a CAS entry.
func (s *Store) IncrementCASRef(ctx context.Context, hash string) error {
	return s.incrementField(ctx, &model.CASEntry{}, hash, "ref_count")
}

// DecrementCASRef decrements the ref count for a CAS entry.
func (s *Store) DecrementCASRef(ctx context.Context, hash string) error {
	return s.decrementField(ctx, &model.CASEntry{}, hash, "ref_count")
}

// --- Quota Operations ---

// GetQuota retrieves quota info for a tenant.
func (s *Store) GetQuota(ctx context.Context, tenantKey string) (*model.Quota, error) {
	q := new(model.Quota)
	if err := s.findByPK(ctx, q, tenantKey); err != nil {
		return nil, fmt.Errorf("store: get quota %q: %w", tenantKey, err)
	}
	return q, nil
}

// UpdateQuotaUsage updates usage counters for a tenant.
func (s *Store) UpdateQuotaUsage(ctx context.Context, tenantKey string, deltaBytes, deltaObjects int64) error {
	return s.updateQuotaCounters(ctx, tenantKey, deltaBytes, deltaObjects)
}

// SetQuota creates or updates quota limits for a tenant.
func (s *Store) SetQuota(ctx context.Context, q *model.Quota) error {
	q.UpdatedAt = time.Now().UTC()
	return s.upsert(ctx, q, "tenant_key")
}

// --- List Options ---

// ListOption configures a list query.
type ListOption func(*listConfig)

type listConfig struct {
	prefix    string
	limit     int
	offset    int
	tenantKey string
}

// WithPrefix filters objects by key prefix.
func WithPrefix(prefix string) ListOption {
	return func(c *listConfig) { c.prefix = prefix }
}

// WithLimit sets the maximum number of results.
func WithLimit(n int) ListOption {
	return func(c *listConfig) { c.limit = n }
}

// WithOffset sets the pagination offset.
func WithOffset(n int) ListOption {
	return func(c *listConfig) { c.offset = n }
}

// WithTenantKey filters by tenant.
func WithTenantKey(key string) ListOption {
	return func(c *listConfig) { c.tenantKey = key }
}

func applyListOptions(opts []ListOption) listConfig {
	cfg := listConfig{limit: 100}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
