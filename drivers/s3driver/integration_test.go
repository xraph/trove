//go:build integration

package s3driver

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/trovetest"
)

// Integration tests require a running S3-compatible service (e.g., MinIO).
//
// Start MinIO locally:
//
//	docker run -d --name minio \
//	  -p 9000:9000 \
//	  -e MINIO_ROOT_USER=minioadmin \
//	  -e MINIO_ROOT_PASSWORD=minioadmin \
//	  minio/minio server /data
//
// Run tests:
//
//	cd drivers/s3driver
//	go test -tags integration ./...
//
// Environment variables:
//
//	S3_ENDPOINT   (default: http://localhost:9000)
//	S3_ACCESS_KEY (default: minioadmin)
//	S3_SECRET_KEY (default: minioadmin)
//	S3_REGION     (default: us-east-1)

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func integrationDSN(bucket string) string {
	endpoint := getEnvOrDefault("S3_ENDPOINT", "http://localhost:9000")
	accessKey := getEnvOrDefault("S3_ACCESS_KEY", "minioadmin")
	secretKey := getEnvOrDefault("S3_SECRET_KEY", "minioadmin")
	region := getEnvOrDefault("S3_REGION", "us-east-1")

	return fmt.Sprintf("s3://%s:%s@%s/%s?endpoint=%s&path_style=true",
		accessKey, secretKey, region, bucket, endpoint)
}

func newIntegrationDriver(t *testing.T) driver.Driver {
	t.Helper()

	// Each test gets a unique bucket to avoid collisions.
	bucket := fmt.Sprintf("trove-test-%d", rand.Int63()) //nolint:gosec
	dsn := integrationDSN(bucket)

	drv := New()
	ctx := context.Background()

	err := drv.Open(ctx, dsn)
	require.NoError(t, err)

	// Create the test bucket.
	err = drv.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Cleanup(func() {
		// Best-effort cleanup: delete bucket and close driver.
		_ = drv.DeleteBucket(ctx, bucket)
		_ = drv.Close(ctx)
	})

	return drv
}

func TestConformanceSuite(t *testing.T) {
	trovetest.RunDriverSuite(t, func(t *testing.T) driver.Driver {
		return newIntegrationDriver(t)
	})
}
