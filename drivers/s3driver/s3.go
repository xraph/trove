// Package s3driver provides an Amazon S3 and S3-compatible storage driver for Trove.
//
// The S3 driver stores objects in Amazon S3 or any S3-compatible service
// (MinIO, DigitalOcean Spaces, Backblaze B2, etc.). It implements the
// core driver.Driver interface plus MultipartDriver, PresignDriver,
// and RangeDriver capability interfaces.
//
// DSN format:
//
//	s3://REGION/BUCKET
//	s3://ACCESS_KEY:SECRET@REGION/BUCKET?endpoint=http://localhost:9000&path_style=true
//
// Usage:
//
//	drv := s3driver.New()
//	drv.Open(ctx, "s3://us-east-1/my-bucket")
//	t, err := trove.Open(drv)
package s3driver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/xraph/trove/driver"
)

// Compile-time interface checks.
var (
	_ driver.Driver          = (*S3Driver)(nil)
	_ driver.MultipartDriver = (*S3Driver)(nil)
	_ driver.PresignDriver   = (*S3Driver)(nil)
	_ driver.RangeDriver     = (*S3Driver)(nil)
)

// S3Driver implements driver.Driver for Amazon S3 and S3-compatible stores.
type S3Driver struct {
	mu       sync.RWMutex
	client   *s3.Client
	psClient *s3.PresignClient
	cfg      *s3Config
	closed   bool
}

// New creates a new S3 storage driver.
func New() *S3Driver {
	return &S3Driver{}
}

// Name returns "s3".
func (d *S3Driver) Name() string { return "s3" }

// Open initializes the S3 client using the given DSN and options.
func (d *S3Driver) Open(ctx context.Context, dsn string, opts ...driver.Option) error {
	drvCfg := driver.ApplyOptions(opts...)

	s3cfg, err := parseDSN(dsn)
	if err != nil {
		return err
	}

	// Allow driver options to override DSN values.
	if drvCfg.Region != "" {
		s3cfg.Region = drvCfg.Region
	}
	if drvCfg.Endpoint != "" {
		s3cfg.Endpoint = drvCfg.Endpoint
	}
	if drvCfg.ForcePathStyle {
		s3cfg.ForcePathStyle = true
	}

	awsCfg, err := d.buildAWSConfig(ctx, s3cfg)
	if err != nil {
		return fmt.Errorf("s3driver: build config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if s3cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(s3cfg.Endpoint)
		}
		o.UsePathStyle = s3cfg.ForcePathStyle
	})

	d.mu.Lock()
	defer d.mu.Unlock()
	d.client = client
	d.psClient = s3.NewPresignClient(client)
	d.cfg = s3cfg
	d.closed = false
	return nil
}

// Close marks the driver as closed.
func (d *S3Driver) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Ping verifies connectivity by performing a HeadBucket on the configured bucket.
func (d *S3Driver) Ping(ctx context.Context) error {
	client, cfg, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		return fmt.Errorf("s3driver: ping: %w", err)
	}
	return nil
}

// Put stores an object in S3.
func (d *S3Driver) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyPutOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	ct := cfg.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	// Read all data to know the size for PutObject.
	// For large objects, use multipart upload instead.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("s3driver: read data: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(ct),
	}

	if cfg.StorageClass != "" {
		input.StorageClass = types.StorageClass(cfg.StorageClass)
	}

	if len(cfg.Metadata) > 0 {
		input.Metadata = cfg.Metadata
	}

	result, err := client.PutObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3driver: put %q: %w", key, err)
	}

	etag := ""
	if result.ETag != nil {
		etag = strings.Trim(*result.ETag, "\"")
	}

	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         int64(len(data)),
		ContentType:  ct,
		ETag:         etag,
		LastModified: time.Now().UTC(),
		Metadata:     cfg.Metadata,
		StorageClass: cfg.StorageClass,
		VersionID:    versionID,
	}

	return info, nil
}

// Get retrieves an object from S3.
func (d *S3Driver) Get(ctx context.Context, bucket, key string, opts ...driver.GetOption) (*driver.ObjectReader, error) {
	getCfg := driver.ApplyGetOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	if getCfg.Range != "" {
		input.Range = aws.String(getCfg.Range)
	}
	if getCfg.VersionID != "" {
		input.VersionId = aws.String(getCfg.VersionID)
	}

	result, err := client.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3driver: get %q: %w", key, err)
	}

	ct := ""
	if result.ContentType != nil {
		ct = *result.ContentType
	}

	etag := ""
	if result.ETag != nil {
		etag = strings.Trim(*result.ETag, "\"")
	}

	var lastMod time.Time
	if result.LastModified != nil {
		lastMod = *result.LastModified
	}

	var size int64
	if result.ContentLength != nil {
		size = *result.ContentLength
	}

	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}

	var meta map[string]string
	if len(result.Metadata) > 0 {
		meta = result.Metadata
	}

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
		Metadata:     meta,
		VersionID:    versionID,
	}

	return &driver.ObjectReader{
		ReadCloser: result.Body,
		Info:       info,
	}, nil
}

// Delete removes an object from S3.
func (d *S3Driver) Delete(ctx context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3driver: delete %q: %w", key, err)
	}
	return nil
}

// Head returns object metadata without content.
func (d *S3Driver) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	result, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3driver: head %q: %w", key, err)
	}

	ct := ""
	if result.ContentType != nil {
		ct = *result.ContentType
	}

	etag := ""
	if result.ETag != nil {
		etag = strings.Trim(*result.ETag, "\"")
	}

	var lastMod time.Time
	if result.LastModified != nil {
		lastMod = *result.LastModified
	}

	var size int64
	if result.ContentLength != nil {
		size = *result.ContentLength
	}

	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}

	var meta map[string]string
	if len(result.Metadata) > 0 {
		meta = result.Metadata
	}

	storageClass := ""
	if result.StorageClass != "" {
		storageClass = string(result.StorageClass)
	}

	return &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
		Metadata:     meta,
		VersionID:    versionID,
		StorageClass: storageClass,
	}, nil
}

// List returns objects matching the given options.
func (d *S3Driver) List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	cfg := driver.ApplyListOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(int32(cfg.MaxKeys)),
	}

	if cfg.Prefix != "" {
		input.Prefix = aws.String(cfg.Prefix)
	}
	if cfg.Delimiter != "" {
		input.Delimiter = aws.String(cfg.Delimiter)
	}
	if cfg.Cursor != "" {
		input.StartAfter = aws.String(cfg.Cursor)
	}

	result, err := client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3driver: list bucket %q: %w", bucket, err)
	}

	infos := make([]driver.ObjectInfo, 0, len(result.Contents))
	for _, obj := range result.Contents {
		etag := ""
		if obj.ETag != nil {
			etag = strings.Trim(*obj.ETag, "\"")
		}
		key := ""
		if obj.Key != nil {
			key = *obj.Key
		}
		var lastMod time.Time
		if obj.LastModified != nil {
			lastMod = *obj.LastModified
		}
		var size int64
		if obj.Size != nil {
			size = *obj.Size
		}

		infos = append(infos, driver.ObjectInfo{
			Key:          key,
			Size:         size,
			ETag:         etag,
			LastModified: lastMod,
			StorageClass: string(obj.StorageClass),
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Key < infos[j].Key
	})

	nextToken := ""
	if result.NextContinuationToken != nil {
		nextToken = *result.NextContinuationToken
	}

	return driver.NewObjectIterator(infos, nextToken), nil
}

// Copy copies an object within or across buckets.
func (d *S3Driver) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, _ ...driver.CopyOption) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	copySource := srcBucket + "/" + srcKey

	result, err := client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(copySource),
	})
	if err != nil {
		return nil, fmt.Errorf("s3driver: copy %q → %q: %w", srcKey, dstKey, err)
	}

	etag := ""
	if result.CopyObjectResult != nil && result.CopyObjectResult.ETag != nil {
		etag = strings.Trim(*result.CopyObjectResult.ETag, "\"")
	}

	var lastMod time.Time
	if result.CopyObjectResult != nil && result.CopyObjectResult.LastModified != nil {
		lastMod = *result.CopyObjectResult.LastModified
	}

	// CopyObject doesn't return size, so do a Head to get it.
	headInfo, headErr := d.Head(ctx, dstBucket, dstKey)
	if headErr == nil {
		return &driver.ObjectInfo{
			Key:          dstKey,
			Size:         headInfo.Size,
			ContentType:  headInfo.ContentType,
			ETag:         etag,
			LastModified: lastMod,
			Metadata:     headInfo.Metadata,
			StorageClass: headInfo.StorageClass,
		}, nil
	}

	return &driver.ObjectInfo{
		Key:          dstKey,
		ETag:         etag,
		LastModified: lastMod,
	}, nil
}

// CreateBucket creates a new S3 bucket.
func (d *S3Driver) CreateBucket(ctx context.Context, name string, opts ...driver.BucketOption) error {
	bucketCfg := driver.ApplyBucketOptions(opts...)
	client, s3cfg, err := d.getClient()
	if err != nil {
		return err
	}

	input := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}

	region := bucketCfg.Region
	if region == "" {
		region = s3cfg.Region
	}

	// us-east-1 must not set LocationConstraint.
	if region != "" && region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}

	_, err = client.CreateBucket(ctx, input)
	if err != nil {
		return fmt.Errorf("s3driver: create bucket %q: %w", name, err)
	}
	return nil
}

// DeleteBucket removes an S3 bucket.
func (d *S3Driver) DeleteBucket(ctx context.Context, name string) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("s3driver: delete bucket %q: %w", name, err)
	}
	return nil
}

// ListBuckets returns all accessible buckets.
func (d *S3Driver) ListBuckets(ctx context.Context) ([]driver.BucketInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("s3driver: list buckets: %w", err)
	}

	buckets := make([]driver.BucketInfo, 0, len(result.Buckets))
	for _, b := range result.Buckets {
		name := ""
		if b.Name != nil {
			name = *b.Name
		}
		var createdAt time.Time
		if b.CreationDate != nil {
			createdAt = *b.CreationDate
		}
		buckets = append(buckets, driver.BucketInfo{
			Name:      name,
			CreatedAt: createdAt,
		})
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}

// Client returns the underlying S3 client for advanced use cases.
func (d *S3Driver) Client() *s3.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

// Unwrap extracts the typed S3Driver from a Trove handle.
func Unwrap(t interface{ Driver() driver.Driver }) *S3Driver {
	if sd, ok := t.Driver().(*S3Driver); ok {
		return sd
	}
	return nil
}

// --- Internal helpers ---

func (d *S3Driver) getClient() (*s3.Client, *s3Config, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, nil, fmt.Errorf("s3driver: driver is closed")
	}
	if d.client == nil {
		return nil, nil, fmt.Errorf("s3driver: driver not opened")
	}
	return d.client, d.cfg, nil
}

func (d *S3Driver) buildAWSConfig(ctx context.Context, s3cfg *s3Config) (aws.Config, error) {
	var optFns []func(*config.LoadOptions) error

	optFns = append(optFns, config.WithRegion(s3cfg.Region))

	if s3cfg.AccessKey != "" && s3cfg.SecretKey != "" {
		optFns = append(optFns, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(s3cfg.AccessKey, s3cfg.SecretKey, ""),
		))
	}

	return config.LoadDefaultConfig(ctx, optFns...)
}

func init() {
	driver.Register("s3", func() driver.Driver { return New() })
}
