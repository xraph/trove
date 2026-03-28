package extension

import (
	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/extension/store"
)

// ExtOption configures the extension.
type ExtOption func(*Extension)

// WithStore provides a pre-built store (skips Grove DB resolution).
func WithStore(s store.Store) ExtOption {
	return func(e *Extension) {
		e.store = s
	}
}

// WithBasePath sets the HTTP route prefix.
func WithBasePath(path string) ExtOption {
	return func(e *Extension) {
		e.config.BasePath = path
	}
}

// WithConfig sets the full configuration.
func WithConfig(cfg Config) ExtOption {
	return func(e *Extension) {
		e.config = cfg
	}
}

// WithTroveOption adds a trove.Option applied during initialization.
func WithTroveOption(opt trove.Option) ExtOption {
	return func(e *Extension) {
		e.troveOpts = append(e.troveOpts, opt)
	}
}

// WithDisableRoutes skips HTTP handler registration.
func WithDisableRoutes() ExtOption {
	return func(e *Extension) {
		e.config.DisableRoutes = true
	}
}

// WithDisableMigrate skips automatic database migration.
func WithDisableMigrate() ExtOption {
	return func(e *Extension) {
		e.config.DisableMigrate = true
	}
}

// WithGroveDatabase sets the named Grove DB to resolve from DI.
func WithGroveDatabase(name string) ExtOption {
	return func(e *Extension) {
		e.config.GroveDatabase = name
		e.useGrove = true
	}
}

// --- Multi-Store Options ---

// fileStoreEntry holds the configuration for a named file store
// provided via WithFileStore or WithFileStoreDSN options.
type fileStoreEntry struct {
	name           string
	storageDriver  driver.Driver // pre-configured driver (nil if DSN-based)
	driverDSN      string        // for DSN-based resolution
	groveDatabase  string
	defaultBucket  string
	enableCAS      bool
	enableEncrypt  bool
	enableCompress bool
}

// WithFileStore adds a named file store with a pre-configured driver.
// Multiple calls create multiple named stores.
//
// Example:
//
//	ext := extension.New(
//	    extension.WithFileStore("primary", s3Driver),
//	    extension.WithFileStore("archive", localDriver),
//	    extension.WithDefaultFileStore("primary"),
//	)
func WithFileStore(name string, drv driver.Driver) ExtOption {
	return func(e *Extension) {
		e.fileStores = append(e.fileStores, fileStoreEntry{
			name:          name,
			storageDriver: drv,
		})
	}
}

// WithFileStoreDSN adds a named file store using a DSN.
// The driver will be resolved from the driver registry during Register().
//
// Example:
//
//	ext := extension.New(
//	    extension.WithFileStoreDSN("primary", "s3://us-east-1/my-bucket"),
//	    extension.WithFileStoreDSN("archive", "file:///data/archive"),
//	)
func WithFileStoreDSN(name, dsn string) ExtOption {
	return func(e *Extension) {
		e.fileStores = append(e.fileStores, fileStoreEntry{
			name:      name,
			driverDSN: dsn,
		})
	}
}

// WithDefaultFileStore sets which named store is the default.
// The default is used for backward-compatible Trove() access and unnamed DI injection.
func WithDefaultFileStore(name string) ExtOption {
	return func(e *Extension) {
		e.defaultStore = name
	}
}

// WithTroveOptionFor adds a trove.Option scoped to a specific named store.
func WithTroveOptionFor(storeName string, opt trove.Option) ExtOption {
	return func(e *Extension) {
		if e.storeOpts == nil {
			e.storeOpts = make(map[string][]trove.Option)
		}
		e.storeOpts[storeName] = append(e.storeOpts[storeName], opt)
	}
}
