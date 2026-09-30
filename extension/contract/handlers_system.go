package contract

import (
	"context"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
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
	DefaultBucket  string `json:"defaultBucket"`
	ChunkSize      int64  `json:"chunkSize"`
	PoolSize       int    `json:"poolSize"`
	MaxUploadBytes int64  `json:"maxUploadBytes"`
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
				DefaultBucket:  cfg.DefaultBucket,
				ChunkSize:      cfg.ChunkSize,
				PoolSize:       cfg.PoolSize,
				MaxUploadBytes: deps.Content.MaxUploadBytes,
			},
			Flags:             protectionFlags(t, st.Configured),
			EtagIsContentHash: etagIsContentHash(name),
			ContentSecret:     secret,
		}, nil
	}
}

// protectionFlags reports each protection configured against applied.
func protectionFlags(t *trove.Trove, configured Flags) []flagStatus {
	registered := map[string]bool{}
	for _, r := range t.Resolver().Registrations() {
		registered[r.Middleware.Name()] = true
	}

	encryption := flagStatus{Name: "encryption", Configured: configured.Encryption, Applied: registered["encrypt"]}
	switch {
	case encryption.Configured && !encryption.Applied:
		encryption.Note = optString("enable_encryption is set, but the extension never registers the encrypt middleware. Nothing is encrypted.")
	case !encryption.Configured && encryption.Applied:
		encryption.Note = optString("Registered in code rather than by a config switch.")
	}

	compression := flagStatus{Name: "compression", Configured: configured.Compression, Applied: registered["compress"]}
	switch {
	case compression.Configured && !compression.Applied:
		compression.Note = optString("enable_compression is set, but no compress middleware is registered.")
	case !compression.Configured && compression.Applied:
		compression.Note = optString("Registered in code rather than by a config switch.")
	}

	// Scanning has no config switch: it is on only when code registers it.
	scanning := flagStatus{Name: "scanning", Configured: registered["scan"], Applied: registered["scan"]}
	if scanning.Applied {
		scanning.Note = optString("Registered in code. A scan with no provider, an excluded extension or an object over its size limit passes through unscanned, and nothing records which objects were scanned.")
	} else {
		scanning.Note = optString("No scan middleware is registered, so uploads are not scanned.")
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
