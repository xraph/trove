package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	dashboard "github.com/xraph/forge/extensions/dashboard"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/grove"
	"github.com/xraph/vessel"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
	trovedash "github.com/xraph/trove/extension/dashboard"
	"github.com/xraph/trove/extension/store"
	mongostore "github.com/xraph/trove/extension/store/mongo"
	pgstore "github.com/xraph/trove/extension/store/postgres"
	sqlitestore "github.com/xraph/trove/extension/store/sqlite"
	"github.com/xraph/trove/middleware/compress"
)

const (
	// ExtensionName is the extension identifier.
	ExtensionName = "trove"

	// ExtensionVersion is the current extension version.
	ExtensionVersion = "0.1.0"

	// ExtensionDescription describes the extension.
	ExtensionDescription = "Object storage engine with middleware, CAS, VFS, and streaming"
)

// Compile-time interface checks.
var (
	_ forge.Extension          = (*Extension)(nil)
	_ dashboard.DashboardAware = (*Extension)(nil)
)

// Extension implements the Forge extension lifecycle for Trove.
type Extension struct {
	*forge.BaseExtension

	config    Config
	t         *trove.Trove
	store     store.Store
	troveOpts []trove.Option
	useGrove  bool
}

// New creates a new Trove extension.
func New(opts ...ExtOption) *Extension {
	ext := &Extension{
		BaseExtension: forge.NewBaseExtension(ExtensionName, ExtensionVersion, ExtensionDescription),
	}
	for _, opt := range opts {
		opt(ext)
	}
	return ext
}

// Register is called when the extension is registered with the Forge app.
func (e *Extension) Register(fapp forge.App) error {
	if err := e.BaseExtension.Register(fapp); err != nil {
		return err
	}

	if err := e.loadConfiguration(); err != nil {
		return err
	}

	if err := e.Init(fapp); err != nil {
		return err
	}

	return vessel.Provide(fapp.Container(), func() (*trove.Trove, error) {
		return e.t, nil
	})
}

// Init initializes the Trove extension: resolves DB, runs migrations,
// creates the Trove instance, and registers routes.
// In a Forge environment this is called during Register.
// For standalone use, call it manually after loadConfiguration.
func (e *Extension) Init(fapp forge.App) error {
	// Resolve Grove DB and build store.
	if e.store == nil && e.useGrove {
		groveDB, err := e.resolveGroveDB(fapp)
		if err != nil {
			return fmt.Errorf("trove: resolve grove db: %w", err)
		}
		s, err := e.buildStoreFromGroveDB(groveDB)
		if err != nil {
			return err
		}
		e.store = s
	}
	if e.store == nil {
		if db, err := vessel.Inject[*grove.DB](fapp.Container()); err == nil {
			s, err := e.buildStoreFromGroveDB(db)
			if err != nil {
				return err
			}
			e.store = s
			e.Logger().Info("trove: auto-discovered grove.DB from container",
				forge.F("driver", db.Driver().Name()),
			)
		}
	}

	// Run migrations.
	if e.store != nil && !e.config.DisableMigrate {
		if err := e.store.Migrate(context.Background()); err != nil {
			return fmt.Errorf("trove: migrations: %w", err)
		}
	}

	// Build Trove instance.
	if err := e.buildTrove(); err != nil {
		return fmt.Errorf("trove: build instance: %w", err)
	}

	// Register HTTP routes.
	if !e.config.DisableRoutes {
		e.registerRoutes(fapp)
	}

	return nil
}

// Start starts the Trove extension.
func (e *Extension) Start(ctx context.Context) error {
	return nil
}

// Stop gracefully shuts down the Trove extension.
func (e *Extension) Stop(ctx context.Context) error {
	if e.t != nil {
		return e.t.Close(ctx)
	}
	return nil
}

// Health checks the health of the Trove extension.
func (e *Extension) Health(ctx context.Context) error {
	if e.store != nil {
		return e.store.Ping(ctx)
	}
	return nil
}

// Trove returns the underlying Trove instance.
func (e *Extension) Trove() *trove.Trove {
	return e.t
}

// Store returns the underlying store.
func (e *Extension) Store() store.Store {
	return e.store
}

// DashboardContributor implements dashboard.DashboardAware. It returns a
// LocalContributor that renders trove pages, widgets, and settings in the
// Forge dashboard using templ + ForgeUI.
func (e *Extension) DashboardContributor() contributor.LocalContributor {
	// Check if driver supports presigned URLs.
	_, presignSupported := e.t.Driver().(driver.PresignDriver)

	return trovedash.New(
		trovedash.NewManifest(),
		e.store,
		e.t,
		trovedash.ContributorConfig{
			StorageDriver:    e.config.StorageDriver,
			BasePath:         e.config.BasePath,
			DefaultBucket:    e.config.DefaultBucket,
			CASEnabled:       e.config.EnableCAS,
			Encryption:       e.config.EnableEncryption,
			Compression:      e.config.EnableCompression,
			PresignSupported: presignSupported,
		},
	)
}

// --- Internal helpers ---

// loadConfiguration loads config from YAML files or programmatic sources.
// YAML takes precedence for value fields; programmatic bool flags override when true.
func (e *Extension) loadConfiguration() error {
	programmaticConfig := e.config

	fileConfig, configLoaded := e.tryLoadFromConfigFile()

	if !configLoaded {
		if programmaticConfig.RequireConfig {
			return errors.New("trove: configuration is required but not found in config files; " +
				"ensure 'extensions.trove' or 'trove' key exists in your config")
		}

		e.config = e.mergeWithDefaults(programmaticConfig)
	} else {
		e.config = e.mergeConfigurations(fileConfig, programmaticConfig)
	}

	if e.config.GroveDatabase != "" {
		e.useGrove = true
	}

	return nil
}

// tryLoadFromConfigFile attempts to load config from YAML files.
func (e *Extension) tryLoadFromConfigFile() (Config, bool) {
	cm := e.App().Config()
	if cm == nil {
		return Config{}, false
	}

	var cfg Config

	if cm.IsSet("extensions.trove") {
		if err := cm.Bind("extensions.trove", &cfg); err == nil {
			return cfg, true
		}
	}

	if cm.IsSet("trove") {
		if err := cm.Bind("trove", &cfg); err == nil {
			return cfg, true
		}
	}

	return Config{}, false
}

// mergeWithDefaults fills zero-valued fields with defaults.
func (e *Extension) mergeWithDefaults(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.BasePath == "" {
		cfg.BasePath = defaults.BasePath
	}
	if cfg.DefaultBucket == "" {
		cfg.DefaultBucket = defaults.DefaultBucket
	}
	return cfg
}

// mergeConfigurations merges YAML config with programmatic options.
// YAML config takes precedence for most fields; programmatic bool flags
// override when true (explicit opt-in via code).
func (e *Extension) mergeConfigurations(yamlConfig, programmaticConfig Config) Config {
	// Programmatic bool flags override when true (explicit opt-in).
	if programmaticConfig.DisableRoutes {
		yamlConfig.DisableRoutes = true
	}
	if programmaticConfig.DisableMigrate {
		yamlConfig.DisableMigrate = true
	}
	if programmaticConfig.EnableCAS {
		yamlConfig.EnableCAS = true
	}
	if programmaticConfig.EnableEncryption {
		yamlConfig.EnableEncryption = true
	}
	if programmaticConfig.EnableCompression {
		yamlConfig.EnableCompression = true
	}

	// String fields: YAML takes precedence, programmatic fills gaps.
	if yamlConfig.BasePath == "" && programmaticConfig.BasePath != "" {
		yamlConfig.BasePath = programmaticConfig.BasePath
	}
	if yamlConfig.StorageDriver == "" && programmaticConfig.StorageDriver != "" {
		yamlConfig.StorageDriver = programmaticConfig.StorageDriver
	}
	if yamlConfig.GroveDatabase == "" && programmaticConfig.GroveDatabase != "" {
		yamlConfig.GroveDatabase = programmaticConfig.GroveDatabase
	}
	if yamlConfig.DefaultBucket == "" && programmaticConfig.DefaultBucket != "" {
		yamlConfig.DefaultBucket = programmaticConfig.DefaultBucket
	}

	return e.mergeWithDefaults(yamlConfig)
}

func (e *Extension) resolveGroveDB(fapp forge.App) (*grove.DB, error) {
	if e.config.GroveDatabase != "" {
		db, err := vessel.InjectNamed[*grove.DB](fapp.Container(), e.config.GroveDatabase)
		if err != nil {
			return nil, fmt.Errorf("named grove DB %q: %w", e.config.GroveDatabase, err)
		}
		return db, nil
	}

	db, err := vessel.Inject[*grove.DB](fapp.Container())
	if err != nil {
		return nil, fmt.Errorf("default grove DB: %w", err)
	}
	return db, nil
}

// buildStoreFromGroveDB creates the appropriate store backend based on the
// Grove driver type (pg, sqlite, mongo).
func (e *Extension) buildStoreFromGroveDB(db *grove.DB) (store.Store, error) {
	driverName := db.Driver().Name()
	switch driverName {
	case "pg":
		return pgstore.New(db), nil
	case "sqlite":
		return sqlitestore.New(db), nil
	case "mongo":
		return mongostore.New(db), nil
	default:
		return nil, fmt.Errorf("trove: unsupported grove driver %q", driverName)
	}
}

func (e *Extension) buildTrove() error {
	// Build options from config.
	opts := e.config.ToTroveOptions()
	opts = append(opts, e.troveOpts...)

	// Add middleware from config flags.
	if e.config.EnableCompression {
		opts = append(opts, trove.WithMiddleware(compress.New()))
	}

	if e.config.EnableCAS {
		opts = append(opts, trove.WithCAS(cas.AlgSHA256))
	}

	// Build storage driver.
	drv := memdriver.New()
	if err := drv.Open(context.Background(), ""); err != nil {
		return fmt.Errorf("open memory driver: %w", err)
	}

	t, err := trove.Open(drv, opts...)
	if err != nil {
		return err
	}

	e.t = t
	return nil
}

func (e *Extension) registerRoutes(fapp forge.App) {
	// Route registration will be handled by the handler package.
	// This is a placeholder for the ForgeAPI integration.
}
