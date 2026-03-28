package extension

import (
	"fmt"

	"github.com/xraph/trove"
)

// Config holds extension configuration.
type Config struct {
	// StorageDriver is the DSN for the storage backend (e.g., "file:///data", "s3://bucket").
	StorageDriver string `json:"storage_driver" yaml:"storage_driver" mapstructure:"storage_driver"`

	// BasePath is the HTTP route prefix (default: "/trove").
	BasePath string `json:"base_path" yaml:"base_path" mapstructure:"base_path"`

	// DisableRoutes skips HTTP handler registration.
	DisableRoutes bool `json:"disable_routes" yaml:"disable_routes" mapstructure:"disable_routes"`

	// DisableMigrate skips automatic database migration.
	DisableMigrate bool `json:"disable_migrate" yaml:"disable_migrate" mapstructure:"disable_migrate"`

	// GroveDatabase is the named Grove DB to resolve from DI (default: "").
	GroveDatabase string `json:"grove_database" yaml:"grove_database" mapstructure:"grove_database"`

	// DefaultBucket is the default storage bucket.
	DefaultBucket string `json:"default_bucket" yaml:"default_bucket" mapstructure:"default_bucket"`

	// EnableCAS enables content-addressable storage.
	EnableCAS bool `json:"enable_cas" yaml:"enable_cas" mapstructure:"enable_cas"`

	// EnableEncryption auto-configures encryption middleware.
	EnableEncryption bool `json:"enable_encryption" yaml:"enable_encryption" mapstructure:"enable_encryption"`

	// EnableCompression auto-configures compression middleware.
	EnableCompression bool `json:"enable_compression" yaml:"enable_compression" mapstructure:"enable_compression"`

	// Stores defines named file store connections for multi-store mode.
	// When set, each entry creates a separate trove.Trove registered in DI.
	Stores []FileStoreConfig `json:"stores" yaml:"stores" mapstructure:"stores"`

	// Default is the name of the default store when using multi-store mode.
	// If empty, the first entry in Stores is the default.
	Default string `json:"default" yaml:"default" mapstructure:"default"`

	// RequireConfig causes Register() to fail if YAML config is missing.
	RequireConfig bool `json:"-" yaml:"-"`
}

// FileStoreConfig defines a single named file store.
type FileStoreConfig struct {
	// Name is the unique identifier for this store.
	Name string `json:"name" yaml:"name" mapstructure:"name"`

	// StorageDriver is the DSN for the storage backend
	// (e.g., "mem://", "file:///data", "s3://region/bucket").
	StorageDriver string `json:"storage_driver" yaml:"storage_driver" mapstructure:"storage_driver"`

	// GroveDatabase is the named Grove DB for this store's metadata.
	GroveDatabase string `json:"grove_database" yaml:"grove_database" mapstructure:"grove_database"`

	// DefaultBucket is the default bucket for this store.
	DefaultBucket string `json:"default_bucket" yaml:"default_bucket" mapstructure:"default_bucket"`

	// EnableCAS enables content-addressable storage for this store.
	EnableCAS bool `json:"enable_cas" yaml:"enable_cas" mapstructure:"enable_cas"`

	// EnableEncryption auto-configures encryption middleware for this store.
	EnableEncryption bool `json:"enable_encryption" yaml:"enable_encryption" mapstructure:"enable_encryption"`

	// EnableCompression auto-configures compression middleware for this store.
	EnableCompression bool `json:"enable_compression" yaml:"enable_compression" mapstructure:"enable_compression"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BasePath:      "/trove",
		DefaultBucket: "default",
	}
}

// Validate checks that the configuration is usable.
func (c *Config) Validate() error {
	seen := make(map[string]bool, len(c.Stores))
	for i, s := range c.Stores {
		if s.Name == "" {
			return fmt.Errorf("trove: stores[%d]: name is required", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("trove: stores[%d]: duplicate name %q", i, s.Name)
		}
		seen[s.Name] = true

		if s.StorageDriver == "" {
			return fmt.Errorf("trove: stores[%d] %q: storage_driver is required", i, s.Name)
		}
	}

	if c.Default != "" && len(c.Stores) > 0 {
		if !seen[c.Default] {
			return fmt.Errorf("trove: default store %q not found in stores list", c.Default)
		}
	}

	return nil
}

// ToTroveOptions converts config values to trove.Option slice.
func (c Config) ToTroveOptions() []trove.Option {
	var opts []trove.Option

	if c.DefaultBucket != "" {
		opts = append(opts, trove.WithDefaultBucket(c.DefaultBucket))
	}

	return opts
}
