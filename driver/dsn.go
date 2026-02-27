package driver

import (
	"fmt"
	"net/url"
)

// DSNConfig holds parsed DSN components.
type DSNConfig struct {
	// Scheme is the protocol scheme (e.g., "s3", "gcs", "file", "mem").
	Scheme string

	// User is the username or access key.
	User string

	// Password is the password or secret key.
	Password string

	// Host is the hostname and optional port.
	Host string

	// Path is the path component (e.g., bucket, directory).
	Path string

	// Params holds query string parameters.
	Params url.Values
}

// ParseDSN parses a DSN string into its components.
//
// Supported formats:
//
//	scheme://host/path?params
//	scheme://user:password@host/path?params
//	scheme:///path (host-less, e.g., file:///tmp/storage)
func ParseDSN(dsn string) (*DSNConfig, error) {
	if dsn == "" {
		return nil, fmt.Errorf("driver: empty DSN")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("driver: parse DSN %q: %w", dsn, err)
	}

	if u.Scheme == "" {
		return nil, fmt.Errorf("driver: DSN %q missing scheme", dsn)
	}

	cfg := &DSNConfig{
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   u.Path,
		Params: u.Query(),
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	return cfg, nil
}
