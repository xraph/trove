// Package sqlite implements the Trove store interface using grove ORM (SQLite).
package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/trove/extension/model"
	"github.com/xraph/trove/extension/store"
)

// Compile-time interface check.
var _ store.Store = (*Store)(nil)

// Store implements store.Store using grove ORM (SQLite).
type Store struct {
	db  *grove.DB
	sdb *sqlitedriver.SqliteDB
}

// New creates a new SQLite-backed store.
func New(db *grove.DB) *Store {
	return &Store{
		db:  db,
		sdb: sqlitedriver.Unwrap(db),
	}
}

// DB returns the underlying grove.DB.
func (s *Store) DB() *grove.DB { return s.db }

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Migrate runs all registered migrations via the grove orchestrator.
func (s *Store) Migrate(ctx context.Context, extraGroups ...*migrate.Group) error {
	executor, err := migrate.NewExecutorFor(s.sdb)
	if err != nil {
		return fmt.Errorf("trove/sqlite: create migration executor: %w", err)
	}

	groups := make([]*migrate.Group, 0, 1+len(extraGroups))
	groups = append(groups, Migrations)
	groups = append(groups, extraGroups...)

	orch := migrate.NewOrchestrator(executor, groups...)
	if _, err := orch.Migrate(ctx); err != nil {
		return fmt.Errorf("trove/sqlite: migration failed: %w", err)
	}

	return nil
}

// --- Bucket Operations ---

func (s *Store) CreateBucket(ctx context.Context, b *model.Bucket) error {
	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	_, err := s.sdb.NewInsert(b).Exec(ctx)
	return err
}

func (s *Store) GetBucket(ctx context.Context, id string) (*model.Bucket, error) {
	b := new(model.Bucket)
	if err := s.sdb.NewSelect(b).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: get bucket %q: %w", id, err)
	}
	return b, nil
}

func (s *Store) GetBucketByName(ctx context.Context, name string) (*model.Bucket, error) {
	b := new(model.Bucket)
	if err := s.sdb.NewSelect(b).Where("name = ?", name).Limit(1).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: get bucket by name %q: %w", name, err)
	}
	return b, nil
}

func (s *Store) ListBuckets(ctx context.Context, tenantKey string) ([]*model.Bucket, error) {
	var buckets []*model.Bucket
	q := s.sdb.NewSelect(&buckets)
	if tenantKey != "" {
		q = q.Where("tenant_key = ?", tenantKey)
	}
	q = q.OrderExpr("created_at DESC")
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: list buckets: %w", err)
	}
	return buckets, nil
}

func (s *Store) UpdateBucket(ctx context.Context, b *model.Bucket) error {
	b.UpdatedAt = time.Now().UTC()
	_, err := s.sdb.NewUpdate(b).WherePK().Exec(ctx)
	return err
}

func (s *Store) DeleteBucket(ctx context.Context, id string) error {
	_, err := s.sdb.NewDelete(&model.Bucket{}).Where("id = ?", id).Exec(ctx)
	return err
}

// --- Object Operations ---

func (s *Store) CreateObject(ctx context.Context, o *model.Object) error {
	now := time.Now().UTC()
	o.CreatedAt = now
	o.UpdatedAt = now
	_, err := s.sdb.NewInsert(o).Exec(ctx)
	return err
}

func (s *Store) GetObject(ctx context.Context, id string) (*model.Object, error) {
	o := new(model.Object)
	if err := s.sdb.NewSelect(o).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: get object %q: %w", id, err)
	}
	return o, nil
}

func (s *Store) GetObjectByKey(ctx context.Context, bucketID, key string) (*model.Object, error) {
	o := new(model.Object)
	err := s.sdb.NewSelect(o).
		Where("bucket_id = ?", bucketID).
		Where("key = ?", key).
		Where("deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: get object by key %q/%q: %w", bucketID, key, err)
	}
	return o, nil
}

func (s *Store) ListObjects(ctx context.Context, bucketID string, opts ...store.ListOption) ([]*model.Object, error) {
	cfg := store.ApplyListOptions(opts)
	var objects []*model.Object
	q := s.sdb.NewSelect(&objects).
		Where("bucket_id = ?", bucketID).
		Where("deleted_at IS NULL")
	if cfg.Prefix != "" {
		q = q.Where("key LIKE ?", cfg.Prefix+"%")
	}
	if cfg.TenantKey != "" {
		q = q.Where("tenant_key = ?", cfg.TenantKey)
	}
	q = q.OrderExpr("key ASC").Limit(cfg.Limit).Offset(cfg.Offset)
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: list objects: %w", err)
	}
	return objects, nil
}

func (s *Store) ListAllObjects(ctx context.Context, limit int) ([]*model.Object, error) {
	if limit <= 0 {
		limit = 100
	}
	var objects []*model.Object
	err := s.sdb.NewSelect(&objects).
		Where("deleted_at IS NULL").
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list all objects: %w", err)
	}
	return objects, nil
}

func (s *Store) UpdateObject(ctx context.Context, o *model.Object) error {
	o.UpdatedAt = time.Now().UTC()
	_, err := s.sdb.NewUpdate(o).WherePK().Exec(ctx)
	return err
}

func (s *Store) SoftDeleteObject(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.sdb.NewUpdate(&model.Object{}).
		Where("id = ?", id).
		Set("deleted_at = ?", now).
		Set("updated_at = ?", now).
		Exec(ctx)
	return err
}

// --- Upload Session Operations ---

func (s *Store) CreateUploadSession(ctx context.Context, u *model.UploadSession) error {
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	_, err := s.sdb.NewInsert(u).Exec(ctx)
	return err
}

func (s *Store) GetUploadSession(ctx context.Context, id string) (*model.UploadSession, error) {
	u := new(model.UploadSession)
	if err := s.sdb.NewSelect(u).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: get upload session %q: %w", id, err)
	}
	return u, nil
}

func (s *Store) UpdateUploadSession(ctx context.Context, u *model.UploadSession) error {
	u.UpdatedAt = time.Now().UTC()
	_, err := s.sdb.NewUpdate(u).WherePK().Exec(ctx)
	return err
}

func (s *Store) ListUploads(ctx context.Context) ([]*model.UploadSession, error) {
	var uploads []*model.UploadSession
	if err := s.sdb.NewSelect(&uploads).OrderExpr("created_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: list uploads: %w", err)
	}
	return uploads, nil
}

func (s *Store) ListExpiredUploads(ctx context.Context) ([]*model.UploadSession, error) {
	var uploads []*model.UploadSession
	err := s.sdb.NewSelect(&uploads).
		Where("expires_at < ?", time.Now().UTC()).
		Where("status IN (?, ?)", model.UploadStatusPending, model.UploadStatusActive).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list expired uploads: %w", err)
	}
	return uploads, nil
}

// --- CAS Index Operations ---

func (s *Store) PutCASEntry(ctx context.Context, entry *model.CASEntry) error {
	entry.CreatedAt = time.Now().UTC()
	_, err := s.sdb.NewInsert(entry).
		OnConflict("(hash) DO UPDATE").
		Exec(ctx)
	return err
}

func (s *Store) GetCASEntry(ctx context.Context, hash string) (*model.CASEntry, error) {
	e := new(model.CASEntry)
	if err := s.sdb.NewSelect(e).Where("hash = ?", hash).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: get CAS entry %q: %w", hash, err)
	}
	return e, nil
}

func (s *Store) ListCASEntries(ctx context.Context) ([]*model.CASEntry, error) {
	var entries []*model.CASEntry
	if err := s.sdb.NewSelect(&entries).OrderExpr("created_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: list CAS entries: %w", err)
	}
	return entries, nil
}

func (s *Store) ListUnpinnedCAS(ctx context.Context) ([]*model.CASEntry, error) {
	var entries []*model.CASEntry
	err := s.sdb.NewSelect(&entries).
		Where("pinned = ?", false).
		Where("ref_count = ?", 0).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list unpinned CAS: %w", err)
	}
	return entries, nil
}

func (s *Store) IncrementCASRef(ctx context.Context, hash string) error {
	_, err := s.sdb.NewUpdate(&model.CASEntry{}).
		Where("hash = ?", hash).
		Set("ref_count = ref_count + 1").
		Exec(ctx)
	return err
}

func (s *Store) DecrementCASRef(ctx context.Context, hash string) error {
	_, err := s.sdb.NewUpdate(&model.CASEntry{}).
		Where("hash = ?", hash).
		Set("ref_count = MAX(ref_count - 1, 0)").
		Exec(ctx)
	return err
}

func (s *Store) SetCASPinned(ctx context.Context, hash string, pinned bool) error {
	_, err := s.sdb.NewUpdate(&model.CASEntry{}).
		Where("hash = ?", hash).
		Set("pinned = ?", pinned).
		Exec(ctx)
	return err
}

func (s *Store) DeleteCASEntry(ctx context.Context, hash string) error {
	_, err := s.sdb.NewDelete(&model.CASEntry{}).Where("hash = ?", hash).Exec(ctx)
	return err
}

// --- Quota Operations ---

func (s *Store) ListQuotas(ctx context.Context) ([]*model.Quota, error) {
	var quotas []*model.Quota
	if err := s.sdb.NewSelect(&quotas).OrderExpr("updated_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: list quotas: %w", err)
	}
	return quotas, nil
}

func (s *Store) GetQuota(ctx context.Context, tenantKey string) (*model.Quota, error) {
	q := new(model.Quota)
	if err := s.sdb.NewSelect(q).Where("tenant_key = ?", tenantKey).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store: get quota %q: %w", tenantKey, err)
	}
	return q, nil
}

func (s *Store) UpdateQuotaUsage(ctx context.Context, tenantKey string, deltaBytes, deltaObjects int64) error {
	_, err := s.sdb.NewUpdate((*model.Quota)(nil)).
		Where("tenant_key = ?", tenantKey).
		Set("used_bytes = used_bytes + ?", deltaBytes).
		Set("object_count = object_count + ?", deltaObjects).
		Set("updated_at = ?", time.Now().UTC()).
		Exec(ctx)
	return err
}

func (s *Store) SetQuota(ctx context.Context, q *model.Quota) error {
	q.UpdatedAt = time.Now().UTC()
	_, err := s.sdb.NewInsert(q).
		OnConflict("(tenant_key) DO UPDATE").
		Exec(ctx)
	return err
}

func (s *Store) DeleteQuota(ctx context.Context, tenantKey string) error {
	_, err := s.sdb.NewDelete(&model.Quota{}).Where("tenant_key = ?", tenantKey).Exec(ctx)
	return err
}
