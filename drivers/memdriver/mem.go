// Package memdriver provides an in-memory storage driver for testing.
//
// The memory driver stores all data in-process using Go maps. It is not
// persistent and is intended exclusively for unit tests and development.
//
// Usage:
//
//	drv := memdriver.New()
//	t, err := trove.Open(drv)
package memdriver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/trove/driver"
)

func init() {
	driver.Register("mem", func() driver.Driver { return New() })
}

// Compile-time interface check.
var _ driver.Driver = (*MemDriver)(nil)

type memObject struct {
	data []byte
	info driver.ObjectInfo
}

// MemDriver implements driver.Driver with in-memory storage.
type MemDriver struct {
	mu      sync.RWMutex
	buckets map[string]map[string]*memObject
	created map[string]time.Time // bucket creation times
	closed  bool
}

// New creates a new in-memory storage driver.
func New() *MemDriver {
	return &MemDriver{
		buckets: make(map[string]map[string]*memObject),
		created: make(map[string]time.Time),
	}
}

// Name returns "mem".
func (d *MemDriver) Name() string { return "mem" }

// Open is a no-op for the memory driver (no DSN required).
func (d *MemDriver) Open(_ context.Context, _ string, _ ...driver.Option) error {
	return nil
}

// Close marks the driver as closed.
func (d *MemDriver) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Ping verifies the driver is open.
func (d *MemDriver) Ping(_ context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return fmt.Errorf("memdriver: driver is closed")
	}
	return nil
}

// Put stores an object in memory.
func (d *MemDriver) Put(_ context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyPutOptions(opts...)

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("memdriver: read data: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, fmt.Errorf("memdriver: driver is closed")
	}

	objects, ok := d.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("memdriver: bucket %q not found", bucket)
	}

	ct := cfg.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	meta := cfg.Metadata
	if meta == nil {
		meta = make(map[string]string)
	}

	now := time.Now().UTC()
	info := driver.ObjectInfo{
		Key:          key,
		Size:         int64(len(data)),
		ContentType:  ct,
		ETag:         fmt.Sprintf("%x", len(data)),
		LastModified: now,
		Metadata:     meta,
		StorageClass: cfg.StorageClass,
	}

	objects[key] = &memObject{data: data, info: info}

	return &info, nil
}

// Get retrieves an object from memory.
func (d *MemDriver) Get(_ context.Context, bucket, key string, _ ...driver.GetOption) (*driver.ObjectReader, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, fmt.Errorf("memdriver: driver is closed")
	}

	objects, ok := d.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("memdriver: bucket %q not found", bucket)
	}

	obj, ok := objects[key]
	if !ok {
		return nil, fmt.Errorf("memdriver: object %q not found in bucket %q", key, bucket)
	}

	// Copy data so callers don't hold the lock.
	dataCopy := make([]byte, len(obj.data))
	copy(dataCopy, obj.data)
	infoCopy := obj.info

	return &driver.ObjectReader{
		ReadCloser: io.NopCloser(bytes.NewReader(dataCopy)),
		Info:       &infoCopy,
	}, nil
}

// Delete removes an object from memory.
func (d *MemDriver) Delete(_ context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return fmt.Errorf("memdriver: driver is closed")
	}

	objects, ok := d.buckets[bucket]
	if !ok {
		return fmt.Errorf("memdriver: bucket %q not found", bucket)
	}

	if _, ok := objects[key]; !ok {
		return nil // Delete is idempotent
	}

	delete(objects, key)
	return nil
}

// Head returns object metadata without content.
func (d *MemDriver) Head(_ context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, fmt.Errorf("memdriver: driver is closed")
	}

	objects, ok := d.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("memdriver: bucket %q not found", bucket)
	}

	obj, ok := objects[key]
	if !ok {
		return nil, fmt.Errorf("memdriver: object %q not found in bucket %q", key, bucket)
	}

	infoCopy := obj.info
	return &infoCopy, nil
}

// List returns objects matching the given options.
func (d *MemDriver) List(_ context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	cfg := driver.ApplyListOptions(opts...)

	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, fmt.Errorf("memdriver: driver is closed")
	}

	objects, ok := d.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("memdriver: bucket %q not found", bucket)
	}

	// Collect keys sorted alphabetically.
	keys := make([]string, 0, len(objects))
	for k := range objects {
		if cfg.Prefix != "" && !strings.HasPrefix(k, cfg.Prefix) {
			continue
		}
		if cfg.Cursor != "" && k <= cfg.Cursor {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Apply max keys limit.
	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	var nextToken string
	if len(keys) > maxKeys {
		nextToken = keys[maxKeys-1]
		keys = keys[:maxKeys]
	}

	infos := make([]driver.ObjectInfo, 0, len(keys))
	for _, k := range keys {
		infos = append(infos, objects[k].info)
	}

	return driver.NewObjectIterator(infos, nextToken), nil
}

// Copy copies an object within or across buckets.
func (d *MemDriver) Copy(_ context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyCopyOptions(opts...)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, fmt.Errorf("memdriver: driver is closed")
	}

	srcObjects, ok := d.buckets[srcBucket]
	if !ok {
		return nil, fmt.Errorf("memdriver: source bucket %q not found", srcBucket)
	}

	srcObj, ok := srcObjects[srcKey]
	if !ok {
		return nil, fmt.Errorf("memdriver: source object %q not found in bucket %q", srcKey, srcBucket)
	}

	dstObjects, ok := d.buckets[dstBucket]
	if !ok {
		return nil, fmt.Errorf("memdriver: destination bucket %q not found", dstBucket)
	}

	// Deep copy data.
	dataCopy := make([]byte, len(srcObj.data))
	copy(dataCopy, srcObj.data)

	now := time.Now().UTC()
	meta := srcObj.info.Metadata
	if cfg.Metadata != nil {
		meta = cfg.Metadata
	}

	info := driver.ObjectInfo{
		Key:          dstKey,
		Size:         srcObj.info.Size,
		ContentType:  srcObj.info.ContentType,
		ETag:         srcObj.info.ETag,
		LastModified: now,
		Metadata:     meta,
		StorageClass: srcObj.info.StorageClass,
	}

	dstObjects[dstKey] = &memObject{data: dataCopy, info: info}

	return &info, nil
}

// CreateBucket creates a new bucket.
func (d *MemDriver) CreateBucket(_ context.Context, name string, _ ...driver.BucketOption) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return fmt.Errorf("memdriver: driver is closed")
	}

	if _, ok := d.buckets[name]; ok {
		return fmt.Errorf("memdriver: bucket %q already exists", name)
	}

	d.buckets[name] = make(map[string]*memObject)
	d.created[name] = time.Now().UTC()
	return nil
}

// DeleteBucket removes a bucket.
func (d *MemDriver) DeleteBucket(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return fmt.Errorf("memdriver: driver is closed")
	}

	if _, ok := d.buckets[name]; !ok {
		return fmt.Errorf("memdriver: bucket %q not found", name)
	}

	delete(d.buckets, name)
	delete(d.created, name)
	return nil
}

// ListBuckets returns all buckets.
func (d *MemDriver) ListBuckets(_ context.Context) ([]driver.BucketInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, fmt.Errorf("memdriver: driver is closed")
	}

	buckets := make([]driver.BucketInfo, 0, len(d.buckets))
	for name := range d.buckets {
		buckets = append(buckets, driver.BucketInfo{
			Name:      name,
			CreatedAt: d.created[name],
		})
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}
