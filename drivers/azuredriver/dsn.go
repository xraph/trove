package azuredriver

import (
	"fmt"
	"strings"

	"github.com/xraph/trove/driver"
)

// azureConfig holds parsed Azure Blob Storage connection parameters.
type azureConfig struct {
	AccountName      string
	AccountKey       string
	Container        string
	ConnectionString string
	Endpoint         string
}

// parseDSN parses an Azure DSN string into an azureConfig.
//
// Supported formats:
//
//	azure://ACCOUNT_NAME/CONTAINER?key=ACCESS_KEY
//	azure://ACCOUNT_NAME/CONTAINER?connection_string=...
//	azure://ACCOUNT_NAME/CONTAINER?endpoint=http://127.0.0.1:10000
func parseDSN(dsn string) (*azureConfig, error) {
	parsed, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: %w", err)
	}

	if parsed.Scheme != "azure" {
		return nil, fmt.Errorf("azuredriver: expected scheme \"azure\", got %q", parsed.Scheme)
	}

	cfg := &azureConfig{
		AccountName: parsed.Host,
	}

	// Path is /CONTAINER — strip leading slash.
	cfg.Container = strings.TrimPrefix(parsed.Path, "/")
	if cfg.Container == "" {
		return nil, fmt.Errorf("azuredriver: DSN missing container in path")
	}

	if cfg.AccountName == "" {
		return nil, fmt.Errorf("azuredriver: DSN missing account name")
	}

	// Parse optional query parameters.
	if v := parsed.Params.Get("key"); v != "" {
		cfg.AccountKey = v
	}
	if v := parsed.Params.Get("connection_string"); v != "" {
		cfg.ConnectionString = v
	}
	if v := parsed.Params.Get("endpoint"); v != "" {
		cfg.Endpoint = v
	}

	return cfg, nil
}
