package gcsdriver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/storage"
)

// PresignGet generates a pre-signed URL for downloading an object.
func (d *GCSDriver) PresignGet(_ context.Context, bucket, key string, expires time.Duration) (string, error) {
	client, _, err := d.getClient()
	if err != nil {
		return "", err
	}

	url, err := client.Bucket(bucket).SignedURL(key, &storage.SignedURLOptions{
		Method:  http.MethodGet,
		Expires: time.Now().Add(expires),
	})
	if err != nil {
		return "", fmt.Errorf("gcsdriver: presign get %q: %w", key, err)
	}

	return url, nil
}

// PresignPut generates a pre-signed URL for uploading an object.
func (d *GCSDriver) PresignPut(_ context.Context, bucket, key string, expires time.Duration) (string, error) {
	client, _, err := d.getClient()
	if err != nil {
		return "", err
	}

	url, err := client.Bucket(bucket).SignedURL(key, &storage.SignedURLOptions{
		Method:  http.MethodPut,
		Expires: time.Now().Add(expires),
	})
	if err != nil {
		return "", fmt.Errorf("gcsdriver: presign put %q: %w", key, err)
	}

	return url, nil
}
