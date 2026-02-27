package azuredriver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"

	"github.com/xraph/trove/driver"
)

// GetRange retrieves a byte range of an object. The offset is the starting
// byte position and length is the number of bytes to read. A length of -1
// reads from offset to end.
func (d *AzureDriver) GetRange(ctx context.Context, bucket, key string, offset, length int64) (*driver.ObjectReader, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	downloadOpts := &blob.DownloadStreamOptions{
		Range: blob.HTTPRange{
			Offset: offset,
		},
	}

	if length >= 0 {
		downloadOpts.Range.Count = length
	}

	blobClient := client.ServiceClient().NewContainerClient(bucket).NewBlobClient(key)
	resp, err := blobClient.DownloadStream(ctx, downloadOpts)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: get range %q [%d:%d]: %w", key, offset, length, err)
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

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
	}

	return &driver.ObjectReader{
		ReadCloser: resp.Body,
		Info:       info,
	}, nil
}
