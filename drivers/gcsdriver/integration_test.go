//go:build integration

package gcsdriver

import (
	"context"
	"os"
	"testing"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/trovetest"
)

// Integration tests require a running fake GCS server.
// Use Docker: docker run -d -p 4443:4443 fsouza/fake-gcs-server -scheme http
//
// Environment variables:
//
//	GCS_ENDPOINT  - GCS server endpoint (default: http://localhost:4443/storage/v1/)
//	GCS_PROJECT   - GCS project ID (default: test-project)

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func TestConformanceSuite(t *testing.T) {
	endpoint := getEnvOrDefault("GCS_ENDPOINT", "http://localhost:4443/storage/v1/")
	project := getEnvOrDefault("GCS_PROJECT", "test-project")

	trovetest.RunDriverSuite(t, func(t *testing.T) driver.Driver {
		t.Helper()

		dsn := "gcs://" + project + "/test-bucket?endpoint=" + endpoint

		drv := New()
		if err := drv.Open(context.Background(), dsn); err != nil {
			t.Fatalf("open gcs driver: %v", err)
		}

		t.Cleanup(func() {
			drv.Close(context.Background())
		})

		return drv
	})
}
