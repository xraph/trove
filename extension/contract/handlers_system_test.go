package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/middleware/compress"
	"github.com/xraph/trove/middleware/encrypt"
	"github.com/xraph/trove/middleware/scan"
)

func flagByName(t *testing.T, s systemStatus, name string) flagStatus {
	t.Helper()
	for _, f := range s.Flags {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no flag %q in %+v", name, s.Flags)
	return flagStatus{}
}

func TestSystemStatus_EncryptionConfiguredButNotApplied(t *testing.T) {
	tv := openMem(t)
	deps := testDeps(t, NewSingleStore(tv, Flags{Encryption: true}))
	s, err := systemStatusHandler(deps)(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	f := flagByName(t, s, "encryption")
	if !f.Configured || f.Applied {
		t.Fatalf("encryption = %+v, want configured and not applied", f)
	}
	if f.Note == nil || !bytes.Contains([]byte(*f.Note), []byte("Nothing is encrypted")) {
		t.Fatalf("encryption note = %v, want it to say nothing is encrypted", f.Note)
	}
}

func TestSystemStatus_AppliedComesFromTheResolver(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	tv := openMem(t,
		trove.WithMiddleware(compress.New()),
		trove.WithMiddleware(encrypt.New(encrypt.WithKeyProvider(encrypt.NewStaticKeyProvider(key)))),
		trove.WithCAS(cas.AlgSHA256),
	)
	deps := testDeps(t, NewSingleStore(tv, Flags{Compression: true, CAS: true}))
	s, err := systemStatusHandler(deps)(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	if f := flagByName(t, s, "compression"); !f.Configured || !f.Applied {
		t.Errorf("compression = %+v", f)
	}
	if f := flagByName(t, s, "encryption"); f.Configured || !f.Applied {
		t.Errorf("encryption registered in code = %+v, want applied and not configured", f)
	}
	if f := flagByName(t, s, "cas"); !f.Configured || !f.Applied {
		t.Errorf("cas = %+v", f)
	}
	if f := flagByName(t, s, "scanning"); f.Applied {
		t.Errorf("scanning = %+v, want not applied", f)
	}
}

func TestSystemStatus_DriverFacts(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		content := testContent(t)
		content.PerProcessSecret = true
		deps := Deps{Stores: newStores(tv), Content: content}
		s, err := systemStatusHandler(deps)(context.Background(), storeInput{}, principalFor("u"))
		if err != nil {
			t.Fatalf("system.status: %v", err)
		}
		if s.Driver != tv.Driver().Name() || s.Store != SingleStoreName {
			t.Errorf("driver=%q store=%q", s.Driver, s.Store)
		}
		if !s.Health.OK || s.Health.Error != nil {
			t.Errorf("health = %+v", s.Health)
		}
		if s.Capabilities.Presign || s.Capabilities.Multipart || !s.Capabilities.Folders {
			t.Errorf("capabilities = %+v", s.Capabilities)
		}
		if s.EtagIsContentHash {
			t.Error("mem and local ETags are not content hashes")
		}
		if s.ContentSecret != "per-process" {
			t.Errorf("contentSecret = %q", s.ContentSecret)
		}
		if s.Config.MaxUploadBytes != content.MaxUploadBytes || s.Config.PoolSize != tv.Config().PoolSize {
			t.Errorf("config = %+v", s.Config)
		}
	})
}

func TestSystemStatus_UnknownStore(t *testing.T) {
	deps := testDeps(t, newStores(openMem(t)))
	if _, err := systemStatusHandler(deps)(context.Background(), storeInput{Store: "nope"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("system.status on an unknown store = %v", err)
	}
}

func TestStoresList_SingleAndMulti(t *testing.T) {
	single := testDeps(t, newStores(openMem(t)))
	out, err := storesListHandler(single)(context.Background(), struct{}{}, principalFor("u"))
	if err != nil || out.Mode != "single" || len(out.Stores) != 1 || !out.Stores[0].IsDefault || out.Stores[0].Driver != "mem" {
		t.Fatalf("single = %+v, %v", out, err)
	}

	multi, err := NewStores("b", []Store{{Name: "a", Trove: openMem(t)}, {Name: "b", Trove: openLocal(t)}})
	if err != nil {
		t.Fatalf("NewStores: %v", err)
	}
	out, err = storesListHandler(testDeps(t, multi))(context.Background(), struct{}{}, principalFor("u"))
	if err != nil || out.Mode != "multi" || len(out.Stores) != 2 {
		t.Fatalf("multi = %+v, %v", out, err)
	}
	if out.Stores[0].IsDefault || !out.Stores[1].IsDefault || out.Stores[1].Driver != "local" {
		t.Fatalf("multi stores = %+v", out.Stores)
	}
}

func encryptMW(t *testing.T) middleware.Middleware {
	t.Helper()
	return encrypt.New(encrypt.WithKeyProvider(encrypt.NewStaticKeyProvider(bytes.Repeat([]byte("k"), 32))))
}

func TestSystemStatus_ScopedEncryptionSaysWhereItApplies(t *testing.T) {
	tv := openMem(t, trove.WithScopedMiddleware(middleware.ForBuckets("reports"), encryptMW(t)))
	s, err := systemStatusHandler(testDeps(t, newStores(tv)))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	f := flagByName(t, s, "encryption")
	if !f.Applied || f.Note == nil {
		t.Fatalf("encryption = %+v, want applied with a note", f)
	}
	for _, want := range []string{"bucket(reports)", "Objects outside that scope are not encrypted"} {
		if !strings.Contains(*f.Note, want) {
			t.Errorf("note %q does not contain %q", *f.Note, want)
		}
	}
}

func TestSystemStatus_ReadOnlyEncryptionIsNotApplied(t *testing.T) {
	tv := openMem(t, trove.WithReadMiddleware(encryptMW(t)))
	s, err := systemStatusHandler(testDeps(t, NewSingleStore(tv, Flags{Encryption: true})))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	f := flagByName(t, s, "encryption")
	if f.Applied || f.Note == nil || !strings.Contains(*f.Note, "Registered for reads only, so nothing is encrypted on write.") {
		t.Fatalf("encryption = %+v (note %v), want not applied with the reads-only note", f, f.Note)
	}
}

func TestSystemStatus_GlobalEncryptionHasNoScopeNote(t *testing.T) {
	tv := openMem(t, trove.WithMiddleware(encryptMW(t)))
	s, err := systemStatusHandler(testDeps(t, NewSingleStore(tv, Flags{Encryption: true})))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	if f := flagByName(t, s, "encryption"); !f.Applied || f.Note != nil {
		t.Fatalf("encryption = %+v (note %v), want applied with no note", f, f.Note)
	}
}

func TestSystemStatus_ScopedScanningKeepsItsCaveat(t *testing.T) {
	tv := openMem(t, trove.WithScopedMiddleware(middleware.ForBuckets("uploads"), scan.New()))
	s, err := systemStatusHandler(testDeps(t, newStores(tv)))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	f := flagByName(t, s, "scanning")
	if !f.Applied || f.Note == nil || !strings.Contains(*f.Note, "bucket(uploads)") || !strings.Contains(*f.Note, "nothing records which objects were scanned") {
		t.Fatalf("scanning = %+v (note %v)", f, f.Note)
	}
}

func TestSystemStatus_DefaultBucketIsNullWhenUnset(t *testing.T) {
	s, err := systemStatusHandler(testDeps(t, newStores(openMem(t))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	if s.Config.DefaultBucket != nil {
		t.Fatalf("defaultBucket = %q, want null when none is configured", *s.Config.DefaultBucket)
	}
	raw, err := json.Marshal(s.Config)
	if err != nil || !strings.Contains(string(raw), `"defaultBucket":null`) {
		t.Fatalf("config = %s (%v), want defaultBucket null", raw, err)
	}

	named, err := systemStatusHandler(testDeps(t, newStores(openMem(t, trove.WithDefaultBucket("primary")))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil || named.Config.DefaultBucket == nil || *named.Config.DefaultBucket != "primary" {
		t.Fatalf("defaultBucket with one configured = %v, %v", named.Config.DefaultBucket, err)
	}
}

func TestSystemStatus_SaysWhenAStoreIsRouted(t *testing.T) {
	plain, err := systemStatusHandler(testDeps(t, newStores(openMem(t))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	if plain.Backends == nil || len(plain.Backends) != 0 || plain.RoutingNote != nil {
		t.Fatalf("unrouted = backends %v, note %v; want [] and null", plain.Backends, plain.RoutingNote)
	}

	archive := memdriver.New()
	if err = archive.Open(context.Background(), ""); err != nil {
		t.Fatalf("open archive: %v", err)
	}
	tv := openMem(t, trove.WithBackend("archive", archive), trove.WithRoute("*.log", "archive"))
	routed, err := systemStatusHandler(testDeps(t, newStores(tv)))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("system.status: %v", err)
	}
	if len(routed.Backends) != 1 || routed.Backends[0] != "archive" {
		t.Fatalf("backends = %v, want [archive]", routed.Backends)
	}
	want := "This store routes some keys to other backends. Listings, bucket operations and health describe the default backend only."
	if routed.RoutingNote == nil || *routed.RoutingNote != want {
		t.Fatalf("routingNote = %v, want %q", routed.RoutingNote, want)
	}
}
