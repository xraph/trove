package contract

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/drivers/memdriver"
)

func casTrove(t *testing.T, open opener) *trove.Trove {
	t.Helper()
	tv := open(t, trove.WithCAS(cas.AlgSHA256))
	mustBucket(t, tv, "cas")
	return tv
}

func TestCASStatus(t *testing.T) {
	off, err := casStatusHandler(testDeps(t, newStores(openMem(t))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil || off.Enabled || off.Bucket != nil {
		t.Fatalf("disabled = %+v, %v", off, err)
	}
	on, err := casStatusHandler(testDeps(t, newStores(casTrove(t, openMem))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil || !on.Enabled || on.Bucket == nil || *on.Bucket != "cas" || on.Algorithm == nil || !on.ResetsOnRestart || on.ReleaseSupported {
		t.Fatalf("enabled = %+v, %v", on, err)
	}
}

func TestCASList_JoinsBucketAndIndex(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := casTrove(t, open)
		ctx := context.Background()
		hash, _, err := tv.CAS().Store(ctx, strings.NewReader("indexed"))
		if err != nil {
			t.Fatalf("store: %v", err)
		}
		if _, _, err = tv.CAS().Store(ctx, strings.NewReader("indexed")); err != nil {
			t.Fatalf("store again: %v", err)
		}
		// A blob in the bucket the index never saw, which is what every
		// blob looks like after a restart.
		put(t, tv, "cas", "sha256:orphan", "lost")

		out, err := casListHandler(testDeps(t, newStores(tv)))(ctx, casListInput{}, principalFor("u"))
		if err != nil {
			t.Fatalf("cas.list: %v", err)
		}
		byHash := map[string]casEntryRow{}
		for _, e := range out.Entries {
			byHash[e.Hash] = e
		}
		if e := byHash[hash]; !e.Indexed || e.RefCount == nil || *e.RefCount != 2 || e.Pinned == nil || *e.Pinned {
			t.Errorf("indexed entry = %+v", e)
		}
		if e := byHash["sha256:orphan"]; e.Indexed || e.RefCount != nil || e.Pinned != nil {
			t.Errorf("orphan = %+v, want not indexed with null refs and pin", e)
		}
	})
}

func TestCASList_MissingBucketIsEmpty(t *testing.T) {
	tv := openMem(t, trove.WithCAS(cas.AlgSHA256))
	out, err := casListHandler(testDeps(t, newStores(tv)))(context.Background(), casListInput{}, principalFor("u"))
	if err != nil || out.Entries == nil || len(out.Entries) != 0 {
		t.Fatalf("no cas bucket = %+v, %v", out, err)
	}
}

func TestCAS_DisabledIsUnavailable(t *testing.T) {
	deps := testDeps(t, newStores(openMem(t)))
	ctx := context.Background()
	if _, err := casListHandler(deps)(ctx, casListInput{}, principalFor("u")); codeOf(err) != "UNAVAILABLE" {
		t.Errorf("list = %v", err)
	}
	if _, err := casPinHandler(deps)(ctx, casHashInput{Hash: "x"}, principalFor("u")); codeOf(err) != "UNAVAILABLE" {
		t.Errorf("pin = %v", err)
	}
	if _, err := casGCHandler(deps)(ctx, storeInput{}, principalFor("u")); codeOf(err) != "UNAVAILABLE" {
		t.Errorf("gc = %v", err)
	}
}

func TestCASPinUnpin(t *testing.T) {
	tv := casTrove(t, openMem)
	ctx := context.Background()
	hash, _, err := tv.CAS().Store(ctx, strings.NewReader("pin me"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	deps := testDeps(t, newStores(tv))
	out, err := casPinHandler(deps)(ctx, casHashInput{Hash: hash}, principalFor("u"))
	if err != nil || out.Pinned == nil || !*out.Pinned {
		t.Fatalf("pin = %+v, %v", out, err)
	}
	out, err = casUnpinHandler(deps)(ctx, casHashInput{Hash: hash}, principalFor("u"))
	if err != nil || out.Pinned == nil || *out.Pinned {
		t.Fatalf("unpin = %+v, %v", out, err)
	}
	if _, err := casPinHandler(deps)(ctx, casHashInput{Hash: "sha256:none"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("pin of an unknown hash = %v", err)
	}
	if _, err := casPinHandler(deps)(ctx, casHashInput{}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Fatalf("pin without a hash = %v", err)
	}
}

// TestCASGC_ReportsWhatItDid seeds one indexed blob and one orphan the
// index never saw. Nothing lowers a refcount, so the GC candidates are the
// indexed entries at refcount 0 and unpinned: none. GC must scan exactly
// those, delete nothing, and leave the orphan where it was.
func TestCASGC_ReportsWhatItDid(t *testing.T) {
	tv := casTrove(t, openMem)
	ctx := context.Background()
	hash, _, err := tv.CAS().Store(ctx, strings.NewReader("kept"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	put(t, tv, "cas", "sha256:orphan", "lost")
	deps := testDeps(t, newStores(tv))

	before, err := casListHandler(deps)(ctx, casListInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("cas.list before gc: %v", err)
	}
	candidates := 0
	for _, e := range before.Entries {
		if e.Indexed && *e.RefCount == 0 && !*e.Pinned {
			candidates++
		}
	}

	out, err := casGCHandler(deps)(ctx, storeInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if out.Deleted != 0 || out.FreedBytes != 0 || out.Errors != 0 {
		t.Fatalf("gc = %+v; nothing can reach refcount 0, so nothing is deleted", out)
	}
	if out.Scanned != candidates {
		t.Fatalf("gc scanned %d, want the %d indexed entries at refcount 0 and unpinned", out.Scanned, candidates)
	}

	after, err := casListHandler(deps)(ctx, casListInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("cas.list after gc: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range after.Entries {
		seen[e.Hash] = true
	}
	if !seen["sha256:orphan"] || !seen[hash] {
		t.Fatalf("after gc cas.list = %+v, want the orphan and %s still there", after.Entries, hash)
	}
}

// TestCASList_ReadsTheDriverCASWritesTo routes the CAS bucket to a second
// backend. CAS writes through the default driver whatever the routes say,
// so cas.list must read the default too, or every blob disappears from it.
func TestCASList_ReadsTheDriverCASWritesTo(t *testing.T) {
	def := memdriver.New()
	if err := def.Open(context.Background(), ""); err != nil {
		t.Fatalf("open default: %v", err)
	}
	other := memdriver.New()
	if err := other.Open(context.Background(), ""); err != nil {
		t.Fatalf("open other: %v", err)
	}
	if err := other.CreateBucket(context.Background(), "cas"); err != nil {
		t.Fatalf("create cas on the other backend: %v", err)
	}
	tv := openTrove(t, def, trove.WithCAS(cas.AlgSHA256), trove.WithBackend("other", other),
		trove.WithRouteFunc(func(bucket, _ string) string {
			if bucket == "cas" {
				return "other"
			}
			return ""
		}))
	mustBucket(t, tv, "cas")
	hash, _, err := tv.CAS().Store(context.Background(), strings.NewReader("routed away"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	out, err := casListHandler(testDeps(t, newStores(tv)))(context.Background(), casListInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("cas.list: %v", err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Hash != hash || !out.Entries[0].Indexed {
		t.Fatalf("cas.list = %+v, want the one blob CAS stored (%s)", out.Entries, hash)
	}
}
