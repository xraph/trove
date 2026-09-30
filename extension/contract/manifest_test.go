package contract

import (
	"bytes"
	"reflect"
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func loadManifest(t *testing.T) *dashcontract.ContractManifest {
	t.Helper()
	m, err := loader.Load(bytes.NewReader(manifestYAML), "trove/contract/manifest.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return m
}

func TestManifest_Loads(t *testing.T) {
	m := loadManifest(t)
	if m.Contributor.Name != ContributorName {
		t.Errorf("contributor = %q, want %q", m.Contributor.Name, ContributorName)
	}
	if got := len(m.Intents); got != 20 {
		t.Errorf("intents = %d, want 20", got)
	}
}

func TestManifest_CommandInvalidates(t *testing.T) {
	want := map[string][]string{
		"buckets.create":         {"buckets.list"},
		"buckets.delete":         {"buckets.list", "objects.list"},
		"objects.delete":         {"objects.list", "objects.head", "cas.list"},
		"objects.copy":           {"objects.list", "objects.head"},
		"objects.beginUpload":    nil,
		"objects.completeUpload": {"objects.list", "objects.head"},
		"objects.presign":        nil,
		"cas.pin":                {"cas.list"},
		"cas.unpin":              {"cas.list"},
		"cas.gc":                 {"cas.list", "cas.status"},
	}
	seen := 0
	for _, in := range loadManifest(t).Intents {
		if in.Kind != dashcontract.IntentKindCommand {
			if len(in.Invalidates) != 0 {
				t.Errorf("query %s declares invalidates %v", in.Name, in.Invalidates)
			}
			continue
		}
		w, ok := want[in.Name]
		if !ok {
			t.Errorf("unexpected command %s", in.Name)
			continue
		}
		seen++
		if len(w) == 0 && len(in.Invalidates) == 0 {
			continue
		}
		if !reflect.DeepEqual(in.Invalidates, w) {
			t.Errorf("%s invalidates = %v, want %v", in.Name, in.Invalidates, w)
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d commands", seen, len(want))
	}
}

func TestRegister_RequiresStoresAndContent(t *testing.T) {
	reg := dashcontract.NewRegistry()
	wreg := dashcontract.NewWardenRegistry()
	if err := Register(dispatcher.New(nil), reg, wreg, Deps{Content: testContent(t)}); err == nil {
		t.Error("Register accepted nil Stores")
	}
	if err := Register(dispatcher.New(nil), reg, wreg, Deps{Stores: newStores(openMem(t))}); err == nil {
		t.Error("Register accepted nil Content")
	}
}

func TestRegister_Succeeds(t *testing.T) {
	err := Register(dispatcher.New(nil), dashcontract.NewRegistry(), dashcontract.NewWardenRegistry(),
		testDeps(t, newStores(openMem(t))))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}
