package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	dashboard "github.com/xraph/forge/extensions/dashboard"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/vessel"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
	_ "github.com/xraph/trove/drivers/localdriver" // register "file" and "local" schemes
	"github.com/xraph/trove/drivers/memdriver"
	trovedash "github.com/xraph/trove/extension/dashboard"
	"github.com/xraph/trove/extension/store"
	mongostore "github.com/xraph/trove/extension/store/mongo"
	pgstore "github.com/xraph/trove/extension/store/postgres"
	sqlitestore "github.com/xraph/trove/extension/store/sqlite"
	"github.com/xraph/trove/middleware/compress"

	"github.com/xraph/grove"
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

	// Multi-store support.
	fileStores   []fileStoreEntry
	defaultStore string
	manager      *TroveManager
	storeOpts    map[string][]trove.Option
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

	// Route to single-store or multi-store initialization.
	if e.isMultiStore() {
		return e.registerMultiStore(fapp)
	}
	return e.registerSingleStore(fapp)
}

// isMultiStore returns true if multiple named stores are configured.
func (e *Extension) isMultiStore() bool {
	return len(e.fileStores) > 0 || len(e.config.Stores) > 0
}

// registerSingleStore handles the original single-store path.
func (e *Extension) registerSingleStore(fapp forge.App) error {
	if err := e.Init(fapp); err != nil {
		return err
	}

	return vessel.Provide(fapp.Container(), func() (*trove.Trove, error) {
		return e.t, nil
	})
}

// registerMultiStore handles the multi-store path.
func (e *Extension) registerMultiStore(fapp forge.App) error {
	mgr := NewTroveManager()

	// Merge programmatic entries with config entries.
	entries := e.buildFileStoreEntries()

	// Initialize each store.
	for _, entry := range entries {
		// 1. Resolve storage driver.
		drv, err := e.resolveStorageDriver(entry)
		if err != nil {
			return fmt.Errorf("trove: store %q: resolve driver: %w", entry.name, err)
		}

		// 2. Resolve Grove DB and build metadata store.
		var metaStore store.Store
		if entry.groveDatabase != "" {
			groveDB, groveErr := vessel.InjectNamed[*grove.DB](fapp.Container(), entry.groveDatabase)
			if groveErr != nil {
				return fmt.Errorf("trove: store %q: resolve grove db %q: %w",
					entry.name, entry.groveDatabase, groveErr)
			}
			metaStore, err = e.buildStoreFromGroveDB(groveDB)
			if err != nil {
				return fmt.Errorf("trove: store %q: build metadata store: %w", entry.name, err)
			}
		} else {
			// Try auto-discovering grove DB from container.
			if db, autoErr := vessel.Inject[*grove.DB](fapp.Container()); autoErr == nil {
				metaStore, _ = e.buildStoreFromGroveDB(db) //nolint:errcheck // best-effort auto-discovery
			}
		}

		// 3. Run migrations for this store's metadata.
		if metaStore != nil && !e.config.DisableMigrate {
			if migrateErr := metaStore.Migrate(context.Background()); migrateErr != nil {
				return fmt.Errorf("trove: store %q: migrations: %w", entry.name, migrateErr)
			}
		}

		// 4. Build trove.Trove instance with per-store options.
		opts := e.buildTroveOptsForEntry(entry)
		if storeSpecificOpts, ok := e.storeOpts[entry.name]; ok {
			opts = append(opts, storeSpecificOpts...)
		}

		t, err := trove.Open(drv, opts...)
		if err != nil {
			return fmt.Errorf("trove: store %q: open: %w", entry.name, err)
		}

		mgr.Add(entry.name, t, metaStore)

		e.Logger().Info("trove: store opened",
			forge.F("name", entry.name),
			forge.F("driver", drv.Name()),
		)
	}

	// Set default store.
	defaultName := e.resolveDefaultStoreName(entries)
	if defaultName != "" {
		if err := mgr.SetDefault(defaultName); err != nil {
			return fmt.Errorf("trove: set default store: %w", err)
		}
	}

	e.manager = mgr

	// Set e.t and e.store to the default for backward-compatible accessors.
	defaultTrove, err := mgr.Default()
	if err != nil {
		return fmt.Errorf("trove: get default store: %w", err)
	}
	e.t = defaultTrove
	if defaultMetaStore, metaErr := mgr.DefaultStore(); metaErr == nil {
		e.store = defaultMetaStore
	}

	// Register HTTP routes.
	if !e.config.DisableRoutes {
		e.registerRoutes(fapp)
	}

	// Register in DI.
	if err := e.registerMultiStoreInDI(fapp); err != nil {
		return err
	}

	e.Logger().Info("trove extension registered",
		forge.F("mode", "multi-store"),
		forge.F("stores", mgr.Len()),
		forge.F("default", defaultName),
	)
	return nil
}

// Init initializes the Trove extension: resolves DB, runs migrations,
// creates the Trove instance, and registers routes.
// In a Forge environment this is called during Register (single-store mode).
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
func (e *Extension) Start(_ context.Context) error {
	return nil
}

// Stop gracefully shuts down the Trove extension.
func (e *Extension) Stop(ctx context.Context) error {
	if e.manager != nil {
		return e.manager.Close(ctx)
	}
	if e.t != nil {
		return e.t.Close(ctx)
	}
	return nil
}

// Health checks the health of the Trove extension.
func (e *Extension) Health(ctx context.Context) error {
	if e.manager != nil {
		for name, t := range e.manager.All() {
			if err := t.Health(ctx); err != nil {
				return fmt.Errorf("trove: store %q: %w", name, err)
			}
		}
		return nil
	}
	if e.store != nil {
		return e.store.Ping(ctx)
	}
	return nil
}

// Trove returns the underlying Trove instance (default in multi-store mode).
func (e *Extension) Trove() *trove.Trove {
	return e.t
}

// Store returns the underlying metadata store (default in multi-store mode).
func (e *Extension) Store() store.Store {
	return e.store
}

// Manager returns the TroveManager for multi-store mode.
// Returns nil in single-store mode.
func (e *Extension) Manager() *TroveManager {
	return e.manager
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

	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid trove configuration: %w", err)
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
	if yamlConfig.Default == "" && programmaticConfig.Default != "" {
		yamlConfig.Default = programmaticConfig.Default
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
	var drv driver.Driver
	if e.config.StorageDriver != "" {
		resolved, err := e.resolveStorageDriver(fileStoreEntry{driverDSN: e.config.StorageDriver})
		if err != nil {
			return fmt.Errorf("resolve storage driver: %w", err)
		}
		drv = resolved
	} else {
		d := memdriver.New()
		if err := d.Open(context.Background(), ""); err != nil {
			return fmt.Errorf("open memory driver: %w", err)
		}
		drv = d
	}

	t, err := trove.Open(drv, opts...)
	if err != nil {
		return err
	}

	// Ensure the default bucket exists so callers don't hit "bucket not found".
	if bucket := e.config.DefaultBucket; bucket != "" {
		if createErr := t.CreateBucket(context.Background(), bucket); createErr != nil {
			// Ignore "already exists" errors — the bucket may have been created previously.
			e.Logger().Debug("trove: ensure default bucket",
				forge.F("bucket", bucket),
				forge.F("result", createErr.Error()),
			)
		}
	}

	e.t = t
	return nil
}

func (e *Extension) registerRoutes(_ forge.App) {
	// Route registration will be handled by the handler package.
	// This is a placeholder for the ForgeAPI integration.
}

// --- Multi-store helpers ---

// buildFileStoreEntries merges programmatic and config-based store entries.
// Programmatic entries take precedence over config entries with the same name.
func (e *Extension) buildFileStoreEntries() []fileStoreEntry {
	entries := make([]fileStoreEntry, len(e.fileStores))
	copy(entries, e.fileStores)

	// Track names from programmatic entries.
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[entry.name] = true
	}

	// Add config entries that aren't already provided programmatically.
	for _, cfg := range e.config.Stores {
		if seen[cfg.Name] {
			continue
		}
		entries = append(entries, fileStoreEntry{
			name:           cfg.Name,
			driverDSN:      cfg.StorageDriver,
			groveDatabase:  cfg.GroveDatabase,
			defaultBucket:  cfg.DefaultBucket,
			enableCAS:      cfg.EnableCAS,
			enableEncrypt:  cfg.EnableEncryption,
			enableCompress: cfg.EnableCompression,
		})
	}

	return entries
}

// resolveStorageDriver creates a driver.Driver from a file store entry.
func (e *Extension) resolveStorageDriver(entry fileStoreEntry) (driver.Driver, error) {
	if entry.storageDriver != nil {
		return entry.storageDriver, nil
	}
	if entry.driverDSN == "" {
		return nil, errors.New("storage_driver DSN is required")
	}

	// Parse the DSN to extract the scheme (driver name).
	cfg, err := driver.ParseDSN(entry.driverDSN)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	// Look up the driver factory from the global registry.
	factory, ok := driver.Lookup(cfg.Scheme)
	if !ok {
		return nil, fmt.Errorf("unknown storage driver %q (registered: %v)",
			cfg.Scheme, driver.Drivers())
	}

	// Create and open the driver.
	drv := factory()
	if err := drv.Open(context.Background(), entry.driverDSN); err != nil {
		return nil, fmt.Errorf("open driver: %w", err)
	}
	return drv, nil
}

// buildTroveOptsForEntry converts per-store flags to trove.Option slice.
func (e *Extension) buildTroveOptsForEntry(entry fileStoreEntry) []trove.Option {
	var opts []trove.Option

	bucket := entry.defaultBucket
	if bucket == "" {
		bucket = "default"
	}
	opts = append(opts, trove.WithDefaultBucket(bucket))

	if entry.enableCompress {
		opts = append(opts, trove.WithMiddleware(compress.New()))
	}
	if entry.enableCAS {
		opts = append(opts, trove.WithCAS(cas.AlgSHA256))
	}

	// Global troveOpts are also applied to each store.
	opts = append(opts, e.troveOpts...)

	return opts
}

// resolveDefaultStoreName determines the default store name.
func (e *Extension) resolveDefaultStoreName(entries []fileStoreEntry) string {
	if e.defaultStore != "" {
		return e.defaultStore
	}
	if e.config.Default != "" {
		return e.config.Default
	}
	if len(entries) > 0 {
		return entries[0].name
	}
	return ""
}

// registerMultiStoreInDI registers the manager and all stores in the DI container.
func (e *Extension) registerMultiStoreInDI(fapp forge.App) error {
	// Register the TroveManager itself.
	if err := vessel.Provide(fapp.Container(), func() *TroveManager {
		return e.manager
	}); err != nil {
		return fmt.Errorf("trove: register manager in container: %w", err)
	}

	// Register default *trove.Trove (unnamed — backward compatible).
	if err := vessel.Provide(fapp.Container(), func() (*trove.Trove, error) {
		return e.manager.Default()
	}); err != nil {
		return fmt.Errorf("trove: register default trove in container: %w", err)
	}

	// Register each named *trove.Trove.
	for name, t := range e.manager.All() {
		namedTrove := t // capture loop variable
		if err := vessel.ProvideNamed(fapp.Container(), name, func() *trove.Trove {
			return namedTrove
		}); err != nil {
			return fmt.Errorf("trove: register store %q in container: %w", name, err)
		}
	}

	return nil
}
