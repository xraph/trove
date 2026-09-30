package contract

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware/compress"
)

func seedListing(t *testing.T, tv *trove.Trove) {
	t.Helper()
	mustBucket(t, tv, "data")
	for _, k := range []string{"a/1.txt", "a/2.txt", "b/c/d.txt", "top.txt", "zed.txt"} {
		put(t, tv, "data", k, "x")
	}
}

func strPtr(s string) *string { return &s }

func TestObjectsList_FoldsAndPages(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		seedListing(t, tv)
		deps := testDeps(t, newStores(tv))
		ctx := context.Background()

		var items []string
		cursor := ""
		pages := 0
		for {
			out, err := objectsListHandler(deps)(ctx, objectsListInput{Bucket: "data", Cursor: cursor, Limit: 2}, principalFor("u"))
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if !out.FoldersSupported {
				t.Fatal("foldersSupported is false on a folding driver")
			}
			items = append(items, out.Prefixes...)
			for _, o := range out.Objects {
				items = append(items, o.Key)
			}
			pages++
			if out.NextCursor == nil {
				break
			}
			cursor = *out.NextCursor
			if pages > 10 {
				t.Fatal("listing never finished")
			}
		}
		got := strings.Join(items, ",")
		for _, want := range []string{"a/", "b/", "top.txt", "zed.txt"} {
			if !strings.Contains(got, want) {
				t.Errorf("items %q missing %q", got, want)
			}
		}
		if strings.Contains(got, "a/1.txt") {
			t.Errorf("items %q contains a key that should have folded", got)
		}
		if pages != 2 {
			t.Errorf("pages = %d, want 2 for 4 items at limit 2", pages)
		}
	})
}

func TestObjectsList_FlatListingHasNullPrefixes(t *testing.T) {
	tv := openMem(t)
	seedListing(t, tv)
	out, err := objectsListHandler(testDeps(t, newStores(tv)))(context.Background(),
		objectsListInput{Bucket: "data", Delimiter: strPtr("")}, principalFor("u"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out.Prefixes != nil || len(out.Objects) != 5 || out.FoldersSupported {
		t.Fatalf("flat = %+v", out)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"prefixes":null`) {
		t.Fatalf("flat listing JSON = %s, want prefixes null", b)
	}
}

func TestObjectsList_EmptyIsArrayNotNull(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "empty")
	out, err := objectsListHandler(testDeps(t, newStores(tv)))(context.Background(), objectsListInput{Bucket: "empty"}, principalFor("u"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"objects":[]`) || !strings.Contains(string(b), `"nextCursor":null`) {
		t.Fatalf("empty listing JSON = %s", b)
	}
}

func TestObjectsList_BadInput(t *testing.T) {
	tv := openMem(t)
	seedListing(t, tv)
	deps := testDeps(t, newStores(tv))
	ctx := context.Background()
	if _, err := objectsListHandler(deps)(ctx, objectsListInput{}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("no bucket = %v", err)
	}
	if _, err := objectsListHandler(deps)(ctx, objectsListInput{Bucket: "data", Cursor: "!!!"}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("bad cursor = %v", err)
	}
	if _, err := objectsListHandler(deps)(ctx, objectsListInput{Bucket: "nope"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Errorf("missing bucket = %v", err)
	}
}

func TestObjectsList_LimitClamps(t *testing.T) {
	tv := openMem(t)
	seedListing(t, tv)
	deps := testDeps(t, newStores(tv))
	out, err := objectsListHandler(deps)(context.Background(), objectsListInput{Bucket: "data", Limit: 5000, Delimiter: strPtr("")}, principalFor("u"))
	if err != nil || len(out.Objects) != 5 {
		t.Fatalf("limit 5000 = %+v, %v", out, err)
	}
	out, err = objectsListHandler(deps)(context.Background(), objectsListInput{Bucket: "data", Limit: -3, Delimiter: strPtr("")}, principalFor("u"))
	if err != nil || len(out.Objects) != 5 {
		t.Fatalf("limit -3 = %+v, %v; want the default of 100", out, err)
	}
}

// TestObjectsList_StoresAreIsolated writes to store a and lists store b,
// asserting on identity: a count check would pass if the wrong store's rows
// came back in the right number.
func TestObjectsList_StoresAreIsolated(t *testing.T) {
	a, b := openMem(t), openMem(t)
	mustBucket(t, a, "data")
	mustBucket(t, b, "data")
	put(t, a, "data", "only-in-a.txt", "x")
	put(t, b, "data", "only-in-b.txt", "y")
	stores, err := NewStores("a", []Store{{Name: "a", Trove: a}, {Name: "b", Trove: b}})
	if err != nil {
		t.Fatalf("NewStores: %v", err)
	}
	deps := testDeps(t, stores)
	for store, want := range map[string]string{"a": "only-in-a.txt", "b": "only-in-b.txt", "": "only-in-a.txt"} {
		out, err := objectsListHandler(deps)(context.Background(), objectsListInput{Store: store, Bucket: "data"}, principalFor("u"))
		if err != nil {
			t.Fatalf("list store %q: %v", store, err)
		}
		if len(out.Objects) != 1 || out.Objects[0].Key != want {
			t.Fatalf("store %q listed %+v, want only %s", store, out.Objects, want)
		}
	}
}

func TestObjectsHead_DetailAndMiddleware(t *testing.T) {
	tv := openMem(t, trove.WithMiddleware(compress.New()))
	mustBucket(t, tv, "data")
	put(t, tv, "data", "notes.txt", strings.Repeat("hello ", 400),
		driver.WithContentType("text/plain"), driver.WithMetadata(map[string]string{"owner": "ops"}))
	out, err := objectsHeadHandler(testDeps(t, newStores(tv)))(context.Background(),
		objectKeyInput{Bucket: "data", Key: "notes.txt"}, principalFor("u"))
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if out.Object.Key != "notes.txt" || out.Object.Metadata["owner"] != "ops" || out.Object.ContentType == nil || *out.Object.ContentType != "text/plain" {
		t.Fatalf("object = %+v", out.Object)
	}
	if out.Object.StoredSize >= int64(len(strings.Repeat("hello ", 400))) {
		t.Errorf("storedSize %d is not the compressed size", out.Object.StoredSize)
	}
	if len(out.Middleware) != 1 || out.Middleware[0].Name != "compress" {
		t.Fatalf("middleware = %+v", out.Middleware)
	}
	if out.Presign.Available || out.Presign.Reason == nil {
		t.Fatalf("presign on memdriver = %+v, want unavailable with a reason", out.Presign)
	}
}

func TestObjectsHead_Missing(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	if _, err := objectsHeadHandler(deps)(context.Background(), objectKeyInput{Bucket: "data", Key: "nope"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("head missing = %v", err)
	}
	if _, err := objectsHeadHandler(deps)(context.Background(), objectKeyInput{Bucket: "data"}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Fatalf("head without key = %v", err)
	}
}
