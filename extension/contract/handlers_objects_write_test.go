package contract

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
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
