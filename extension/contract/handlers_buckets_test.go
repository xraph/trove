package contract

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/trove/drivers/localdriver"
)

func TestBucketsCreateListDelete(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		deps := testDeps(t, newStores(open(t)))
		ctx := context.Background()
		p := principalFor("u")

		if _, err := bucketsCreateHandler(deps)(ctx, bucketInput{Name: "reports"}, p); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := bucketsCreateHandler(deps)(ctx, bucketInput{Name: "reports"}, p); codeOf(err) != "CONFLICT" {
			t.Fatalf("second create = %v, want CONFLICT", err)
		}
		list, err := bucketsListHandler(deps)(ctx, storeInput{}, p)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, b := range list.Buckets {
			if b.Name == "reports" {
				found = true
			}
		}
		if !found {
			t.Fatalf("list = %+v, missing reports", list.Buckets)
		}
		if _, err := bucketsDeleteHandler(deps)(ctx, bucketInput{Name: "reports"}, p); err != nil {
			t.Fatalf("delete empty bucket: %v", err)
		}
	})
}

func TestBucketsCreate_BlankNameIsBadRequest(t *testing.T) {
	deps := testDeps(t, newStores(openMem(t)))
	if _, err := bucketsCreateHandler(deps)(context.Background(), bucketInput{Name: "  "}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Fatalf("create blank = %v", err)
	}
}

func TestBucketsList_CreatedAtMeaning(t *testing.T) {
	mem, err := bucketsListHandler(testDeps(t, newStores(openMem(t))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil || mem.CreatedAtMeaning != "created" || mem.Buckets == nil {
		t.Fatalf("mem = %+v, %v", mem, err)
	}
	local, err := bucketsListHandler(testDeps(t, newStores(openLocal(t))))(context.Background(), storeInput{}, principalFor("u"))
	if err != nil || local.CreatedAtMeaning != "modified" {
		t.Fatalf("local = %+v, %v", local, err)
	}
}

func TestBucketsDelete_RefusesNonEmptyAndKeepsFiles(t *testing.T) {
	root := t.TempDir()
	drv := localdriver.New()
	if err := drv.Open(context.Background(), "file://"+root); err != nil {
		t.Fatalf("open: %v", err)
	}
	tv := openTrove(t, drv)
	mustBucket(t, tv, "reports")
	put(t, tv, "reports", "2026/q3.csv", "a,b")
	deps := testDeps(t, newStores(tv))

	_, err := bucketsDeleteHandler(deps)(context.Background(), bucketInput{Name: "reports"}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("delete non-empty = %v, want CONFLICT", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "reports", "2026", "q3.csv")); statErr != nil {
		t.Fatalf("the file is gone after a refused delete: %v", statErr)
	}
}

func TestBucketsDelete_MissingIsNotFound(t *testing.T) {
	deps := testDeps(t, newStores(openMem(t)))
	if _, err := bucketsDeleteHandler(deps)(context.Background(), bucketInput{Name: "nope"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("delete missing = %v", err)
	}
}
