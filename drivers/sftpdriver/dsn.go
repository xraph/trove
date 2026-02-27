package sftpdriver

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xraph/trove/driver"
)

// sftpConfig holds parsed SFTP connection parameters.
type sftpConfig struct {
	User     string
	Password string
	Host     string
	Port     int
	BasePath string
	KeyFile  string
}

// parseDSN parses an SFTP DSN string into an sftpConfig.
//
// Supported formats:
//
//	sftp://user:pass@host:22/basepath
//	sftp://user@host/basepath?key=/path/to/key
//	sftp://user:pass@host/basepath
func parseDSN(dsn string) (*sftpConfig, error) {
	parsed, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("sftpdriver: %w", err)
	}

	if parsed.Scheme != "sftp" {
		return nil, fmt.Errorf("sftpdriver: expected scheme \"sftp\", got %q", parsed.Scheme)
	}

	cfg := &sftpConfig{
		User:     parsed.User,
		Password: parsed.Password,
		Port:     22, // default
	}

	// Parse host:port.
	host := parsed.Host
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		portStr := host[idx+1:]
		p, err := strconv.Atoi(portStr)
		if err == nil {
			cfg.Port = p
			host = host[:idx]
		}
	}
	cfg.Host = host

	if cfg.Host == "" {
		return nil, fmt.Errorf("sftpdriver: DSN missing host")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("sftpdriver: DSN missing user")
	}

	// Path is the base directory on the remote.
	cfg.BasePath = parsed.Path
	if cfg.BasePath == "" {
		cfg.BasePath = "/"
	}

	// Optional query parameters.
	if v := parsed.Params.Get("key"); v != "" {
		cfg.KeyFile = v
	}

	return cfg, nil
}

// addr returns the host:port string for SSH dialing.
func (c *sftpConfig) addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
