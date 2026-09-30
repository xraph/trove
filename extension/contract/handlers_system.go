package contract

import (
	"context"
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
)

// storeInput is the input of every intent that takes only a store.
type storeInput struct {
	Store string `json:"store"`
}

type health struct {
	OK    bool    `json:"ok"`
	Error *string `json:"error"`
}

type capabilities struct {
	Multipart    bool `json:"multipart"`
	Presign      bool `json:"presign"`
	Range        bool `json:"range"`
	ServerCopy   bool `json:"serverCopy"`
	Versioning   bool `json:"versioning"`
	Notification bool `json:"notification"`
	Lifecycle    bool `json:"lifecycle"`
	Folders      bool `json:"folders"`
}

type statusConfig struct {
	DefaultBucket  *string `json:"defaultBucket"`
	ChunkSize      int64   `json:"chunkSize"`
	PoolSize       int     `json:"poolSize"`
	MaxUploadBytes int64   `json:"maxUploadBytes"`
}

// flagStatus compares what an operator configured with what the resolver
// actually has registered. A config flag describes the system now; it says
// nothing about objects written before it changed.
type flagStatus struct {
	Name       string  `json:"name"`
	Configured bool    `json:"configured"`
	Applied    bool    `json:"applied"`
	Note       *string `json:"note"`
}

type systemStatus struct {
	Store             string       `json:"store"`
	Driver            string       `json:"driver"`
	Health            health       `json:"health"`
	Capabilities      capabilities `json:"capabilities"`
	Config            statusConfig `json:"config"`
	Flags             []flagStatus `json:"flags"`
	EtagIsContentHash bool         `json:"etagIsContentHash"`
	ContentSecret     string       `json:"contentSecret"`
	// Backends names the backends registered beside the default, sorted.
	// The default has no name and is not listed, so [] means every key
	// goes to the default.
	Backends []string `json:"backends"`
	// RoutingNote is set when Backends is not empty and says what the
	// other answers cover. It is null otherwise.
	RoutingNote *string `json:"routingNote"`
}

// routingNote is what system.status says about a store that routes keys to
// more than one backend.
const routingNote = "This store routes some keys to other backends. Listings, bucket operations and health describe the default backend only."

// isRouted reports whether t has a backend besides its default, so a
// listing or a health check of the default may not describe every key.
func isRouted(t *trove.Trove) bool {
	return len(t.Backends()) > 0
}

func systemStatusHandler(deps Deps) func(context.Context, storeInput, contract.Principal) (systemStatus, error) {
	return func(ctx context.Context, in storeInput, _ contract.Principal) (systemStatus, error) {
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return systemStatus{}, err
		}
		t := st.Trove
		drv := t.Driver()
		name := drv.Name()

		h := health{OK: true}
		if err := t.Health(ctx); err != nil {
			h = health{OK: false, Error: optString("The driver did not answer a ping.")}
			if deps.Logger != nil {
				deps.Logger.Warn("trove/contract: store failed its health check",
					forge.F("store", st.Name), forge.F("error", err))
			}
		}

		_, multipart := drv.(driver.MultipartDriver)
		_, presign := drv.(driver.PresignDriver)
		_, rng := drv.(driver.RangeDriver)
		_, serverCopy := drv.(driver.ServerCopyDriver)
		_, versioning := drv.(driver.VersioningDriver)
		_, notification := drv.(driver.NotificationDriver)
		_, lifecycle := drv.(driver.LifecycleDriver)

		cfg := t.Config()
		secret := "configured"
		if deps.Content.PerProcessSecret {
			secret = "per-process"
		}
		var note *string
		if isRouted(t) {
			note = optString(routingNote)
		}

		return systemStatus{
			Store:  st.Name,
			Driver: name,
			Health: h,
			Capabilities: capabilities{
				Multipart: multipart, Presign: presign, Range: rng, ServerCopy: serverCopy,
				Versioning: versioning, Notification: notification, Lifecycle: lifecycle,
				Folders: driverFolds(name),
			},
			Config: statusConfig{
				DefaultBucket:  optString(cfg.DefaultBucket),
				ChunkSize:      cfg.ChunkSize,
				PoolSize:       cfg.PoolSize,
				MaxUploadBytes: deps.Content.MaxUploadBytes,
			},
			Flags:             protectionFlags(t, st.Configured),
			EtagIsContentHash: etagIsContentHash(name),
			ContentSecret:     secret,
			Backends:          t.Backends(),
			RoutingNote:       note,
		}, nil
	}
}

// writeCoverage describes where one middleware runs on the write path.
type writeCoverage struct {
	registered bool     // any registration, in either direction
	applied    bool     // at least one registration runs on the write path
	global     bool     // one of those has a global scope
	scopes     []string // distinct scopes of the write-path registrations
}

func coverageOf(t *trove.Trove, name string) writeCoverage {
	var c writeCoverage
	seen := map[string]bool{}
	for _, r := range t.Resolver().Registrations() {
		if r.Middleware.Name() != name {
			continue
		}
		c.registered = true
		if !runs(r, middleware.DirectionWrite) {
			continue
		}
		c.applied = true
		sc := scopeOf(r)
		if _, ok := sc.(middleware.ScopeGlobal); ok {
			c.global = true
		}
		if str := sc.String(); !seen[str] {
			seen[str] = true
			c.scopes = append(c.scopes, str)
		}
	}
	return c
}

// protectionFlag builds one flag from what was configured and where the
// middleware really runs. Applied means it runs when an object is written;
// a registration that only runs on reads, or only inside a narrow scope,
// never reads as blanket protection. verb is the past participle
// ("encrypted"); missing is the note for a configured flag with nothing
// registered.
func protectionFlag(name string, configured bool, c writeCoverage, verb, missing string) flagStatus {
	f := flagStatus{Name: name, Configured: configured, Applied: c.applied}
	var notes []string
	switch {
	case c.registered && !c.applied:
		notes = append(notes, "Registered for reads only, so nothing is "+verb+" on write.")
	case !c.registered && configured:
		notes = append(notes, missing)
	case c.applied && !configured:
		notes = append(notes, "Registered in code rather than by a config switch.")
	}
	if c.applied && !c.global {
		notes = append(notes, "Applies only where its scope matches: "+strings.Join(c.scopes, ", ")+". Objects outside that scope are not "+verb+".")
	}
	if len(notes) > 0 {
		f.Note = optString(strings.Join(notes, " "))
	}
	return f
}

// protectionFlags reports each protection configured against applied.
func protectionFlags(t *trove.Trove, configured Flags) []flagStatus {
	encryption := protectionFlag("encryption", configured.Encryption, coverageOf(t, "encrypt"), "encrypted",
		"enable_encryption is set, but the extension never registers the encrypt middleware. Nothing is encrypted.")
	compression := protectionFlag("compression", configured.Compression, coverageOf(t, "compress"), "compressed",
		"enable_compression is set, but no compress middleware is registered.")

	// Scanning has no config switch: it is on only when code registers it.
	sc := coverageOf(t, "scan")
	scanning := protectionFlag("scanning", sc.registered, sc, "scanned", "")
	caveat := "Registered in code. A scan with no provider, an excluded extension or an object over its size limit passes through unscanned, and nothing records which objects were scanned."
	switch {
	case !sc.registered:
		scanning.Note = optString("No scan middleware is registered, so uploads are not scanned.")
	case scanning.Note != nil:
		scanning.Note = optString(caveat + " " + *scanning.Note)
	default:
		scanning.Note = optString(caveat)
	}

	casFlag := flagStatus{Name: "cas", Configured: configured.CAS, Applied: t.CAS() != nil}
	if casFlag.Configured && !casFlag.Applied {
		casFlag.Note = optString("enable_cas is set, but this store has no CAS engine.")
	}

	return []flagStatus{encryption, compression, scanning, casFlag}
}

type storeRow struct {
	Name      string `json:"name"`
	Driver    string `json:"driver"`
	IsDefault bool   `json:"isDefault"`
}

type storesListOutput struct {
	Mode   string     `json:"mode"`
	Stores []storeRow `json:"stores"`
}

func storesListHandler(deps Deps) func(context.Context, struct{}, contract.Principal) (storesListOutput, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (storesListOutput, error) {
		out := storesListOutput{Mode: "single", Stores: []storeRow{}}
		if deps.Stores.Multi() {
			out.Mode = "multi"
		}
		for _, st := range deps.Stores.All() {
			out.Stores = append(out.Stores, storeRow{
				Name:      st.Name,
				Driver:    st.Trove.Driver().Name(),
				IsDefault: st.Name == deps.Stores.DefaultName(),
			})
		}
		return out, nil
	}
}
