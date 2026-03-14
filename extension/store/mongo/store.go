// Package mongo implements the Trove store interface using MongoDB via Grove ORM.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/trove/extension/model"
	"github.com/xraph/trove/extension/store"
)

// Collection name constants.
const (
	colBuckets = "trove_buckets"
	colObjects = "trove_objects"
	colUploads = "trove_upload_sessions"
	colCAS     = "trove_cas_index"
	colQuotas  = "trove_quotas"
)

// Compile-time interface check.
var _ store.Store = (*Store)(nil)

// Store implements store.Store using MongoDB via Grove ORM.
type Store struct {
	db  *grove.DB
	mdb *mongodriver.MongoDB
}

// New creates a new MongoDB store backed by Grove ORM.
func New(db *grove.DB) *Store {
	return &Store{
		db:  db,
		mdb: mongodriver.Unwrap(db),
	}
}

// DB returns the underlying grove database for direct access.
func (s *Store) DB() *grove.DB { return s.db }

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Migrate creates indexes for all trove collections.
func (s *Store) Migrate(ctx context.Context, _ ...*migrate.Group) error {
	indexes := migrationIndexes()

	for col, models := range indexes {
		if len(models) == 0 {
			continue
		}
		_, err := s.mdb.Collection(col).Indexes().CreateMany(ctx, models)
		if err != nil {
			return fmt.Errorf("trove/mongo: migrate %s indexes: %w", col, err)
		}
	}

	return nil
}

// isNoDocuments checks if an error wraps mongo.ErrNoDocuments.
func isNoDocuments(err error) bool {
	return errors.Is(err, mongo.ErrNoDocuments)
}

// now returns the current UTC time.
func now() time.Time {
	return time.Now().UTC()
}

// --- Bucket Operations ---

func (s *Store) CreateBucket(ctx context.Context, b *model.Bucket) error {
	b.CreatedAt = now()
	b.UpdatedAt = now()
	_, err := s.mdb.NewInsert(b).Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: create bucket: %w", err)
	}
	return nil
}

func (s *Store) GetBucket(ctx context.Context, id string) (*model.Bucket, error) {
	var b model.Bucket
	err := s.mdb.NewFind(&b).
		Filter(bson.M{"_id": id}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get bucket: %w", err)
	}
	return &b, nil
}

func (s *Store) GetBucketByName(ctx context.Context, name string) (*model.Bucket, error) {
	var b model.Bucket
	err := s.mdb.NewFind(&b).
		Filter(bson.M{"name": name}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get bucket by name: %w", err)
	}
	return &b, nil
}

func (s *Store) ListBuckets(ctx context.Context, tenantKey string) ([]*model.Bucket, error) {
	var buckets []*model.Bucket
	filter := bson.M{}
	if tenantKey != "" {
		filter["tenant_key"] = tenantKey
	}
	err := s.mdb.NewFind(&buckets).
		Filter(filter).
		Sort(bson.D{{Key: "created_at", Value: -1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list buckets: %w", err)
	}
	return buckets, nil
}

func (s *Store) UpdateBucket(ctx context.Context, b *model.Bucket) error {
	b.UpdatedAt = now()
	res, err := s.mdb.NewUpdate(b).
		Filter(bson.M{"_id": b.ID}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: update bucket: %w", err)
	}
	if res.MatchedCount() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteBucket(ctx context.Context, id string) error {
	res, err := s.mdb.NewDelete((*model.Bucket)(nil)).
		Filter(bson.M{"_id": id}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: delete bucket: %w", err)
	}
	if res.DeletedCount() == 0 {
		return store.ErrNotFound
	}
	return nil
}

// --- Object Operations ---

func (s *Store) CreateObject(ctx context.Context, o *model.Object) error {
	o.CreatedAt = now()
	o.UpdatedAt = now()
	_, err := s.mdb.NewInsert(o).Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: create object: %w", err)
	}
	return nil
}

func (s *Store) GetObject(ctx context.Context, id string) (*model.Object, error) {
	var o model.Object
	err := s.mdb.NewFind(&o).
		Filter(bson.M{"_id": id}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get object: %w", err)
	}
	return &o, nil
}

func (s *Store) GetObjectByKey(ctx context.Context, bucketID, key string) (*model.Object, error) {
	var o model.Object
	err := s.mdb.NewFind(&o).
		Filter(bson.M{
			"bucket_id":  bucketID,
			"key":        key,
			"deleted_at": nil,
		}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get object by key: %w", err)
	}
	return &o, nil
}

func (s *Store) ListObjects(ctx context.Context, bucketID string, opts ...store.ListOption) ([]*model.Object, error) {
	cfg := store.ApplyListOptions(opts)
	filter := bson.M{
		"bucket_id":  bucketID,
		"deleted_at": nil,
	}
	if cfg.Prefix != "" {
		filter["key"] = bson.M{"$regex": "^" + cfg.Prefix}
	}
	if cfg.TenantKey != "" {
		filter["tenant_key"] = cfg.TenantKey
	}

	var objects []*model.Object
	q := s.mdb.NewFind(&objects).
		Filter(filter).
		Sort(bson.D{{Key: "key", Value: 1}}).
		Limit(int64(cfg.Limit))
	if cfg.Offset > 0 {
		q = q.Skip(int64(cfg.Offset))
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("trove/mongo: list objects: %w", err)
	}
	return objects, nil
}

func (s *Store) ListAllObjects(ctx context.Context, limit int) ([]*model.Object, error) {
	if limit <= 0 {
		limit = 100
	}
	var objects []*model.Object
	err := s.mdb.NewFind(&objects).
		Filter(bson.M{"deleted_at": nil}).
		Sort(bson.D{{Key: "created_at", Value: -1}}).
		Limit(int64(limit)).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list all objects: %w", err)
	}
	return objects, nil
}

func (s *Store) UpdateObject(ctx context.Context, o *model.Object) error {
	o.UpdatedAt = now()
	res, err := s.mdb.NewUpdate(o).
		Filter(bson.M{"_id": o.ID}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: update object: %w", err)
	}
	if res.MatchedCount() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) SoftDeleteObject(ctx context.Context, id string) error {
	t := now()
	_, err := s.mdb.Collection(colObjects).UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"deleted_at": t, "updated_at": t}},
	)
	return err
}

// --- Upload Session Operations ---

func (s *Store) CreateUploadSession(ctx context.Context, u *model.UploadSession) error {
	u.CreatedAt = now()
	u.UpdatedAt = now()
	_, err := s.mdb.NewInsert(u).Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: create upload session: %w", err)
	}
	return nil
}

func (s *Store) GetUploadSession(ctx context.Context, id string) (*model.UploadSession, error) {
	var u model.UploadSession
	err := s.mdb.NewFind(&u).
		Filter(bson.M{"_id": id}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get upload session: %w", err)
	}
	return &u, nil
}

func (s *Store) UpdateUploadSession(ctx context.Context, u *model.UploadSession) error {
	u.UpdatedAt = now()
	res, err := s.mdb.NewUpdate(u).
		Filter(bson.M{"_id": u.ID}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("trove/mongo: update upload session: %w", err)
	}
	if res.MatchedCount() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUploads(ctx context.Context) ([]*model.UploadSession, error) {
	var uploads []*model.UploadSession
	err := s.mdb.NewFind(&uploads).
		Sort(bson.D{{Key: "created_at", Value: -1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list uploads: %w", err)
	}
	return uploads, nil
}

func (s *Store) ListExpiredUploads(ctx context.Context) ([]*model.UploadSession, error) {
	var uploads []*model.UploadSession
	err := s.mdb.NewFind(&uploads).
		Filter(bson.M{
			"expires_at": bson.M{"$lt": now()},
			"status":     bson.M{"$in": []string{string(model.UploadStatusPending), string(model.UploadStatusActive)}},
		}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list expired uploads: %w", err)
	}
	return uploads, nil
}

// --- CAS Index Operations ---

func (s *Store) PutCASEntry(ctx context.Context, entry *model.CASEntry) error {
	entry.CreatedAt = now()
	_, err := s.mdb.Collection(colCAS).UpdateOne(ctx,
		bson.M{"_id": entry.Hash},
		bson.M{"$set": entry},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (s *Store) GetCASEntry(ctx context.Context, hash string) (*model.CASEntry, error) {
	var e model.CASEntry
	err := s.mdb.NewFind(&e).
		Filter(bson.M{"_id": hash}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get CAS entry: %w", err)
	}
	return &e, nil
}

func (s *Store) ListCASEntries(ctx context.Context) ([]*model.CASEntry, error) {
	var entries []*model.CASEntry
	err := s.mdb.NewFind(&entries).
		Sort(bson.D{{Key: "created_at", Value: -1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list CAS entries: %w", err)
	}
	return entries, nil
}

func (s *Store) ListUnpinnedCAS(ctx context.Context) ([]*model.CASEntry, error) {
	var entries []*model.CASEntry
	err := s.mdb.NewFind(&entries).
		Filter(bson.M{
			"pinned":    false,
			"ref_count": 0,
		}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list unpinned CAS: %w", err)
	}
	return entries, nil
}

func (s *Store) IncrementCASRef(ctx context.Context, hash string) error {
	_, err := s.mdb.Collection(colCAS).UpdateOne(ctx,
		bson.M{"_id": hash},
		bson.M{"$inc": bson.M{"ref_count": 1}},
	)
	return err
}

func (s *Store) DecrementCASRef(ctx context.Context, hash string) error {
	// First increment by -1, then ensure non-negative via a second update.
	_, err := s.mdb.Collection(colCAS).UpdateOne(ctx,
		bson.M{"_id": hash, "ref_count": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"ref_count": -1}},
	)
	return err
}

func (s *Store) SetCASPinned(ctx context.Context, hash string, pinned bool) error {
	_, err := s.mdb.Collection(colCAS).UpdateOne(ctx,
		bson.M{"_id": hash},
		bson.M{"$set": bson.M{"pinned": pinned}},
	)
	return err
}

func (s *Store) DeleteCASEntry(ctx context.Context, hash string) error {
	_, err := s.mdb.Collection(colCAS).DeleteOne(ctx, bson.M{"_id": hash})
	return err
}

// --- Quota Operations ---

func (s *Store) ListQuotas(ctx context.Context) ([]*model.Quota, error) {
	var quotas []*model.Quota
	err := s.mdb.NewFind(&quotas).
		Sort(bson.D{{Key: "updated_at", Value: -1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("trove/mongo: list quotas: %w", err)
	}
	return quotas, nil
}

func (s *Store) GetQuota(ctx context.Context, tenantKey string) (*model.Quota, error) {
	var q model.Quota
	err := s.mdb.NewFind(&q).
		Filter(bson.M{"_id": tenantKey}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("trove/mongo: get quota: %w", err)
	}
	return &q, nil
}

func (s *Store) UpdateQuotaUsage(ctx context.Context, tenantKey string, deltaBytes, deltaObjects int64) error {
	_, err := s.mdb.Collection(colQuotas).UpdateOne(ctx,
		bson.M{"_id": tenantKey},
		bson.M{
			"$inc": bson.M{
				"used_bytes":   deltaBytes,
				"object_count": deltaObjects,
			},
			"$set": bson.M{"updated_at": now()},
		},
	)
	return err
}

func (s *Store) SetQuota(ctx context.Context, q *model.Quota) error {
	q.UpdatedAt = now()
	_, err := s.mdb.Collection(colQuotas).UpdateOne(ctx,
		bson.M{"_id": q.TenantKey},
		bson.M{"$set": q},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (s *Store) DeleteQuota(ctx context.Context, tenantKey string) error {
	_, err := s.mdb.Collection(colQuotas).DeleteOne(ctx, bson.M{"_id": tenantKey})
	return err
}

// migrationIndexes returns the index definitions for all trove collections.
func migrationIndexes() map[string][]mongo.IndexModel {
	return map[string][]mongo.IndexModel{
		colBuckets: {
			{
				Keys:    bson.D{{Key: "name", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "tenant_key", Value: 1}}},
		},
		colObjects: {
			{
				Keys:    bson.D{{Key: "bucket_id", Value: 1}, {Key: "key", Value: 1}},
				Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"deleted_at": nil}),
			},
			{Keys: bson.D{{Key: "tenant_key", Value: 1}}},
			{Keys: bson.D{{Key: "bucket_id", Value: 1}, {Key: "created_at", Value: -1}}},
			{Keys: bson.D{{Key: "deleted_at", Value: 1}}},
		},
		colUploads: {
			{Keys: bson.D{{Key: "bucket_id", Value: 1}}},
			{Keys: bson.D{{Key: "expires_at", Value: 1}, {Key: "status", Value: 1}}},
		},
		colCAS: {
			{Keys: bson.D{{Key: "pinned", Value: 1}, {Key: "ref_count", Value: 1}}},
		},
		colQuotas: {},
	}
}
