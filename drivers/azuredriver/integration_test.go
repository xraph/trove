//go:build integration

package azuredriver

import (
	"context"
	"os"
	"testing"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/trovetest"
)

// Integration tests require a running Azurite emulator.
// Use Docker: docker run -d -p 10000:10000 mcr.microsoft.com/azure-storage/azurite azurite-blob --blobHost 0.0.0.0
//
// Environment variables:
//
//	AZURE_ENDPOINT     - Azurite endpoint (default: http://127.0.0.1:10000/devstoreaccount1)
//	AZURE_ACCOUNT_NAME - Account name (default: devstoreaccount1)
//	AZURE_ACCOUNT_KEY  - Account key (default: Azurite well-known key)

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func TestConformanceSuite(t *testing.T) {
	endpoint := getEnvOrDefault("AZURE_ENDPOINT", "http://127.0.0.1:10000/devstoreaccount1")
	accountName := getEnvOrDefault("AZURE_ACCOUNT_NAME", "devstoreaccount1")
	// Well-known Azurite account key.
	accountKey := getEnvOrDefault("AZURE_ACCOUNT_KEY", "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==")

	trovetest.RunDriverSuite(t, func(t *testing.T) driver.Driver {
		t.Helper()

		dsn := "azure://" + accountName + "/test-container?key=" + accountKey + "&endpoint=" + endpoint

		drv := New()
		if err := drv.Open(context.Background(), dsn); err != nil {
			t.Fatalf("open azure driver: %v", err)
		}

		t.Cleanup(func() {
			drv.Close(context.Background())
		})

		return drv
	})
}
