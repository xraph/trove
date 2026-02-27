// Package gcsdriver provides a Google Cloud Storage driver for Trove.
//
// The GCS driver stores objects in Google Cloud Storage. It implements the
// core driver.Driver interface plus MultipartDriver, PresignDriver,
// and RangeDriver capability interfaces.
//
// DSN format:
//
//	gcs://PROJECT_ID/BUCKET
//	gcs://PROJECT_ID/BUCKET?credentials=/path/to/key.json&endpoint=http://localhost:4443
//
// Usage:
//
//	drv := gcsdriver.New()
//	drv.Open(ctx, "gcs://my-project/my-bucket")
//	t, err := trove.Open(drv)
package gcsdriver

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/xraph/trove/driver"
)

// Compile-time interface checks.
var (
	_ driver.Driver          = (*GCSDriver)(nil)
	_ driver.MultipartDriver = (*GCSDriver)(nil)
	_ driver.PresignDriver   = (*GCSDriver)(nil)
	_ driver.RangeDriver     = (*GCSDriver)(nil)
)

// GCSDriver implements driver.Driver for Google Cloud Storage.
type GCSDriver struct {
	mu     sync.RWMutex
	client *storage.Client
	cfg    *gcsConfig
	closed bool
}

// New creates a new GCS storage driver.
func New() *GCSDriver {
	return &GCSDriver{}
}

// Name returns "gcs".
func (d *GCSDriver) Name() string { return "gcs" }

// Open initializes the GCS client using the given DSN and options.
func (d *GCSDriver) Open(ctx context.Context, dsn string, opts ...driver.Option) error {
	drvCfg := driver.ApplyOptions(opts...)

	gcsCfg, err := parseDSN(dsn)
	if err != nil {
		return err
	}

	// Allow driver options to override DSN values.
	if drvCfg.Endpoint != "" {
		gcsCfg.Endpoint = drvCfg.Endpoint
	}

	var clientOpts []option.ClientOption

	if gcsCfg.CredentialsFile != "" {
		clientOpts = append(clientOpts, option.WithCredentialsFile(gcsCfg.CredentialsFile))
	}

	if gcsCfg.Endpoint != "" {
		clientOpts = append(clientOpts, option.WithEndpoint(gcsCfg.Endpoint))
		clientOpts = append(clientOpts, option.WithoutAuthentication())
	}

	client, err := storage.NewClient(ctx, clientOpts...)
	if err != nil {
		return fmt.Errorf("gcsdriver: create client: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.client = client
	d.cfg = gcsCfg
	d.closed = false
	return nil
}

// Close shuts down the GCS client.
func (d *GCSDriver) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// Ping verifies GCS connectivity by checking bucket attributes.
func (d *GCSDriver) Ping(ctx context.Context) error {
	client, cfg, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.Bucket(cfg.Bucket).Attrs(ctx)
	if err != nil {
		return fmt.Errorf("gcsdriver: ping: %w", err)
	}
	return nil
}

// Put stores an object in GCS.
func (d *GCSDriver) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyPutOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	ct := cfg.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	obj := client.Bucket(bucket).Object(key)
	w := obj.NewWriter(ctx)
	w.ContentType = ct

	if cfg.StorageClass != "" {
		w.StorageClass = cfg.StorageClass
	}
	if len(cfg.Metadata) > 0 {
		w.Metadata = cfg.Metadata
	}

	n, err := io.Copy(w, r)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("gcsdriver: put %q: write: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gcsdriver: put %q: close: %w", key, err)
	}

	// Get the attrs after write for ETag and timestamp.
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		// Fall back to constructed info.
		return &driver.ObjectInfo{
			Key:          key,
			Size:         n,
			ContentType:  ct,
			ETag:         fmt.Sprintf("%x-%x", n, time.Now().UnixNano()),
			LastModified: time.Now().UTC(),
			Metadata:     cfg.Metadata,
			StorageClass: cfg.StorageClass,
		}, nil
	}

	return attrsToInfo(key, attrs), nil
}

// Get retrieves an object from GCS.
func (d *GCSDriver) Get(ctx context.Context, bucket, key string, _ ...driver.GetOption) (*driver.ObjectReader, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	obj := client.Bucket(bucket).Object(key)

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcsdriver: get %q: %w", key, err)
	}

	attrs, err := obj.Attrs(ctx)
	if err != nil {
		reader.Close()
		return nil, fmt.Errorf("gcsdriver: get %q attrs: %w", key, err)
	}

	return &driver.ObjectReader{
		ReadCloser: reader,
		Info:       attrsToInfo(key, attrs),
	}, nil
}

// Delete removes an object from GCS.
func (d *GCSDriver) Delete(ctx context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	err = client.Bucket(bucket).Object(key).Delete(ctx)
	if err != nil {
		// Treat "not found" as idempotent delete.
		if err == storage.ErrObjectNotExist {
			return nil
		}
		return fmt.Errorf("gcsdriver: delete %q: %w", key, err)
	}
	return nil
}

// Head returns object metadata without content.
func (d *GCSDriver) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	attrs, err := client.Bucket(bucket).Object(key).Attrs(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcsdriver: head %q: %w", key, err)
	}

	return attrsToInfo(key, attrs), nil
}

// List returns objects matching the given options.
func (d *GCSDriver) List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	cfg := driver.ApplyListOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	query := &storage.Query{}
	if cfg.Prefix != "" {
		query.Prefix = cfg.Prefix
	}
	if cfg.Delimiter != "" {
		query.Delimiter = cfg.Delimiter
	}

	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	it := client.Bucket(bucket).Objects(ctx, query)

	var infos []driver.ObjectInfo
	var nextToken string
	count := 0

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcsdriver: list bucket %q: %w", bucket, err)
		}

		if cfg.Cursor != "" && attrs.Name <= cfg.Cursor {
			continue
		}

		count++
		if count > maxKeys {
			nextToken = infos[len(infos)-1].Key
			break
		}

		infos = append(infos, driver.ObjectInfo{
			Key:          attrs.Name,
			Size:         attrs.Size,
			ContentType:  attrs.ContentType,
			ETag:         attrs.Etag,
			LastModified: attrs.Updated,
			Metadata:     attrs.Metadata,
			StorageClass: attrs.StorageClass,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Key < infos[j].Key
	})

	return driver.NewObjectIterator(infos, nextToken), nil
}

// Copy copies an object within or across buckets.
func (d *GCSDriver) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, _ ...driver.CopyOption) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	src := client.Bucket(srcBucket).Object(srcKey)
	dst := client.Bucket(dstBucket).Object(dstKey)

	attrs, err := dst.CopierFrom(src).Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcsdriver: copy %q → %q: %w", srcKey, dstKey, err)
	}

	return attrsToInfo(dstKey, attrs), nil
}

// CreateBucket creates a new GCS bucket.
func (d *GCSDriver) CreateBucket(ctx context.Context, name string, _ ...driver.BucketOption) error {
	client, cfg, err := d.getClient()
	if err != nil {
		return err
	}

	attrs := &storage.BucketAttrs{
		Name: name,
	}

	if err := client.Bucket(name).Create(ctx, cfg.ProjectID, attrs); err != nil {
		return fmt.Errorf("gcsdriver: create bucket %q: %w", name, err)
	}
	return nil
}

// DeleteBucket removes a GCS bucket.
func (d *GCSDriver) DeleteBucket(ctx context.Context, name string) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	if err := client.Bucket(name).Delete(ctx); err != nil {
		return fmt.Errorf("gcsdriver: delete bucket %q: %w", name, err)
	}
	return nil
}

// ListBuckets returns all accessible buckets.
func (d *GCSDriver) ListBuckets(ctx context.Context) ([]driver.BucketInfo, error) {
	client, cfg, err := d.getClient()
	if err != nil {
		return nil, err
	}

	it := client.Buckets(ctx, cfg.ProjectID)
	var buckets []driver.BucketInfo

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcsdriver: list buckets: %w", err)
		}

		buckets = append(buckets, driver.BucketInfo{
			Name:      attrs.Name,
			CreatedAt: attrs.Created,
		})
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}

// Client returns the underlying GCS client for advanced use cases.
func (d *GCSDriver) Client() *storage.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

// Unwrap extracts the typed GCSDriver from a Trove handle.
func Unwrap(t interface{ Driver() driver.Driver }) *GCSDriver {
	if gd, ok := t.Driver().(*GCSDriver); ok {
		return gd
	}
	return nil
}

// --- Internal helpers ---

func (d *GCSDriver) getClient() (*storage.Client, *gcsConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, nil, fmt.Errorf("gcsdriver: driver is closed")
	}
	if d.client == nil {
		return nil, nil, fmt.Errorf("gcsdriver: driver not opened")
	}
	return d.client, d.cfg, nil
}

func attrsToInfo(key string, attrs *storage.ObjectAttrs) *driver.ObjectInfo {
	var meta map[string]string
	if len(attrs.Metadata) > 0 {
		meta = attrs.Metadata
	}

	return &driver.ObjectInfo{
		Key:          key,
		Size:         attrs.Size,
		ContentType:  attrs.ContentType,
		ETag:         attrs.Etag,
		LastModified: attrs.Updated,
		Metadata:     meta,
		StorageClass: attrs.StorageClass,
	}
}

func init() {
	driver.Register("gcs", func() driver.Driver { return New() })
}
