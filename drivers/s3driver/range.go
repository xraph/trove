package s3driver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/xraph/trove/driver"
)

// GetRange retrieves a byte range of an object. The offset is the starting
// byte position and length is the number of bytes to read. A length of -1
// reads from offset to end.
func (d *S3Driver) GetRange(ctx context.Context, bucket, key string, offset, length int64) (*driver.ObjectReader, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	var rangeHeader string
	if length < 0 {
		rangeHeader = fmt.Sprintf("bytes=%d-", offset)
	} else {
		rangeHeader = fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
	}

	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Range:  aws.String(rangeHeader),
	})
	if err != nil {
		return nil, fmt.Errorf("s3driver: get range %q [%s]: %w", key, rangeHeader, err)
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

	info := &driver.ObjectInfo{
		Key:          key,
		Size:         size,
		ContentType:  ct,
		ETag:         etag,
		LastModified: lastMod,
	}

	return &driver.ObjectReader{
		ReadCloser: result.Body,
		Info:       info,
	}, nil
}
