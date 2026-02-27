package gcsdriver

import (
	"fmt"
	"strings"

	"github.com/xraph/trove/driver"
)

// gcsConfig holds parsed GCS connection parameters.
type gcsConfig struct {
	ProjectID       string
	Bucket          string
	CredentialsFile string
	Endpoint        string
}

// parseDSN parses a GCS DSN string into a gcsConfig.
//
// Supported formats:
//
//	gcs://PROJECT_ID/BUCKET
//	gcs://PROJECT_ID/BUCKET?credentials=/path/to/key.json
//	gcs://PROJECT_ID/BUCKET?endpoint=http://localhost:4443
func parseDSN(dsn string) (*gcsConfig, error) {
	parsed, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("gcsdriver: %w", err)
	}

	if parsed.Scheme != "gcs" {
		return nil, fmt.Errorf("gcsdriver: expected scheme \"gcs\", got %q", parsed.Scheme)
	}

	cfg := &gcsConfig{
		ProjectID: parsed.Host,
	}

	// Path is /BUCKET — strip leading slash.
	cfg.Bucket = strings.TrimPrefix(parsed.Path, "/")
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("gcsdriver: DSN missing bucket in path")
	}

	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("gcsdriver: DSN missing project ID")
	}

	// Parse optional query parameters.
	if v := parsed.Params.Get("credentials"); v != "" {
		cfg.CredentialsFile = v
	}
	if v := parsed.Params.Get("endpoint"); v != "" {
		cfg.Endpoint = v
	}

	return cfg, nil
}
