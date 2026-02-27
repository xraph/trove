// Package azuredriver provides an Azure Blob Storage driver for Trove.
//
// The Azure driver stores objects in Azure Blob Storage. It implements the
// core driver.Driver interface plus MultipartDriver, PresignDriver,
// and RangeDriver capability interfaces.
//
// DSN format:
//
//	azure://ACCOUNT_NAME/CONTAINER?key=ACCESS_KEY
//	azure://ACCOUNT_NAME/CONTAINER?connection_string=...
//	azure://ACCOUNT_NAME/CONTAINER?endpoint=http://127.0.0.1:10000
//
// Usage:
//
//	drv := azuredriver.New()
//	drv.Open(ctx, "azure://myaccount/mycontainer?key=mykey")
//	t, err := trove.Open(drv)
package azuredriver

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/xraph/trove/driver"
)

// Compile-time interface checks.
var (
	_ driver.Driver          = (*AzureDriver)(nil)
	_ driver.MultipartDriver = (*AzureDriver)(nil)
	_ driver.PresignDriver   = (*AzureDriver)(nil)
	_ driver.RangeDriver     = (*AzureDriver)(nil)
)

// AzureDriver implements driver.Driver for Azure Blob Storage.
type AzureDriver struct {
	mu     sync.RWMutex
	client *azblob.Client
	cfg    *azureConfig
	closed bool
}

// New creates a new Azure Blob Storage driver.
func New() *AzureDriver {
	return &AzureDriver{}
}

// Name returns "azure".
func (d *AzureDriver) Name() string { return "azure" }

// Open initializes the Azure client using the given DSN and options.
func (d *AzureDriver) Open(_ context.Context, dsn string, opts ...driver.Option) error {
	drvCfg := driver.ApplyOptions(opts...)

	azCfg, err := parseDSN(dsn)
	if err != nil {
		return err
	}

	if drvCfg.Endpoint != "" {
		azCfg.Endpoint = drvCfg.Endpoint
	}

	var client *azblob.Client

	switch {
	case azCfg.ConnectionString != "":
		client, err = azblob.NewClientFromConnectionString(azCfg.ConnectionString, nil)
	case azCfg.AccountKey != "":
		cred, credErr := azblob.NewSharedKeyCredential(azCfg.AccountName, azCfg.AccountKey)
		if credErr != nil {
			return fmt.Errorf("azuredriver: create credential: %w", credErr)
		}
		serviceURL := azCfg.Endpoint
		if serviceURL == "" {
			serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", azCfg.AccountName)
		}
		client, err = azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	default:
		serviceURL := azCfg.Endpoint
		if serviceURL == "" {
			serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", azCfg.AccountName)
		}
		client, err = azblob.NewClientWithNoCredential(serviceURL, nil)
	}

	if err != nil {
		return fmt.Errorf("azuredriver: create client: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.client = client
	d.cfg = azCfg
	d.closed = false
	return nil
}

// Close marks the driver as closed.
func (d *AzureDriver) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Ping verifies Azure connectivity by getting container properties.
func (d *AzureDriver) Ping(ctx context.Context) error {
	client, cfg, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.ServiceClient().NewContainerClient(cfg.Container).GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("azuredriver: ping: %w", err)
	}
	return nil
}

// Put stores an object in Azure Blob Storage.
func (d *AzureDriver) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	cfg := driver.ApplyPutOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	ct := cfg.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: read data: %w", err)
	}

	uploadOpts := &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &ct,
		},
	}

	if len(cfg.Metadata) > 0 {
		azMeta := make(map[string]*string, len(cfg.Metadata))
		for k, v := range cfg.Metadata {
			v := v
			azMeta[k] = &v
		}
		uploadOpts.Metadata = azMeta
	}

	_, err = client.UploadBuffer(ctx, bucket, key, data, uploadOpts)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: put %q: %w", key, err)
	}

	now := time.Now().UTC()
	info := &driver.ObjectInfo{
		Key:          key,
		Size:         int64(len(data)),
		ContentType:  ct,
		ETag:         fmt.Sprintf("%x-%x", len(data), now.UnixNano()),
		LastModified: now,
		Metadata:     cfg.Metadata,
		StorageClass: cfg.StorageClass,
	}

	return info, nil
}

// Get retrieves an object from Azure Blob Storage.
func (d *AzureDriver) Get(ctx context.Context, bucket, key string, _ ...driver.GetOption) (*driver.ObjectReader, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.DownloadStream(ctx, bucket, key, nil)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: get %q: %w", key, err)
	}

	ct := ""
	if resp.ContentType != nil {
		ct = *resp.ContentType
	}

	etag := ""
	if resp.ETag != nil {
		etag = string(*resp.ETag)
		etag = strings.Trim(etag, "\"")
	}

	var lastMod time.Time
	if resp.LastModified != nil {
		lastMod = *resp.LastModified
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}

	var meta map[string]string
	if len(resp.Metadata) > 0 {
		meta = make(map[string]string, len(resp.Metadata))
		for k, v := range resp.Metadata {
			if v != nil {
				meta[k] = *v
			}
		}
	}

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
		Metadata:     meta,
	}

	return &driver.ObjectReader{
		ReadCloser: resp.Body,
		Info:       info,
	}, nil
}

// Delete removes an object from Azure Blob Storage.
func (d *AzureDriver) Delete(ctx context.Context, bucket, key string, _ ...driver.DeleteOption) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.DeleteBlob(ctx, bucket, key, nil)
	if err != nil {
		// Idempotent: ignore "not found" errors.
		if strings.Contains(err.Error(), "BlobNotFound") || strings.Contains(err.Error(), "404") {
			return nil
		}
		return fmt.Errorf("azuredriver: delete %q: %w", key, err)
	}
	return nil
}

// Head returns object metadata without content.
func (d *AzureDriver) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	blobClient := client.ServiceClient().NewContainerClient(bucket).NewBlobClient(key)
	resp, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: head %q: %w", key, err)
	}

	ct := ""
	if resp.ContentType != nil {
		ct = *resp.ContentType
	}

	etag := ""
	if resp.ETag != nil {
		etag = string(*resp.ETag)
		etag = strings.Trim(etag, "\"")
	}

	var lastMod time.Time
	if resp.LastModified != nil {
		lastMod = *resp.LastModified
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}

	var meta map[string]string
	if len(resp.Metadata) > 0 {
		meta = make(map[string]string, len(resp.Metadata))
		for k, v := range resp.Metadata {
			if v != nil {
				meta[k] = *v
			}
		}
	}

	return &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
		Metadata:     meta,
	}, nil
}

// List returns objects matching the given options.
func (d *AzureDriver) List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	cfg := driver.ApplyListOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	containerClient := client.ServiceClient().NewContainerClient(bucket)

	listOpts := &container.ListBlobsFlatOptions{}
	if cfg.Prefix != "" {
		listOpts.Prefix = &cfg.Prefix
	}

	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	maxResults := int32(maxKeys + 1) // +1 for pagination detection
	listOpts.MaxResults = &maxResults

	if cfg.Cursor != "" {
		listOpts.Marker = &cfg.Cursor
	}

	pager := containerClient.NewListBlobsFlatPager(listOpts)

	var infos []driver.ObjectInfo
	var nextToken string

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("azuredriver: list bucket %q: %w", bucket, err)
		}

		for _, item := range resp.Segment.BlobItems {
			if item.Name == nil {
				continue
			}

			var size int64
			ct := ""
			etag := ""
			var lastMod time.Time

			if item.Properties != nil {
				if item.Properties.ContentLength != nil {
					size = *item.Properties.ContentLength
				}
				if item.Properties.ContentType != nil {
					ct = *item.Properties.ContentType
				}
				if item.Properties.ETag != nil {
					etag = string(*item.Properties.ETag)
					etag = strings.Trim(etag, "\"")
				}
				if item.Properties.LastModified != nil {
					lastMod = *item.Properties.LastModified
				}
			}

			infos = append(infos, driver.ObjectInfo{
				Key:          *item.Name,
				Size:         size,
				ContentType:  ct,
				ETag:         etag,
				LastModified: lastMod,
			})

			if len(infos) > maxKeys {
				nextToken = infos[maxKeys-1].Key
				infos = infos[:maxKeys]
				goto done
			}
		}
	}

done:
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Key < infos[j].Key
	})

	return driver.NewObjectIterator(infos, nextToken), nil
}

// Copy copies an object within or across containers.
func (d *AzureDriver) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, _ ...driver.CopyOption) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	srcBlobClient := client.ServiceClient().NewContainerClient(srcBucket).NewBlobClient(srcKey)
	dstBlobClient := client.ServiceClient().NewContainerClient(dstBucket).NewBlobClient(dstKey)

	_, err = dstBlobClient.StartCopyFromURL(ctx, srcBlobClient.URL(), nil)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: copy %q → %q: %w", srcKey, dstKey, err)
	}

	// Get destination attrs.
	resp, err := dstBlobClient.GetProperties(ctx, nil)
	if err != nil {
		return &driver.ObjectInfo{
			Key:          dstKey,
			LastModified: time.Now().UTC(),
		}, nil
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}

	ct := ""
	if resp.ContentType != nil {
		ct = *resp.ContentType
	}

	etag := ""
	if resp.ETag != nil {
		etag = string(*resp.ETag)
		etag = strings.Trim(etag, "\"")
	}

	var lastMod time.Time
	if resp.LastModified != nil {
		lastMod = *resp.LastModified
	}

	return &driver.ObjectInfo{
		Key:          dstKey,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
	}, nil
}

// CreateBucket creates a new Azure container.
func (d *AzureDriver) CreateBucket(ctx context.Context, name string, _ ...driver.BucketOption) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.CreateContainer(ctx, name, nil)
	if err != nil {
		return fmt.Errorf("azuredriver: create bucket %q: %w", name, err)
	}
	return nil
}

// DeleteBucket removes an Azure container.
func (d *AzureDriver) DeleteBucket(ctx context.Context, name string) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.DeleteContainer(ctx, name, nil)
	if err != nil {
		return fmt.Errorf("azuredriver: delete bucket %q: %w", name, err)
	}
	return nil
}

// ListBuckets returns all accessible containers.
func (d *AzureDriver) ListBuckets(ctx context.Context) ([]driver.BucketInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	pager := client.NewListContainersPager(nil)
	var buckets []driver.BucketInfo

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("azuredriver: list buckets: %w", err)
		}

		for _, item := range resp.ContainerItems {
			if item.Name == nil {
				continue
			}
			var createdAt time.Time
			if item.Properties != nil && item.Properties.LastModified != nil {
				createdAt = *item.Properties.LastModified
			}
			buckets = append(buckets, driver.BucketInfo{
				Name:      *item.Name,
				CreatedAt: createdAt,
			})
		}
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}

// Client returns the underlying Azure Blob client for advanced use cases.
func (d *AzureDriver) Client() *azblob.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

// Unwrap extracts the typed AzureDriver from a Trove handle.
func Unwrap(t interface{ Driver() driver.Driver }) *AzureDriver {
	if ad, ok := t.Driver().(*AzureDriver); ok {
		return ad
	}
	return nil
}

// --- Internal helpers ---

func (d *AzureDriver) getClient() (*azblob.Client, *azureConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, nil, fmt.Errorf("azuredriver: driver is closed")
	}
	if d.client == nil {
		return nil, nil, fmt.Errorf("azuredriver: driver not opened")
	}
	return d.client, d.cfg, nil
}

func init() {
	driver.Register("azure", func() driver.Driver { return New() })
}
