package contract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/go-utils/log"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/middleware/compress"
)

func TestObjectsDelete_RemovesTheBytes(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		mustBucket(t, tv, "data")
		put(t, tv, "data", "a.txt", "x")
		deps := testDeps(t, newStores(tv))
		if _, err := objectsDeleteHandler(deps)(context.Background(), objectKeyInput{Bucket: "data", Key: "a.txt"}, principalFor("u")); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := tv.Head(context.Background(), "data", "a.txt"); err == nil {
			t.Fatal("the object is still there")
		}
	})
}

func TestObjectsDelete_RefusesTheCASBucket(t *testing.T) {
	tv := openMem(t, trove.WithCAS(cas.AlgSHA256))
	mustBucket(t, tv, "cas")
	hash, _, err := tv.CAS().Store(context.Background(), strings.NewReader("blob"))
	if err != nil {
		t.Fatalf("cas store: %v", err)
	}
	deps := testDeps(t, newStores(tv))
	_, err = objectsDeleteHandler(deps)(context.Background(), objectKeyInput{Bucket: "cas", Key: hash}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("delete in the CAS bucket = %v, want CONFLICT", err)
	}
	if ok, _ := tv.CAS().Exists(context.Background(), hash); !ok {
		t.Fatal("the CAS entry is gone")
	}
}

func TestObjectsCopy_CopiesAndRespectsOverwrite(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		mustBucket(t, tv, "src")
		mustBucket(t, tv, "dst")
		put(t, tv, "src", "a.txt", "hello")
		put(t, tv, "dst", "taken.txt", "old")
		deps := testDeps(t, newStores(tv))
		ctx := context.Background()
		h := objectsCopyHandler(deps)

		out, err := h(ctx, objectsCopyInput{SrcBucket: "src", SrcKey: "a.txt", DstBucket: "dst", DstKey: "b.txt"}, principalFor("u"))
		if err != nil || out.Key != "b.txt" {
			t.Fatalf("copy = %+v, %v", out, err)
		}
		if _, err := h(ctx, objectsCopyInput{SrcBucket: "src", SrcKey: "a.txt", DstBucket: "dst", DstKey: "taken.txt"}, principalFor("u")); codeOf(err) != "CONFLICT" {
			t.Fatalf("copy onto an existing key = %v, want CONFLICT", err)
		}
		if _, err := h(ctx, objectsCopyInput{SrcBucket: "src", SrcKey: "a.txt", DstBucket: "dst", DstKey: "taken.txt", Overwrite: true}, principalFor("u")); err != nil {
			t.Fatalf("copy with overwrite: %v", err)
		}
		if _, err := h(ctx, objectsCopyInput{SrcBucket: "src", SrcKey: "a.txt", DstBucket: "src", DstKey: "a.txt"}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
			t.Fatalf("copy onto itself = %v, want BAD_REQUEST", err)
		}
		if _, err := h(ctx, objectsCopyInput{SrcBucket: "src", SrcKey: "nope", DstBucket: "dst", DstKey: "n.txt"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
			t.Fatalf("copy of a missing source = %v, want NOT_FOUND", err)
		}
	})
}

func TestObjectsCopy_RefusesAcrossDifferentMiddleware(t *testing.T) {
	tv := openMem(t, trove.WithScopedMiddleware(middleware.ForBuckets("zipped"), compress.New()))
	mustBucket(t, tv, "zipped")
	mustBucket(t, tv, "plain")
	put(t, tv, "zipped", "big.txt", strings.Repeat("hello ", 400))
	deps := testDeps(t, newStores(tv))
	_, err := objectsCopyHandler(deps)(context.Background(),
		objectsCopyInput{SrcBucket: "zipped", SrcKey: "big.txt", DstBucket: "plain", DstKey: "big.txt"}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("copy across middleware = %v, want CONFLICT", err)
	}
	if _, headErr := tv.Head(context.Background(), "plain", "big.txt"); headErr == nil {
		t.Fatal("the refused copy wrote the object anyway")
	}
}

// openPresign opens a Trove whose default driver can presign, reusing the
// presignMem double from handlers_objects_test.go.
func openPresign(t *testing.T, opts ...trove.Option) *trove.Trove {
	t.Helper()
	m := memdriver.New()
	if err := m.Open(context.Background(), ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	var _ driver.PresignDriver = presignMem{m}
	return openTrove(t, presignMem{m}, opts...)
}

func TestObjectsPresign(t *testing.T) {
	tv := openPresign(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	deps := testDeps(t, newStores(tv))
	out, err := objectsPresignHandler(deps)(context.Background(), objectsPresignInput{Bucket: "data", Key: "a.txt"}, principalFor("u"))
	if err != nil || out.URL != "https://signed.example/data/a.txt" || out.ExpiresAt == "" {
		t.Fatalf("presign = %+v, %v", out, err)
	}
}

func TestObjectsPresign_RefusedWithReason(t *testing.T) {
	tv := openPresign(t, trove.WithMiddleware(compress.New()))
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	_, err := objectsPresignHandler(testDeps(t, newStores(tv)))(context.Background(), objectsPresignInput{Bucket: "data", Key: "a.txt"}, principalFor("u"))
	if codeOf(err) != "UNAVAILABLE" || !strings.Contains(err.Error(), "compress") {
		t.Fatalf("presign with middleware = %v, want UNAVAILABLE naming compress", err)
	}

	plain := openMem(t)
	mustBucket(t, plain, "data")
	put(t, plain, "data", "a.txt", "x")
	if _, err := objectsPresignHandler(testDeps(t, newStores(plain)))(context.Background(), objectsPresignInput{Bucket: "data", Key: "a.txt"}, principalFor("u")); codeOf(err) != "UNAVAILABLE" {
		t.Fatalf("presign on memdriver = %v, want UNAVAILABLE", err)
	}
}

// TestObjectsPresign_SignsWithTheRoutedDriver routes *.log from a signing
// default to a plain backend. The default can sign, so using it would hand
// out a link for bytes it does not hold.
func TestObjectsPresign_SignsWithTheRoutedDriver(t *testing.T) {
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
	h := objectsPresignHandler(testDeps(t, newStores(tv)))

	if _, err := h(context.Background(), objectsPresignInput{Bucket: "data", Key: "app.log"}, principalFor("u")); codeOf(err) != "UNAVAILABLE" {
		t.Fatalf("presign on a routed key = %v, want UNAVAILABLE", err)
	}
	out, err := h(context.Background(), objectsPresignInput{Bucket: "data", Key: "app.txt"}, principalFor("u"))
	if err != nil || out.URL != "https://signed.example/data/app.txt" {
		t.Fatalf("presign on the default key = %+v, %v", out, err)
	}
}

// TestObjectsCopy_RefusesAcrossBackends routes *.log to a second backend.
// Copy writes through the source key's driver, so a.txt copied to a.log
// would land on the default backend where a.log is never looked up.
func TestObjectsCopy_RefusesAcrossBackends(t *testing.T) {
	def := memdriver.New()
	if err := def.Open(context.Background(), ""); err != nil {
		t.Fatalf("open default: %v", err)
	}
	archive := memdriver.New()
	if err := archive.Open(context.Background(), ""); err != nil {
		t.Fatalf("open archive: %v", err)
	}
	tv := openTrove(t, def, trove.WithBackend("archive", archive), trove.WithRoute("*.log", "archive"))
	mustBucket(t, tv, "data")
	if err := archive.CreateBucket(context.Background(), "data"); err != nil {
		t.Fatalf("create bucket on the routed backend: %v", err)
	}
	put(t, tv, "data", "a.txt", "x")
	deps := testDeps(t, newStores(tv))
	h := objectsCopyHandler(deps)

	_, err := h(context.Background(), objectsCopyInput{SrcBucket: "data", SrcKey: "a.txt", DstBucket: "data", DstKey: "a.log"}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("copy across backends = %v, want CONFLICT", err)
	}
	if _, err := def.Head(context.Background(), "data", "a.log"); err == nil {
		t.Fatal("the refused copy wrote a.log to the source backend")
	}
	if _, err := archive.Head(context.Background(), "data", "a.log"); err == nil {
		t.Fatal("the refused copy wrote a.log to the routed backend")
	}

	// Two keys on the same routed backend still copy.
	put(t, tv, "data", "b.log", "y")
	if _, err := h(context.Background(), objectsCopyInput{SrcBucket: "data", SrcKey: "b.log", DstBucket: "data", DstKey: "c.log"}, principalFor("u")); err != nil {
		t.Fatalf("copy within one routed backend: %v", err)
	}
}

// TestObjectsCopy_ComparesMiddlewareInstances scopes two separate
// instances of one middleware type to different buckets. They share a
// name but not a configuration, so the guard must treat them as different.
func TestObjectsCopy_ComparesMiddlewareInstances(t *testing.T) {
	tv := openMem(t,
		trove.WithScopedMiddleware(middleware.ForBuckets("x"), compress.New()),
		trove.WithScopedMiddleware(middleware.ForBuckets("y"), compress.New()),
	)
	mustBucket(t, tv, "x")
	mustBucket(t, tv, "y")
	put(t, tv, "x", "k", strings.Repeat("hello ", 400))
	deps := testDeps(t, newStores(tv))
	_, err := objectsCopyHandler(deps)(context.Background(),
		objectsCopyInput{SrcBucket: "x", SrcKey: "k", DstBucket: "y", DstKey: "k"}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("copy across two instances of one middleware = %v, want CONFLICT", err)
	}
	if _, headErr := tv.Head(context.Background(), "y", "k"); headErr == nil {
		t.Fatal("the refused copy wrote the object anyway")
	}
}

// TestObjectsCopy_AllowsOneMiddlewareInstance checks the guard does not
// refuse when one instance covers both source and destination.
func TestObjectsCopy_AllowsOneMiddlewareInstance(t *testing.T) {
	tv := openMem(t, trove.WithScopedMiddleware(middleware.ForBuckets("x", "y"), compress.New()))
	mustBucket(t, tv, "x")
	mustBucket(t, tv, "y")
	put(t, tv, "x", "k", strings.Repeat("hello ", 400))
	deps := testDeps(t, newStores(tv))
	if _, err := objectsCopyHandler(deps)(context.Background(),
		objectsCopyInput{SrcBucket: "x", SrcKey: "k", DstBucket: "y", DstKey: "k"}, principalFor("u")); err != nil {
		t.Fatalf("copy under one shared instance: %v", err)
	}
}

func TestObjectsCopy_MissingDestinationBucket(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		mustBucket(t, tv, "src")
		put(t, tv, "src", "a.txt", "hello")
		h := objectsCopyHandler(testDeps(t, newStores(tv)))
		for _, overwrite := range []bool{false, true} {
			_, err := h(context.Background(), objectsCopyInput{SrcBucket: "src", SrcKey: "a.txt", DstBucket: "typo", DstKey: "a.txt", Overwrite: overwrite}, principalFor("u"))
			if codeOf(err) != "NOT_FOUND" || !strings.Contains(err.Error(), "bucket not found") {
				t.Fatalf("copy to a missing bucket (overwrite=%v) = %v, want NOT_FOUND bucket not found", overwrite, err)
			}
		}
	})
}

// TestObjectsCopy_MissingDestinationBucketCreatesNothingOnDisk makes sure a
// refused copy leaves no directory behind on localdriver, which would
// otherwise MkdirAll the typo.
func TestObjectsCopy_MissingDestinationBucketCreatesNothingOnDisk(t *testing.T) {
	root := t.TempDir()
	drv := localdriver.New()
	if err := drv.Open(context.Background(), "file://"+root); err != nil {
		t.Fatalf("open localdriver: %v", err)
	}
	tv := openTrove(t, drv)
	mustBucket(t, tv, "src")
	put(t, tv, "src", "a.txt", "hello")
	h := objectsCopyHandler(testDeps(t, newStores(tv)))
	for _, overwrite := range []bool{false, true} {
		if _, err := h(context.Background(), objectsCopyInput{SrcBucket: "src", SrcKey: "a.txt", DstBucket: "typo", DstKey: "a.txt", Overwrite: overwrite}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
			t.Fatalf("overwrite=%v: %v, want NOT_FOUND", overwrite, err)
		}
		if _, err := os.Stat(filepath.Join(root, "typo")); !os.IsNotExist(err) {
			t.Fatalf("overwrite=%v: the typo directory exists (stat err %v)", overwrite, err)
		}
	}
}

func TestObjectsPresign_LogsTheLink(t *testing.T) {
	tv := openPresign(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	deps := testDeps(t, newStores(tv))
	logger := log.NewTestLogger()
	deps.Logger = logger
	out, err := objectsPresignHandler(deps)(context.Background(), objectsPresignInput{Bucket: "data", Key: "a.txt"}, principalFor("user_5"))
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	entries := logger.(*log.TestLogger).GetLogsByLevel("INFO")
	if len(entries) != 1 {
		t.Fatalf("info entries = %d, want 1", len(entries))
	}
	want := map[string]any{"store": SingleStoreName, "bucket": "data", "key": "a.txt", "subject": "user_5", "expiresAt": out.ExpiresAt}
	for k, v := range want {
		if got, ok := entries[0].Field(k); !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
}

// failingPresignMem claims it can presign and always fails to.
type failingPresignMem struct{ *memdriver.MemDriver }

func (failingPresignMem) PresignGet(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("no signing key")
}

func (failingPresignMem) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("no signing key")
}

func TestObjectsPresign_FailureLogSaysWhichObject(t *testing.T) {
	m := memdriver.New()
	if err := m.Open(context.Background(), ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	tv := openTrove(t, failingPresignMem{m})
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	deps := testDeps(t, newStores(tv))
	logger := log.NewTestLogger()
	deps.Logger = logger
	if _, err := objectsPresignHandler(deps)(context.Background(), objectsPresignInput{Bucket: "data", Key: "a.txt"}, principalFor("u")); codeOf(err) != "UNAVAILABLE" {
		t.Fatalf("presign with a failing signer = %v, want UNAVAILABLE", err)
	}
	entries := logger.(*log.TestLogger).GetLogsByLevel("ERROR")
	if len(entries) != 1 {
		t.Fatalf("error entries = %d, want 1", len(entries))
	}
	for k, v := range map[string]any{"store": SingleStoreName, "bucket": "data", "key": "a.txt"} {
		if got, ok := entries[0].Field(k); !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
}
