package s3driver

import (
	"fmt"
	"strings"

	"github.com/xraph/trove/driver"
)

// s3Config holds parsed S3 connection parameters.
type s3Config struct {
	AccessKey      string
	SecretKey      string
	Region         string
	Bucket         string
	Endpoint       string
	ForcePathStyle bool
}

// parseDSN parses an S3 DSN string into an s3Config.
//
// Supported formats:
//
//	s3://REGION/BUCKET
//	s3://ACCESS_KEY:SECRET@REGION/BUCKET
//	s3://ACCESS_KEY:SECRET@REGION/BUCKET?endpoint=http://localhost:9000&path_style=true
func parseDSN(dsn string) (*s3Config, error) {
	parsed, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("s3driver: %w", err)
	}

	if parsed.Scheme != "s3" {
		return nil, fmt.Errorf("s3driver: expected scheme \"s3\", got %q", parsed.Scheme)
	}

	cfg := &s3Config{
		AccessKey: parsed.User,
		SecretKey: parsed.Password,
		Region:    parsed.Host,
	}

	// Path is /BUCKET — strip leading slash.
	cfg.Bucket = strings.TrimPrefix(parsed.Path, "/")
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3driver: DSN missing bucket in path")
	}

	// Parse optional query parameters.
	if v := parsed.Params.Get("endpoint"); v != "" {
		cfg.Endpoint = v
	}
	if v := parsed.Params.Get("path_style"); v == "true" || v == "1" {
		cfg.ForcePathStyle = true
	}

	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	return cfg, nil
}
