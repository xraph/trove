//go:build integration

package sftpdriver

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/trovetest"
)

// Integration tests require a running SFTP server.
// Use Docker: docker run -p 2222:22 -d atmoz/sftp user:pass:::upload
//
// Environment variables:
//
//	SFTP_HOST     - SFTP server host (default: localhost)
//	SFTP_PORT     - SFTP server port (default: 2222)
//	SFTP_USER     - SFTP username (default: user)
//	SFTP_PASSWORD - SFTP password (default: pass)

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func TestConformanceSuite(t *testing.T) {
	host := getEnvOrDefault("SFTP_HOST", "localhost")
	port := getEnvOrDefault("SFTP_PORT", "2222")
	user := getEnvOrDefault("SFTP_USER", "user")
	pass := getEnvOrDefault("SFTP_PASSWORD", "pass")

	trovetest.RunDriverSuite(t, func(t *testing.T) driver.Driver {
		t.Helper()

		// Each test gets its own subdirectory to avoid interference.
		basePath := fmt.Sprintf("/upload/test-%d", os.Getpid())
		dsn := fmt.Sprintf("sftp://%s:%s@%s:%s%s", user, pass, host, port, basePath)

		drv := New()
		if err := drv.Open(context.Background(), dsn); err != nil {
			t.Fatalf("open sftp driver: %v", err)
		}

		t.Cleanup(func() {
			drv.Close(context.Background())
		})

		return drv
	})
}
