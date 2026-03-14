// Package localdriver provides a local filesystem storage driver.
//
// The local driver stores objects as files on the local filesystem.
// Buckets map to subdirectories under a configurable root path.
// Object metadata is stored in sidecar .meta.json files.
//
// DSN format:
//
//	file:///path/to/root
//
// Usage:
//
//	drv := localdriver.New()
//	drv.Open(ctx, "file:///tmp/storage")
//	t, err := trove.Open(drv)
package localdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/trove/driver"
)

// Compile-time interface check.
var _ driver.Driver = (*LocalDriver)(nil)

// metadata is the sidecar JSON structure stored alongside objects.
type metadata struct {
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	StorageClass string            `json:"storage_class,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// LocalDriver implements driver.Driver using the local filesystem.
type LocalDriver struct {
	mu      sync.RWMutex
	rootDir string
	closed  bool
}

// New creates a new local filesystem driver.
func New() *LocalDriver {
	return &LocalDriver{}
}

// Name returns "local".
func (d *LocalDriver) Name() string { return "local" }

// Open initializes the driver with the given DSN.
// DSN format: file:///path/to/root
func (d *LocalDriver) Open(_ context.Context, dsn string, _ ...driver.Option) error {
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("localdriver: %w", err)
	}

	if cfg.Scheme != "file" {
		return fmt.Errorf("localdriver: expected scheme \"file\", got %q", cfg.Scheme)
	}

	rootDir := cfg.Path
	if rootDir == "" {
		return fmt.Errorf("localdriver: DSN path is empty")
	}

	// Ensure root directory exists.
	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		return fmt.Errorf("localdriver: create root dir: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.rootDir = rootDir
	d.closed = false
	return nil
}

// Close marks the driver as closed.
func (d *LocalDriver) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Ping verifies the root directory is accessible.
func (d *LocalDriver) Ping(_ context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return fmt.Errorf("localdriver: driver is closed")
	}

	_, err := os.Stat(d.rootDir)
	if err != nil {
		return fmt.Errorf("localdriver: ping: %w", err)
	}
	return nil
}

// Put stores an object as a file on disk.
func (d *LocalDriver) Put(_ context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyPutOptions(opts...)

	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	objPath := filepath.Join(rootDir, bucket, key)
	objDir := filepath.Dir(objPath)

	// Ensure parent directory exists.
	if err := os.MkdirAll(objDir, 0o750); err != nil {
		return nil, fmt.Errorf("localdriver: create dir: %w", err)
	}

	// Write to a temp file first, then atomically rename.
	tmpFile, err := os.CreateTemp(objDir, ".trove-tmp-*")
	if err != nil {
		return nil, fmt.Errorf("localdriver: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on failure.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	n, err := io.Copy(tmpFile, r)
	if err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("localdriver: write data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("localdriver: close temp file: %w", err)
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, objPath); err != nil { //nolint:gosec // paths are derived from controlled rootDir
		return nil, fmt.Errorf("localdriver: rename: %w", err)
	}
	success = true

	// Determine content type.
	ct := cfg.ContentType
	if ct == "" {
		ct = detectContentType(key)
	}

	now := time.Now().UTC()
	meta := metadata{
		ContentType:  ct,
		Metadata:     cfg.Metadata,
		StorageClass: cfg.StorageClass,
		CreatedAt:    now,
	}

	// Write sidecar metadata file.
	if err := d.writeMeta(objPath, meta); err != nil {
		return nil, err
	}

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         n,
		ContentType:  ct,
		ETag:         fmt.Sprintf("%x-%x", n, now.UnixNano()),
		LastModified: now,
		Metadata:     cfg.Metadata,
		StorageClass: cfg.StorageClass,
	}

	return info, nil
}

// Get retrieves an object from disk.
func (d *LocalDriver) Get(_ context.Context, bucket, key string, _ ...driver.GetOption) (*driver.ObjectReader, error) {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	objPath := filepath.Join(rootDir, bucket, key)

	f, err := os.Open(objPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("localdriver: object %q not found in bucket %q", key, bucket)
		}
		return nil, fmt.Errorf("localdriver: open file: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("localdriver: stat file: %w", err)
	}

	meta := d.readMeta(objPath)

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         stat.Size(),
		ContentType:  meta.ContentType,
		ETag:         fmt.Sprintf("%x-%x", stat.Size(), stat.ModTime().UnixNano()),
		LastModified: stat.ModTime(),
		Metadata:     meta.Metadata,
		StorageClass: meta.StorageClass,
	}

	return &driver.ObjectReader{
		ReadCloser: f,
		Info:       info,
	}, nil
}

// Delete removes an object from disk.
func (d *LocalDriver) Delete(_ context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	objPath := filepath.Join(rootDir, bucket, key)
	metaPath := objPath + ".meta.json"

	// Remove data file (idempotent).
	if err := os.Remove(objPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("localdriver: delete: %w", err)
	}

	// Remove sidecar metadata.
	os.Remove(metaPath)

	return nil
}

// Head returns object metadata without content.
func (d *LocalDriver) Head(_ context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	objPath := filepath.Join(rootDir, bucket, key)

	stat, err := os.Stat(objPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("localdriver: object %q not found in bucket %q", key, bucket)
		}
		return nil, fmt.Errorf("localdriver: stat: %w", err)
	}

	meta := d.readMeta(objPath)

	return &driver.ObjectInfo{
		Key:          key,
		Size:         stat.Size(),
		ContentType:  meta.ContentType,
		ETag:         fmt.Sprintf("%x-%x", stat.Size(), stat.ModTime().UnixNano()),
		LastModified: stat.ModTime(),
		Metadata:     meta.Metadata,
		StorageClass: meta.StorageClass,
	}, nil
}

// List returns objects matching the given options.
func (d *LocalDriver) List(_ context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	cfg := driver.ApplyListOptions(opts...)

	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	bucketDir := filepath.Join(rootDir, bucket)
	if _, err := os.Stat(bucketDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("localdriver: bucket %q not found", bucket)
	}

	var keys []string
	err := filepath.Walk(bucketDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Skip sidecar metadata files.
		if strings.HasSuffix(path, ".meta.json") {
			return nil
		}
		// Skip temp files.
		if strings.HasPrefix(filepath.Base(path), ".trove-tmp-") {
			return nil
		}

		rel, err := filepath.Rel(bucketDir, path)
		if err != nil {
			return err
		}

		if cfg.Prefix != "" && !strings.HasPrefix(rel, cfg.Prefix) {
			return nil
		}
		if cfg.Cursor != "" && rel <= cfg.Cursor {
			return nil
		}

		keys = append(keys, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("localdriver: list: %w", err)
	}

	sort.Strings(keys)

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
	for _, key := range keys {
		objPath := filepath.Join(bucketDir, key)
		stat, err := os.Stat(objPath)
		if err != nil {
			continue
		}
		meta := d.readMeta(objPath)
		infos = append(infos, driver.ObjectInfo{
			Key:          key,
			Size:         stat.Size(),
			ContentType:  meta.ContentType,
			ETag:         fmt.Sprintf("%x-%x", stat.Size(), stat.ModTime().UnixNano()),
			LastModified: stat.ModTime(),
			Metadata:     meta.Metadata,
		})
	}

	return driver.NewObjectIterator(infos, nextToken), nil
}

// Copy copies an object within or across buckets.
func (d *LocalDriver) Copy(_ context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyCopyOptions(opts...)

	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	srcPath := filepath.Join(rootDir, srcBucket, srcKey)
	dstPath := filepath.Join(rootDir, dstBucket, dstKey)

	// Read source file.
	srcFile, err := os.Open(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("localdriver: source object %q not found in bucket %q", srcKey, srcBucket)
		}
		return nil, fmt.Errorf("localdriver: open source: %w", err)
	}
	defer srcFile.Close()

	// Ensure destination directory exists.
	dstDir := filepath.Dir(dstPath)
	if mkErr := os.MkdirAll(dstDir, 0o750); mkErr != nil {
		return nil, fmt.Errorf("localdriver: create dst dir: %w", mkErr)
	}

	// Write destination file.
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("localdriver: create dst: %w", err)
	}

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		return nil, fmt.Errorf("localdriver: copy data: %w", err)
	}
	dstFile.Close()

	// Copy or override metadata.
	srcMeta := d.readMeta(srcPath)
	dstMeta := srcMeta
	if cfg.Metadata != nil {
		dstMeta.Metadata = cfg.Metadata
	}
	if err := d.writeMeta(dstPath, dstMeta); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	info := &driver.ObjectInfo{
		Key:          dstKey,
		Size:         n,
		ContentType:  dstMeta.ContentType,
		ETag:         fmt.Sprintf("%x-%x", n, now.UnixNano()),
		LastModified: now,
		Metadata:     dstMeta.Metadata,
		StorageClass: dstMeta.StorageClass,
	}

	return info, nil
}

// CreateBucket creates a subdirectory under the root.
func (d *LocalDriver) CreateBucket(_ context.Context, name string, _ ...driver.BucketOption) error {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	bucketDir := filepath.Join(rootDir, name)
	if _, err := os.Stat(bucketDir); err == nil {
		return fmt.Errorf("localdriver: bucket %q already exists", name)
	}

	if err := os.MkdirAll(bucketDir, 0o750); err != nil {
		return fmt.Errorf("localdriver: create bucket: %w", err)
	}

	return nil
}

// DeleteBucket removes a bucket subdirectory.
func (d *LocalDriver) DeleteBucket(_ context.Context, name string) error {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	bucketDir := filepath.Join(rootDir, name)
	if _, err := os.Stat(bucketDir); os.IsNotExist(err) {
		return fmt.Errorf("localdriver: bucket %q not found", name)
	}

	if err := os.RemoveAll(bucketDir); err != nil {
		return fmt.Errorf("localdriver: delete bucket: %w", err)
	}

	return nil
}

// ListBuckets returns all bucket directories.
func (d *LocalDriver) ListBuckets(_ context.Context) ([]driver.BucketInfo, error) {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return nil, fmt.Errorf("localdriver: driver is closed")
	}
	rootDir := d.rootDir
	d.mu.RUnlock()

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("localdriver: list buckets: %w", err)
	}

	var buckets []driver.BucketInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		buckets = append(buckets, driver.BucketInfo{
			Name:      entry.Name(),
			CreatedAt: info.ModTime(),
		})
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}

// --- Internal helpers ---

func (d *LocalDriver) writeMeta(objPath string, meta metadata) error {
	metaPath := objPath + ".meta.json"
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("localdriver: marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0o600); err != nil {
		return fmt.Errorf("localdriver: write metadata: %w", err)
	}
	return nil
}

func (d *LocalDriver) readMeta(objPath string) metadata {
	metaPath := objPath + ".meta.json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		// No sidecar — infer content type from extension.
		return metadata{
			ContentType: detectContentType(filepath.Base(objPath)),
		}
	}

	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{
			ContentType: detectContentType(filepath.Base(objPath)),
		}
	}
	return meta
}

// detectContentType infers MIME type from file extension.
func detectContentType(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

// Unwrap extracts the typed LocalDriver from a Trove handle.
func Unwrap(t interface{ Driver() driver.Driver }) *LocalDriver {
	if ld, ok := t.Driver().(*LocalDriver); ok {
		return ld
	}
	return nil
}

// RootDir returns the root directory path, useful for testing and debugging.
func (d *LocalDriver) RootDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.rootDir
}

// SetRootDir directly sets the root directory without parsing a DSN.
// Useful for testing when you already have a temporary directory.
func (d *LocalDriver) SetRootDir(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rootDir = dir
	d.closed = false
}
