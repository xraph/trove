package extension

import "github.com/xraph/trove"

// Config holds extension configuration.
type Config struct {
	// StorageDriver is the DSN for the storage backend (e.g., "local:///data", "s3://bucket").
	StorageDriver string `json:"storage_driver" yaml:"storage_driver" mapstructure:"storage_driver"`

	// BasePath is the HTTP route prefix (default: "/trove/v1").
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

	// RequireConfig causes Register() to fail if YAML config is missing.
	RequireConfig bool `json:"-" yaml:"-"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BasePath:      "/trove/v1",
		DefaultBucket: "default",
	}
}

// ToTroveOptions converts config values to trove.Option slice.
func (c Config) ToTroveOptions() []trove.Option {
	var opts []trove.Option

	if c.DefaultBucket != "" {
		opts = append(opts, trove.WithDefaultBucket(c.DefaultBucket))
	}

	return opts
}
