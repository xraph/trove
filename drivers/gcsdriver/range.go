package gcsdriver

import (
	"context"
	"fmt"

	"github.com/xraph/trove/driver"
)

// GetRange retrieves a byte range of an object. The offset is the starting
// byte position and length is the number of bytes to read. A length of -1
// reads from offset to end.
func (d *GCSDriver) GetRange(ctx context.Context, bucket, key string, offset, length int64) (*driver.ObjectReader, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	obj := client.Bucket(bucket).Object(key)

	reader, err := obj.NewRangeReader(ctx, offset, length)
	if err != nil {
		return nil, fmt.Errorf("gcsdriver: get range %q [%d:%d]: %w", key, offset, length, err)
	}

	attrs, err := obj.Attrs(ctx)
	if err != nil {
		reader.Close()
		return nil, fmt.Errorf("gcsdriver: get range %q attrs: %w", key, err)
	}

	size := reader.Remain()

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  attrs.ContentType,
		ETag:         attrs.Etag,
		LastModified: attrs.Updated,
	}

	return &driver.ObjectReader{
		ReadCloser: reader,
		Info:       info,
	}, nil
}
