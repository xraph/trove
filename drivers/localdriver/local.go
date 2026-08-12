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

func init() {
	driver.Register("file", func() driver.Driver { return New() })
	driver.Register("local", func() driver.Driver { return New() })
}

// Compile-time interface check.
var _ driver.Driver = (*LocalDriver)(nil)

// safeJoin joins rootDir with the given parts and verifies the resulting
// path is contained within rootDir. It prevents path traversal via taint
// inputs such as bucket/key containing "..".
//
// Every path the driver touches is built here, so a bucket or key that tries
// to climb out of the root is refused before it reaches the filesystem rather
// than after. Segments must be non-empty, NUL-free, and relative.
//
// Containment is enforced once per segment, against the directory the
// preceding segments produced — not once at the end against rootDir. The
// difference matters: joining everything first and testing only the final
// result lets a key spend the bucket segment on the way out, so
// safeJoin(root, "tenant-a", "../tenant-b/secret") would land on another
// bucket's object while still sitting under the root. Buckets are a tenancy
// boundary, so a key must stay inside its own bucket.
func safeJoin(rootDir string, parts ...string) (string, error) {
	dir := filepath.Clean(rootDir)
	for _, p := range parts {
		if p == "" || strings.Contains(p, "\x00") {
			return "", fmt.Errorf("localdriver: invalid path segment %q: %w", p, driver.ErrInvalidPath)
		}
		if isRooted(p) {
			return "", fmt.Errorf("localdriver: path segment must be relative: %q: %w", p, driver.ErrInvalidPath)
		}

		joined := filepath.Join(dir, p)
		rel, err := filepath.Rel(dir, joined)
		if err != nil {
			// Not wrapped as ErrInvalidPath: Rel failing is a defect in this
			// function's own inputs, not a judgement about the caller's
			// segment, and the message may name the resolved directory.
			return "", fmt.Errorf("localdriver: resolve path: %w", err)
		}
		// Every rejection below wraps driver.ErrInvalidPath so callers can
		// classify it — the HTTP extension turns it into 400 rather than 500.
		// These messages name the offending segment but never the directory
		// it was resolved against: the segment came from the caller, while
		// the resolved path is a server-side absolute path, and callers such
		// as the HTTP extension put classified driver errors straight into
		// responses.
		switch {
		case rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
			return "", fmt.Errorf("localdriver: path segment %q escapes its parent directory: %w", p, driver.ErrInvalidPath)
		case rel == ".":
			// The segment names its own parent rather than something inside
			// it — a key of "." resolves to the bucket directory, and a
			// bucket of "." to the root. DeleteBucket(".") would then hand
			// os.RemoveAll the entire storage root.
			return "", fmt.Errorf("localdriver: path segment %q does not name anything inside its parent directory: %w", p, driver.ErrInvalidPath)
		}
		dir = joined
	}
	return dir, nil
}

// isRooted reports whether p would be read as an absolute path on any
// platform. filepath.IsAbs alone is not enough: it is platform-specific, so
// on Windows it answers false for "/etc/passwd" and the segment would slip
// through a check that holds on Unix.
//
// A rooted segment cannot escape the root — filepath.Join reinterprets it as
// relative — but it is rejected anyway, because silently storing a key of
// "/etc/passwd" under <root>/<bucket>/etc/passwd hands the caller back an
// object at a key it never asked for.
func isRooted(p string) bool {
	return filepath.IsAbs(p) ||
		strings.HasPrefix(p, "/") ||
		strings.HasPrefix(p, `\`) ||
		filepath.VolumeName(p) != ""
}

// classifyObjectErr maps a filesystem error encountered while reaching an
// object onto the Trove sentinels, returning nil when the error is not one
// of the classified conditions and the caller should wrap it itself.
//
// A missing file inside a missing bucket directory is reported as a missing
// bucket: that is both more accurate and more useful to a caller deciding
// whether the object could ever come back.
func classifyObjectErr(err error, rootDir, bucket, key string) error {
	switch {
	case os.IsNotExist(err):
		// Route the bucket probe through safeJoin as well, so this helper
		// cannot be the one place that stats an unvalidated path if a future
		// caller forgets to validate. A bucket name that will not join is a
		// bucket that cannot exist.
		bucketDir, joinErr := safeJoin(rootDir, bucket)
		if joinErr != nil {
			return fmt.Errorf("localdriver: bucket %q not found: %w", bucket, driver.ErrBucketNotFound)
		}
		if _, statErr := os.Stat(bucketDir); os.IsNotExist(statErr) {
			return fmt.Errorf("localdriver: bucket %q not found: %w", bucket, driver.ErrBucketNotFound)
		}
		return fmt.Errorf("localdriver: object %q not found in bucket %q: %w", key, bucket, driver.ErrObjectNotFound)
	case os.IsPermission(err):
		return fmt.Errorf("localdriver: permission denied for object %q in bucket %q: %w", key, bucket, driver.ErrPermissionDenied)
	default:
		return nil
	}
}

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
//
// Supported DSN formats:
//
//	file:///path/to/root          – absolute path
//	local://./relative/path       – relative path (resolved from cwd)
//	local:///absolute/path        – absolute path
func (d *LocalDriver) Open(_ context.Context, dsn string, _ ...driver.Option) error {
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("localdriver: %w", err)
	}

	if cfg.Scheme != "file" && cfg.Scheme != "local" {
		return fmt.Errorf("localdriver: expected scheme \"file\" or \"local\", got %q", cfg.Scheme)
	}

	// For "local" scheme, reconstruct the path from host+path to support
	// relative paths like "local://./storages/local" where URL parsing
	// splits "." into host and "/storages/local" into path.
	var rootDir string
	if cfg.Scheme == "local" && cfg.Host != "" {
		rootDir = cfg.Host + cfg.Path
	} else {
		rootDir = cfg.Path
	}
	if rootDir == "" {
		return fmt.Errorf("localdriver: DSN path is empty")
	}

	// Resolve relative paths to absolute.
	if !filepath.IsAbs(rootDir) {
		abs, absErr := filepath.Abs(rootDir)
		if absErr != nil {
			return fmt.Errorf("localdriver: resolve absolute path: %w", absErr)
		}
		rootDir = abs
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

	objPath, err := safeJoin(rootDir, bucket, key)
	if err != nil {
		return nil, err
	}
	objDir := filepath.Dir(objPath)

	// Ensure parent directory exists.
	if mkErr := os.MkdirAll(objDir, 0o750); mkErr != nil {
		return nil, fmt.Errorf("localdriver: create dir: %w", mkErr)
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
			_ = os.Remove(tmpPath)
		}
	}()

	n, err := io.Copy(tmpFile, r)
	if err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("localdriver: write data: %w", err)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		return nil, fmt.Errorf("localdriver: close temp file: %w", closeErr)
	}

	// Atomic rename. objPath came from safeJoin, which proved it resolves
	// inside rootDir and inside its bucket before any of this ran.
	//
	// gosec reports G703 here regardless. Its taint analysis tracks the flow
	// from the bucket and key parameters to this filesystem sink but has no
	// way to recognize safeJoin's filepath.Rel containment check as a
	// sanitizer — the check is already in the flow and the finding persists,
	// so this is a false positive, not a missing guard. The only way to
	// silence it structurally would be to launder objPath through something
	// the analyzer cannot follow, which would hide real traversal bugs here
	// in future as well.
	// #nosec G703 -- path validated by safeJoin at the top of this function.
	if renameErr := os.Rename(tmpPath, objPath); renameErr != nil {
		return nil, fmt.Errorf("localdriver: rename: %w", renameErr)
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

	objPath, err := safeJoin(rootDir, bucket, key)
	if err != nil {
		return nil, err
	}

	// #nosec G304 -- path produced by safeJoin, which rejects anything resolving outside rootDir.
	f, err := os.Open(objPath)
	if err != nil {
		if cErr := classifyObjectErr(err, rootDir, bucket, key); cErr != nil {
			return nil, cErr
		}
		return nil, fmt.Errorf("localdriver: open file: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
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

	objPath, err := safeJoin(rootDir, bucket, key)
	if err != nil {
		return err
	}
	metaPath := objPath + ".meta.json"

	// Remove data file (idempotent).
	if err := os.Remove(objPath); err != nil && !os.IsNotExist(err) {
		if cErr := classifyObjectErr(err, rootDir, bucket, key); cErr != nil {
			return cErr
		}
		return fmt.Errorf("localdriver: delete: %w", err)
	}

	// Remove sidecar metadata.
	_ = os.Remove(metaPath)

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

	objPath, err := safeJoin(rootDir, bucket, key)
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(objPath)
	if err != nil {
		if cErr := classifyObjectErr(err, rootDir, bucket, key); cErr != nil {
			return nil, cErr
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

	bucketDir, err := safeJoin(rootDir, bucket)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(bucketDir); os.IsNotExist(statErr) {
		return nil, fmt.Errorf("localdriver: bucket %q not found: %w", bucket, driver.ErrBucketNotFound)
	}

	var keys []string
	err = filepath.Walk(bucketDir, func(path string, info os.FileInfo, err error) error {
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

	srcPath, err := safeJoin(rootDir, srcBucket, srcKey)
	if err != nil {
		return nil, err
	}
	dstPath, err := safeJoin(rootDir, dstBucket, dstKey)
	if err != nil {
		return nil, err
	}

	// Read source file.
	// #nosec G304 -- path produced by safeJoin, which rejects anything resolving outside rootDir.
	srcFile, err := os.Open(srcPath)
	if err != nil {
		if cErr := classifyObjectErr(err, rootDir, srcBucket, srcKey); cErr != nil {
			return nil, fmt.Errorf("localdriver: open source: %w", cErr)
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
	// #nosec G304 -- path produced by safeJoin, which rejects anything resolving outside rootDir.
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("localdriver: create dst: %w", err)
	}

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		_ = dstFile.Close()
		return nil, fmt.Errorf("localdriver: copy data: %w", err)
	}
	_ = dstFile.Close()

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

	bucketDir, err := safeJoin(rootDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(bucketDir); err == nil {
		return fmt.Errorf("localdriver: bucket %q already exists: %w", name, driver.ErrBucketExists)
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

	bucketDir, err := safeJoin(rootDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(bucketDir); os.IsNotExist(err) {
		return fmt.Errorf("localdriver: bucket %q not found: %w", name, driver.ErrBucketNotFound)
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
	// #nosec G304 -- path produced by safeJoin, which rejects anything resolving outside rootDir.
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
