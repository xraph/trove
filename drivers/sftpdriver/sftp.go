// Package sftpdriver provides an SFTP storage driver for Trove.
//
// The SFTP driver stores objects as files on a remote server accessed via SSH/SFTP.
// Buckets map to subdirectories under a configurable base path.
// Object metadata is stored in sidecar .meta.json files (following localdriver pattern).
//
// DSN format:
//
//	sftp://user:pass@host:22/basepath
//	sftp://user@host/basepath?key=/path/to/key
//
// Usage:
//
//	drv := sftpdriver.New()
//	drv.Open(ctx, "sftp://deploy:secret@files.example.com:22/data/storage")
//	t, err := trove.Open(drv)
package sftpdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/xraph/trove/driver"
)

// Compile-time interface check.
var _ driver.Driver = (*SFTPDriver)(nil)

// metadata is the sidecar JSON structure stored alongside objects.
type metadata struct {
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	StorageClass string            `json:"storage_class,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// SFTPDriver implements driver.Driver using SFTP.
type SFTPDriver struct {
	mu      sync.RWMutex
	sshConn *ssh.Client
	client  *sftp.Client
	cfg     *sftpConfig
	closed  bool
}

// New creates a new SFTP storage driver.
func New() *SFTPDriver {
	return &SFTPDriver{}
}

// Name returns "sftp".
func (d *SFTPDriver) Name() string { return "sftp" }

// Open initializes the SFTP connection using the given DSN.
func (d *SFTPDriver) Open(_ context.Context, dsn string, _ ...driver.Option) error {
	cfg, err := parseDSN(dsn)
	if err != nil {
		return err
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // configurable in production
		Timeout:         30 * time.Second,
	}

	// Configure authentication.
	if cfg.KeyFile != "" {
		keyData, err := os.ReadFile(cfg.KeyFile)
		if err != nil {
			return fmt.Errorf("sftpdriver: read key file: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return fmt.Errorf("sftpdriver: parse private key: %w", err)
		}
		sshCfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else if cfg.Password != "" {
		sshCfg.Auth = []ssh.AuthMethod{ssh.Password(cfg.Password)}
	}

	conn, err := ssh.Dial("tcp", cfg.addr(), sshCfg)
	if err != nil {
		return fmt.Errorf("sftpdriver: ssh dial: %w", err)
	}

	client, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("sftpdriver: sftp client: %w", err)
	}

	// Ensure base path exists.
	if err := client.MkdirAll(cfg.BasePath); err != nil {
		client.Close()
		conn.Close()
		return fmt.Errorf("sftpdriver: create base path: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.sshConn = conn
	d.client = client
	d.cfg = cfg
	d.closed = false
	return nil
}

// Close disconnects the SFTP and SSH connections.
func (d *SFTPDriver) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true

	var errs []error
	if d.client != nil {
		if err := d.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.sshConn != nil {
		if err := d.sshConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("sftpdriver: close: %v", errs[0])
	}
	return nil
}

// Ping verifies SFTP connectivity by stat'ing the base path.
func (d *SFTPDriver) Ping(_ context.Context) error {
	client, cfg, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.Stat(cfg.BasePath)
	if err != nil {
		return fmt.Errorf("sftpdriver: ping: %w", err)
	}
	return nil
}

// Put stores an object on the remote SFTP server.
func (d *SFTPDriver) Put(_ context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyPutOptions(opts...)
	client, drvCfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	objPath := d.objectPath(drvCfg, bucket, key)
	objDir := path.Dir(objPath)

	// Ensure parent directory exists.
	if err := client.MkdirAll(objDir); err != nil {
		return nil, fmt.Errorf("sftpdriver: create dir: %w", err)
	}

	// Write object data.
	f, err := client.Create(objPath)
	if err != nil {
		return nil, fmt.Errorf("sftpdriver: create file: %w", err)
	}

	n, err := io.Copy(f, r)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("sftpdriver: write data: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("sftpdriver: close file: %w", err)
	}

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
	if err := d.writeMeta(client, objPath, meta); err != nil {
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

// Get retrieves an object from the remote SFTP server.
func (d *SFTPDriver) Get(_ context.Context, bucket, key string, _ ...driver.GetOption) (*driver.ObjectReader, error) {
	client, drvCfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	objPath := d.objectPath(drvCfg, bucket, key)

	f, err := client.Open(objPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("sftpdriver: object %q not found in bucket %q", key, bucket)
		}
		return nil, fmt.Errorf("sftpdriver: open file: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("sftpdriver: stat file: %w", err)
	}

	meta := d.readMeta(client, objPath)

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

// Delete removes an object from the remote SFTP server.
func (d *SFTPDriver) Delete(_ context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	client, drvCfg, err := d.getClient()
	if err != nil {
		return err
	}

	objPath := d.objectPath(drvCfg, bucket, key)
	metaPath := objPath + ".meta.json"

	// Remove data file (idempotent).
	if err := client.Remove(objPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sftpdriver: delete: %w", err)
	}

	// Remove sidecar metadata (best effort).
	client.Remove(metaPath) //nolint:errcheck

	return nil
}

// Head returns object metadata without content.
func (d *SFTPDriver) Head(_ context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	client, drvCfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	objPath := d.objectPath(drvCfg, bucket, key)

	stat, err := client.Stat(objPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("sftpdriver: object %q not found in bucket %q", key, bucket)
		}
		return nil, fmt.Errorf("sftpdriver: stat: %w", err)
	}

	meta := d.readMeta(client, objPath)

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
func (d *SFTPDriver) List(_ context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	cfg := driver.ApplyListOptions(opts...)
	client, drvCfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	bucketDir := path.Join(drvCfg.BasePath, bucket)
	stat, err := client.Stat(bucketDir)
	if err != nil || !stat.IsDir() {
		return nil, fmt.Errorf("sftpdriver: bucket %q not found", bucket)
	}

	var keys []string
	err = d.walkDir(client, bucketDir, bucketDir, &keys)
	if err != nil {
		return nil, fmt.Errorf("sftpdriver: list: %w", err)
	}

	// Filter by prefix and cursor.
	var filtered []string
	for _, key := range keys {
		if cfg.Prefix != "" && !strings.HasPrefix(key, cfg.Prefix) {
			continue
		}
		if cfg.Cursor != "" && key <= cfg.Cursor {
			continue
		}
		filtered = append(filtered, key)
	}

	sort.Strings(filtered)

	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	var nextToken string
	if len(filtered) > maxKeys {
		nextToken = filtered[maxKeys-1]
		filtered = filtered[:maxKeys]
	}

	infos := make([]driver.ObjectInfo, 0, len(filtered))
	for _, key := range filtered {
		objPath := path.Join(bucketDir, key)
		stat, err := client.Stat(objPath)
		if err != nil {
			continue
		}
		meta := d.readMeta(client, objPath)
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
func (d *SFTPDriver) Copy(_ context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error) {
	copyCfg := driver.ApplyCopyOptions(opts...)
	client, drvCfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	srcPath := d.objectPath(drvCfg, srcBucket, srcKey)
	dstPath := d.objectPath(drvCfg, dstBucket, dstKey)

	// Open source file.
	srcFile, err := client.Open(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("sftpdriver: source object %q not found in bucket %q", srcKey, srcBucket)
		}
		return nil, fmt.Errorf("sftpdriver: open source: %w", err)
	}
	defer srcFile.Close()

	// Ensure destination directory exists.
	dstDir := path.Dir(dstPath)
	if err := client.MkdirAll(dstDir); err != nil {
		return nil, fmt.Errorf("sftpdriver: create dst dir: %w", err)
	}

	// Write destination file.
	dstFile, err := client.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("sftpdriver: create dst: %w", err)
	}

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		return nil, fmt.Errorf("sftpdriver: copy data: %w", err)
	}
	dstFile.Close()

	// Copy or override metadata.
	srcMeta := d.readMeta(client, srcPath)
	dstMeta := srcMeta
	if copyCfg.Metadata != nil {
		dstMeta.Metadata = copyCfg.Metadata
	}
	if err := d.writeMeta(client, dstPath, dstMeta); err != nil {
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

// CreateBucket creates a subdirectory under the base path.
func (d *SFTPDriver) CreateBucket(_ context.Context, name string, _ ...driver.BucketOption) error {
	client, drvCfg, err := d.getClient()
	if err != nil {
		return err
	}

	bucketDir := path.Join(drvCfg.BasePath, name)

	// Check if it already exists.
	if stat, err := client.Stat(bucketDir); err == nil && stat.IsDir() {
		return fmt.Errorf("sftpdriver: bucket %q already exists", name)
	}

	if err := client.MkdirAll(bucketDir); err != nil {
		return fmt.Errorf("sftpdriver: create bucket: %w", err)
	}

	return nil
}

// DeleteBucket removes a bucket subdirectory.
func (d *SFTPDriver) DeleteBucket(_ context.Context, name string) error {
	client, drvCfg, err := d.getClient()
	if err != nil {
		return err
	}

	bucketDir := path.Join(drvCfg.BasePath, name)

	stat, err := client.Stat(bucketDir)
	if err != nil || !stat.IsDir() {
		return fmt.Errorf("sftpdriver: bucket %q not found", name)
	}

	// Recursively remove all contents.
	if err := d.removeAll(client, bucketDir); err != nil {
		return fmt.Errorf("sftpdriver: delete bucket: %w", err)
	}

	return nil
}

// ListBuckets returns all bucket directories.
func (d *SFTPDriver) ListBuckets(_ context.Context) ([]driver.BucketInfo, error) {
	client, drvCfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	entries, err := client.ReadDir(drvCfg.BasePath)
	if err != nil {
		return nil, fmt.Errorf("sftpdriver: list buckets: %w", err)
	}

	var buckets []driver.BucketInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		buckets = append(buckets, driver.BucketInfo{
			Name:      entry.Name(),
			CreatedAt: entry.ModTime(),
		})
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}

// Client returns the underlying SFTP client for advanced use cases.
func (d *SFTPDriver) Client() *sftp.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

// Unwrap extracts the typed SFTPDriver from a Trove handle.
func Unwrap(t interface{ Driver() driver.Driver }) *SFTPDriver {
	if sd, ok := t.Driver().(*SFTPDriver); ok {
		return sd
	}
	return nil
}

// --- Internal helpers ---

func (d *SFTPDriver) getClient() (*sftp.Client, *sftpConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, nil, fmt.Errorf("sftpdriver: driver is closed")
	}
	if d.client == nil {
		return nil, nil, fmt.Errorf("sftpdriver: driver not opened")
	}
	return d.client, d.cfg, nil
}

func (d *SFTPDriver) objectPath(cfg *sftpConfig, bucket, key string) string {
	return path.Join(cfg.BasePath, bucket, key)
}

func (d *SFTPDriver) writeMeta(client *sftp.Client, objPath string, meta metadata) error {
	metaPath := objPath + ".meta.json"
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("sftpdriver: marshal metadata: %w", err)
	}

	f, err := client.Create(metaPath)
	if err != nil {
		return fmt.Errorf("sftpdriver: create metadata file: %w", err)
	}

	if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
		f.Close()
		return fmt.Errorf("sftpdriver: write metadata: %w", err)
	}

	return f.Close()
}

func (d *SFTPDriver) readMeta(client *sftp.Client, objPath string) metadata {
	metaPath := objPath + ".meta.json"

	f, err := client.Open(metaPath)
	if err != nil {
		return metadata{
			ContentType: detectContentType(path.Base(objPath)),
		}
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return metadata{
			ContentType: detectContentType(path.Base(objPath)),
		}
	}

	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{
			ContentType: detectContentType(path.Base(objPath)),
		}
	}
	return meta
}

func (d *SFTPDriver) walkDir(client *sftp.Client, baseDir, dir string, keys *[]string) error {
	entries, err := client.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryPath := path.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := d.walkDir(client, baseDir, entryPath, keys); err != nil {
				return err
			}
			continue
		}

		// Skip sidecar metadata files.
		if strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}

		// Compute relative key.
		rel := strings.TrimPrefix(entryPath, baseDir+"/")
		*keys = append(*keys, rel)
	}

	return nil
}

func (d *SFTPDriver) removeAll(client *sftp.Client, dir string) error {
	entries, err := client.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryPath := path.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := d.removeAll(client, entryPath); err != nil {
				return err
			}
		} else {
			if err := client.Remove(entryPath); err != nil {
				return err
			}
		}
	}

	return client.RemoveDirectory(dir)
}

// detectContentType infers MIME type from file extension.
func detectContentType(name string) string {
	ext := path.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func init() {
	driver.Register("sftp", func() driver.Driver { return New() })
}
