package contract

import (
	"sort"
	"testing"
)

// Every intent the manifest declares must have a handler. Register checks
// the other direction; this closes the loop, so a declared intent can never
// answer "no handler" in production.
func TestEveryDeclaredIntentIsBound(t *testing.T) {
	bound := map[string]bool{}
	for _, b := range bindings(testDeps(t, newStores(openMem(t)))) {
		if bound[b.intent] {
			t.Errorf("%s is bound twice", b.intent)
		}
		bound[b.intent] = true
	}
	var missing []string
	for _, in := range loadManifest(t).Intents {
		if !bound[in.Name] {
			missing = append(missing, in.Name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("declared but not bound: %v", missing)
	}
}
