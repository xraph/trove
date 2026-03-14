// Package memory provides an in-memory implementation of store.Store for testing.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/trove/extension/model"
	"github.com/xraph/trove/extension/store"
)

// Compile-time interface check.
var _ store.Store = (*Store)(nil)

// Store is an in-memory implementation of store.Store.
type Store struct {
	mu         sync.RWMutex
	buckets    map[string]*model.Bucket
	objects    map[string]*model.Object
	uploads    map[string]*model.UploadSession
	casEntries map[string]*model.CASEntry
	quotas     map[string]*model.Quota
}

// New creates a new in-memory store.
func New() *Store {
	return &Store{
		buckets:    make(map[string]*model.Bucket),
		objects:    make(map[string]*model.Object),
		uploads:    make(map[string]*model.UploadSession),
		casEntries: make(map[string]*model.CASEntry),
		quotas:     make(map[string]*model.Quota),
	}
}

func (s *Store) DB() *grove.DB                                        { return nil }
func (s *Store) Ping(_ context.Context) error                         { return nil }
func (s *Store) Close() error                                         { return nil }
func (s *Store) Migrate(_ context.Context, _ ...*migrate.Group) error { return nil }

// --- Bucket Operations ---

func (s *Store) CreateBucket(_ context.Context, b *model.Bucket) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.buckets {
		if existing.Name == b.Name {
			return fmt.Errorf("store: bucket name %q already exists", b.Name)
		}
	}

	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	cp := *b
	s.buckets[b.ID] = &cp
	return nil
}

func (s *Store) GetBucket(_ context.Context, id string) (*model.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.buckets[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *b
	return &cp, nil
}

func (s *Store) GetBucketByName(_ context.Context, name string) (*model.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, b := range s.buckets {
		if b.Name == name {
			cp := *b
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) ListBuckets(_ context.Context, tenantKey string) ([]*model.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.Bucket
	for _, b := range s.buckets {
		if tenantKey != "" && b.TenantKey != tenantKey {
			continue
		}
		cp := *b
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (s *Store) UpdateBucket(_ context.Context, b *model.Bucket) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.buckets[b.ID]; !ok {
		return store.ErrNotFound
	}
	b.UpdatedAt = time.Now().UTC()
	cp := *b
	s.buckets[b.ID] = &cp
	return nil
}

func (s *Store) DeleteBucket(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.buckets[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.buckets, id)
	return nil
}

// --- Object Operations ---

func (s *Store) CreateObject(_ context.Context, o *model.Object) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	o.CreatedAt = now
	o.UpdatedAt = now
	cp := *o
	s.objects[o.ID] = &cp
	return nil
}

func (s *Store) GetObject(_ context.Context, id string) (*model.Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	o, ok := s.objects[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (s *Store) GetObjectByKey(_ context.Context, bucketID, key string) (*model.Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, o := range s.objects {
		if o.BucketID == bucketID && o.Key == key && o.DeletedAt == nil {
			cp := *o
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *Store) ListObjects(_ context.Context, bucketID string, opts ...store.ListOption) ([]*model.Object, error) {
	cfg := store.ApplyListOptions(opts)
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.Object
	for _, o := range s.objects {
		if o.BucketID != bucketID || o.DeletedAt != nil {
			continue
		}
		if cfg.Prefix != "" && !strings.HasPrefix(o.Key, cfg.Prefix) {
			continue
		}
		if cfg.TenantKey != "" && o.TenantKey != cfg.TenantKey {
			continue
		}
		cp := *o
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})

	// Apply offset and limit.
	if cfg.Offset > 0 && cfg.Offset < len(result) {
		result = result[cfg.Offset:]
	} else if cfg.Offset >= len(result) {
		return nil, nil
	}
	if cfg.Limit > 0 && cfg.Limit < len(result) {
		result = result[:cfg.Limit]
	}

	return result, nil
}

func (s *Store) ListAllObjects(_ context.Context, limit int) ([]*model.Object, error) {
	if limit <= 0 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.Object
	for _, o := range s.objects {
		if o.DeletedAt != nil {
			continue
		}
		cp := *o
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) UpdateObject(_ context.Context, o *model.Object) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.objects[o.ID]; !ok {
		return store.ErrNotFound
	}
	o.UpdatedAt = time.Now().UTC()
	cp := *o
	s.objects[o.ID] = &cp
	return nil
}

func (s *Store) SoftDeleteObject(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	o, ok := s.objects[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now().UTC()
	o.DeletedAt = &now
	o.UpdatedAt = now
	return nil
}

// --- Upload Session Operations ---

func (s *Store) CreateUploadSession(_ context.Context, u *model.UploadSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	cp := *u
	s.uploads[u.ID] = &cp
	return nil
}

func (s *Store) GetUploadSession(_ context.Context, id string) (*model.UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, ok := s.uploads[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (s *Store) UpdateUploadSession(_ context.Context, u *model.UploadSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.uploads[u.ID]; !ok {
		return store.ErrNotFound
	}
	u.UpdatedAt = time.Now().UTC()
	cp := *u
	s.uploads[u.ID] = &cp
	return nil
}

func (s *Store) ListUploads(_ context.Context) ([]*model.UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.UploadSession
	for _, u := range s.uploads {
		cp := *u
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (s *Store) ListExpiredUploads(_ context.Context) ([]*model.UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	var result []*model.UploadSession
	for _, u := range s.uploads {
		if u.ExpiresAt.Before(now) && (u.Status == model.UploadStatusPending || u.Status == model.UploadStatusActive) {
			cp := *u
			result = append(result, &cp)
		}
	}
	return result, nil
}

// --- CAS Index Operations ---

func (s *Store) PutCASEntry(_ context.Context, entry *model.CASEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry.CreatedAt = time.Now().UTC()
	cp := *entry
	s.casEntries[entry.Hash] = &cp
	return nil
}

func (s *Store) GetCASEntry(_ context.Context, hash string) (*model.CASEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.casEntries[hash]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *e
	return &cp, nil
}

func (s *Store) ListCASEntries(_ context.Context) ([]*model.CASEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.CASEntry
	for _, e := range s.casEntries {
		cp := *e
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (s *Store) ListUnpinnedCAS(_ context.Context) ([]*model.CASEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.CASEntry
	for _, e := range s.casEntries {
		if !e.Pinned && e.RefCount == 0 {
			cp := *e
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *Store) IncrementCASRef(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.casEntries[hash]
	if !ok {
		return store.ErrNotFound
	}
	e.RefCount++
	return nil
}

func (s *Store) DecrementCASRef(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.casEntries[hash]
	if !ok {
		return store.ErrNotFound
	}
	if e.RefCount > 0 {
		e.RefCount--
	}
	return nil
}

func (s *Store) SetCASPinned(_ context.Context, hash string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.casEntries[hash]
	if !ok {
		return store.ErrNotFound
	}
	e.Pinned = pinned
	return nil
}

func (s *Store) DeleteCASEntry(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.casEntries, hash)
	return nil
}

// --- Quota Operations ---

func (s *Store) ListQuotas(_ context.Context) ([]*model.Quota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*model.Quota
	for _, q := range s.quotas {
		cp := *q
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func (s *Store) GetQuota(_ context.Context, tenantKey string) (*model.Quota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q, ok := s.quotas[tenantKey]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *q
	return &cp, nil
}

func (s *Store) UpdateQuotaUsage(_ context.Context, tenantKey string, deltaBytes, deltaObjects int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, ok := s.quotas[tenantKey]
	if !ok {
		return store.ErrNotFound
	}
	q.UsedBytes += deltaBytes
	q.ObjectCount += deltaObjects
	q.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) SetQuota(_ context.Context, q *model.Quota) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	q.UpdatedAt = time.Now().UTC()
	cp := *q
	s.quotas[q.TenantKey] = &cp
	return nil
}

func (s *Store) DeleteQuota(_ context.Context, tenantKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.quotas, tenantKey)
	return nil
}
