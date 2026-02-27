package s3driver

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// PresignGet generates a pre-signed URL for downloading an object.
func (d *S3Driver) PresignGet(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return "", fmt.Errorf("s3driver: driver is closed")
	}
	if d.psClient == nil {
		d.mu.RUnlock()
		return "", fmt.Errorf("s3driver: driver not opened")
	}
	psClient := d.psClient
	d.mu.RUnlock()

	result, err := psClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("s3driver: presign get %q: %w", key, err)
	}

	return result.URL, nil
}

// PresignPut generates a pre-signed URL for uploading an object.
func (d *S3Driver) PresignPut(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return "", fmt.Errorf("s3driver: driver is closed")
	}
	if d.psClient == nil {
		d.mu.RUnlock()
		return "", fmt.Errorf("s3driver: driver not opened")
	}
	psClient := d.psClient
	d.mu.RUnlock()

	result, err := psClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("s3driver: presign put %q: %w", key, err)
	}

	return result.URL, nil
}
