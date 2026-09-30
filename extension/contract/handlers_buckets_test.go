package contract

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/drivers/memdriver"
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

// pagedEmptyMem lists zero objects with a continuation token, as Azure can,
// and records whether DeleteBucket was reached.
type pagedEmptyMem struct {
	*memdriver.MemDriver
	deleted bool
}

func (d *pagedEmptyMem) List(context.Context, string, ...driver.ListOption) (*driver.ObjectIterator, error) {
	return driver.NewObjectIterator(nil, "more"), nil
}

func (d *pagedEmptyMem) DeleteBucket(context.Context, string) error {
	d.deleted = true
	return nil
}

func TestBucketsDelete_EmptyPageWithTokenIsNotEmpty(t *testing.T) {
	drv := &pagedEmptyMem{MemDriver: memdriver.New()}
	if err := drv.Open(context.Background(), ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	deps := testDeps(t, newStores(openTrove(t, drv)))

	_, err := bucketsDeleteHandler(deps)(context.Background(), bucketInput{Name: "reports"}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("delete with an unfinished listing = %v, want CONFLICT", err)
	}
	if drv.deleted {
		t.Fatal("DeleteBucket was called although the listing had more pages")
	}
}

// TestBucketsDelete_ChecksTheDriverItDeletesFrom routes every key to a second
// backend that holds an object. Trove.List would read that backend and call
// the default bucket empty; the check must read the default, which is where
// DeleteBucket acts, and find the object there.
func TestBucketsDelete_ChecksTheDriverItDeletesFrom(t *testing.T) {
	def := openMem(t)
	mustBucket(t, def, "reports")
	put(t, def, "reports", "a.txt", "x")
	other := memdriver.New()
	if err := other.Open(context.Background(), ""); err != nil {
		t.Fatalf("open other: %v", err)
	}
	tv := openTrove(t, def.Driver(), trove.WithBackend("other", other), trove.WithRouteFunc(func(string, string) string { return "other" }))
	deps := testDeps(t, newStores(tv))

	_, err := bucketsDeleteHandler(deps)(context.Background(), bucketInput{Name: "reports"}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("delete = %v, want CONFLICT because the default driver holds a.txt", err)
	}
}
