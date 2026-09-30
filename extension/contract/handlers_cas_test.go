package contract

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
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

func TestCASGC_ReportsWhatItDid(t *testing.T) {
	tv := casTrove(t, openMem)
	if _, _, err := tv.CAS().Store(context.Background(), strings.NewReader("kept")); err != nil {
		t.Fatalf("store: %v", err)
	}
	out, err := casGCHandler(testDeps(t, newStores(tv)))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil || out.Deleted != 0 {
		t.Fatalf("gc = %+v, %v; nothing can reach refcount 0, so nothing is deleted", out, err)
	}
}
