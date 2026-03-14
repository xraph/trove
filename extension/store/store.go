// Package store defines the aggregate persistence interface for Trove metadata.
// Backends: PostgreSQL, SQLite, MongoDB, and Memory.
package store

import (
	"context"
	"errors"

	"github.com/xraph/grove"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/trove/extension/model"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("trove: not found")

// Store is the aggregate persistence interface for Trove metadata.
// A single backend (postgres, sqlite, mongo, memory) implements all methods.
type Store interface {
	// DB returns the underlying Grove DB (nil for memory store).
	DB() *grove.DB

	// Ping checks database connectivity.
	Ping(ctx context.Context) error

	// Close closes the store connection.
	Close() error

	// Migrate runs all schema migrations.
	Migrate(ctx context.Context, extraGroups ...*migrate.Group) error

	// --- Bucket Operations ---

	CreateBucket(ctx context.Context, b *model.Bucket) error
	GetBucket(ctx context.Context, id string) (*model.Bucket, error)
	GetBucketByName(ctx context.Context, name string) (*model.Bucket, error)
	ListBuckets(ctx context.Context, tenantKey string) ([]*model.Bucket, error)
	UpdateBucket(ctx context.Context, b *model.Bucket) error
	DeleteBucket(ctx context.Context, id string) error

	// --- Object Operations ---

	CreateObject(ctx context.Context, o *model.Object) error
	GetObject(ctx context.Context, id string) (*model.Object, error)
	GetObjectByKey(ctx context.Context, bucketID, key string) (*model.Object, error)
	ListObjects(ctx context.Context, bucketID string, opts ...ListOption) ([]*model.Object, error)
	ListAllObjects(ctx context.Context, limit int) ([]*model.Object, error)
	UpdateObject(ctx context.Context, o *model.Object) error
	SoftDeleteObject(ctx context.Context, id string) error

	// --- Upload Session Operations ---

	CreateUploadSession(ctx context.Context, u *model.UploadSession) error
	GetUploadSession(ctx context.Context, id string) (*model.UploadSession, error)
	UpdateUploadSession(ctx context.Context, u *model.UploadSession) error
	ListUploads(ctx context.Context) ([]*model.UploadSession, error)
	ListExpiredUploads(ctx context.Context) ([]*model.UploadSession, error)

	// --- CAS Index Operations ---

	PutCASEntry(ctx context.Context, entry *model.CASEntry) error
	GetCASEntry(ctx context.Context, hash string) (*model.CASEntry, error)
	ListCASEntries(ctx context.Context) ([]*model.CASEntry, error)
	ListUnpinnedCAS(ctx context.Context) ([]*model.CASEntry, error)
	IncrementCASRef(ctx context.Context, hash string) error
	DecrementCASRef(ctx context.Context, hash string) error
	SetCASPinned(ctx context.Context, hash string, pinned bool) error
	DeleteCASEntry(ctx context.Context, hash string) error

	// --- Quota Operations ---

	ListQuotas(ctx context.Context) ([]*model.Quota, error)
	GetQuota(ctx context.Context, tenantKey string) (*model.Quota, error)
	UpdateQuotaUsage(ctx context.Context, tenantKey string, deltaBytes, deltaObjects int64) error
	SetQuota(ctx context.Context, q *model.Quota) error
	DeleteQuota(ctx context.Context, tenantKey string) error
}

// --- List Options ---

// ListOption configures a list query.
type ListOption func(*ListConfig)

// ListConfig holds list query parameters.
type ListConfig struct {
	Prefix    string
	Limit     int
	Offset    int
	TenantKey string
}

// WithPrefix filters objects by key prefix.
func WithPrefix(prefix string) ListOption {
	return func(c *ListConfig) { c.Prefix = prefix }
}

// WithLimit sets the maximum number of results.
func WithLimit(n int) ListOption {
	return func(c *ListConfig) { c.Limit = n }
}

// WithOffset sets the pagination offset.
func WithOffset(n int) ListOption {
	return func(c *ListConfig) { c.Offset = n }
}

// WithTenantKey filters by tenant.
func WithTenantKey(key string) ListOption {
	return func(c *ListConfig) { c.TenantKey = key }
}

// ApplyListOptions applies list options to a default config.
func ApplyListOptions(opts []ListOption) ListConfig {
	cfg := ListConfig{Limit: 100}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
