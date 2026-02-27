package extension

import (
	"github.com/xraph/trove"
	"github.com/xraph/trove/extension/store"
)

// ExtOption configures the extension.
type ExtOption func(*Extension)

// WithStore provides a pre-built store (skips Grove DB resolution).
func WithStore(s *store.Store) ExtOption {
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
