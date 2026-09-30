package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
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

// TestObjectsList_LimitClamps seeds one more key than the cap so a missing
// clamp shows: without it the limit of 5000 would return all 1001.
func TestObjectsList_LimitClamps(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "many")
	for i := 0; i < 1001; i++ {
		put(t, tv, "many", fmt.Sprintf("k%04d", i), "x")
	}
	deps := testDeps(t, newStores(tv))
	ctx := context.Background()

	out, err := objectsListHandler(deps)(ctx, objectsListInput{Bucket: "many", Limit: 5000, Delimiter: strPtr("")}, principalFor("u"))
	if err != nil {
		t.Fatalf("limit 5000: %v", err)
	}
	if len(out.Objects) != 1000 || out.NextCursor == nil {
		t.Fatalf("limit 5000 returned %d objects, nextCursor %v; want exactly 1000 and a cursor", len(out.Objects), out.NextCursor)
	}
	out, err = objectsListHandler(deps)(ctx, objectsListInput{Bucket: "many", Limit: -3, Delimiter: strPtr("")}, principalFor("u"))
	if err != nil {
		t.Fatalf("limit -3: %v", err)
	}
	if len(out.Objects) != 100 {
		t.Fatalf("limit -3 returned %d objects, want the default of 100", len(out.Objects))
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

// presignMem is a memdriver that claims it can presign.
type presignMem struct{ *memdriver.MemDriver }

func (presignMem) PresignGet(_ context.Context, bucket, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + bucket + "/" + key, nil
}

func (presignMem) PresignPut(_ context.Context, bucket, key string, _ time.Duration) (string, error) {
	return "https://signed.example/put/" + bucket + "/" + key, nil
}

// TestObjectsHead_PresignJudgesTheRoutedDriver gives the Trove a presign
// capable default and routes *.log to a plain backend. The default can sign,
// so judging it would wrongly offer a link for the routed key.
func TestObjectsHead_PresignJudgesTheRoutedDriver(t *testing.T) {
	def := presignMem{memdriver.New()}
	if err := def.Open(context.Background(), ""); err != nil {
		t.Fatalf("open default: %v", err)
	}
	plain := memdriver.New()
	if err := plain.Open(context.Background(), ""); err != nil {
		t.Fatalf("open plain: %v", err)
	}
	tv := openTrove(t, def, trove.WithBackend("plain", plain), trove.WithRoute("*.log", "plain"))
	mustBucket(t, tv, "data")
	if err := plain.CreateBucket(context.Background(), "data"); err != nil {
		t.Fatalf("create bucket on the routed backend: %v", err)
	}
	put(t, tv, "data", "app.log", "line")
	put(t, tv, "data", "app.txt", "text")
	deps := testDeps(t, newStores(tv))

	routed, err := objectsHeadHandler(deps)(context.Background(), objectKeyInput{Bucket: "data", Key: "app.log"}, principalFor("u"))
	if err != nil {
		t.Fatalf("head routed: %v", err)
	}
	if routed.Presign.Available || routed.Presign.Reason == nil || !strings.Contains(*routed.Presign.Reason, "cannot create presigned links") {
		t.Fatalf("routed key presign = %+v, want unavailable because the plain backend cannot sign", routed.Presign)
	}
	direct, err := objectsHeadHandler(deps)(context.Background(), objectKeyInput{Bucket: "data", Key: "app.txt"}, principalFor("u"))
	if err != nil {
		t.Fatalf("head default: %v", err)
	}
	if !direct.Presign.Available {
		t.Fatalf("default key presign = %+v, want available", direct.Presign)
	}
}

// TestObjectsList_SaysWhenTheStoreIsRouted routes *.log to a second
// backend. The listing reads the default only, so app.log is missing from
// it, and routed is what tells the page the listing may be partial.
func TestObjectsList_SaysWhenTheStoreIsRouted(t *testing.T) {
	plain := openMem(t)
	mustBucket(t, plain, "data")
	out, err := objectsListHandler(testDeps(t, newStores(plain)))(context.Background(), objectsListInput{Bucket: "data"}, principalFor("u"))
	if err != nil || out.Routed {
		t.Fatalf("unrouted listing = %+v, %v; want routed false", out, err)
	}

	archive := memdriver.New()
	if err = archive.Open(context.Background(), ""); err != nil {
		t.Fatalf("open archive: %v", err)
	}
	if err = archive.CreateBucket(context.Background(), "data"); err != nil {
		t.Fatalf("create bucket on the archive: %v", err)
	}
	tv := openMem(t, trove.WithBackend("archive", archive), trove.WithRoute("*.log", "archive"))
	mustBucket(t, tv, "data")
	put(t, tv, "data", "app.txt", "x")
	put(t, tv, "data", "app.log", "y")
	out, err = objectsListHandler(testDeps(t, newStores(tv)))(context.Background(), objectsListInput{Bucket: "data"}, principalFor("u"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !out.Routed {
		t.Fatalf("routed listing = %+v, want routed true", out)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"routed":true`) {
		t.Fatalf("wire = %s, want routed true", raw)
	}
}
