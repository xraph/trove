package contract

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/drivers/memdriver"
)

// opener builds a fresh Trove over one driver, with opts applied.
type opener func(t *testing.T, opts ...trove.Option) *trove.Trove

func openMem(t *testing.T, opts ...trove.Option) *trove.Trove {
	t.Helper()
	drv := memdriver.New()
	if err := drv.Open(context.Background(), ""); err != nil {
		t.Fatalf("open memdriver: %v", err)
	}
	return openTrove(t, drv, opts...)
}

func openLocal(t *testing.T, opts ...trove.Option) *trove.Trove {
	t.Helper()
	drv := localdriver.New()
	if err := drv.Open(context.Background(), "file://"+t.TempDir()); err != nil {
		t.Fatalf("open localdriver: %v", err)
	}
	return openTrove(t, drv, opts...)
}

func openTrove(t *testing.T, drv driver.Driver, opts ...trove.Option) *trove.Trove {
	t.Helper()
	tv, err := trove.Open(drv, opts...)
	if err != nil {
		t.Fatalf("trove.Open: %v", err)
	}
	t.Cleanup(func() { _ = tv.Close(context.Background()) })
	return tv
}

// forEachBackend runs fn once per backend CI can run: memdriver and
// localdriver on a real temp directory.
func forEachBackend(t *testing.T, fn func(t *testing.T, open opener)) {
	t.Helper()
	for _, b := range []struct {
		name string
		open opener
	}{{"mem", openMem}, {"local", openLocal}} {
		t.Run(b.name, func(t *testing.T) { fn(t, b.open) })
	}
}

func newStores(tv *trove.Trove) *Stores {
	return NewSingleStore(tv, Flags{})
}

func testContent(t *testing.T) *Content {
	t.Helper()
	s, err := NewSigner(bytes.Repeat([]byte("s"), 32))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return &Content{Signer: s, Path: "/dashboard/trove/content", MaxUploadBytes: 1 << 20}
}

func testDeps(t *testing.T, stores *Stores) Deps {
	t.Helper()
	return Deps{Stores: stores, Content: testContent(t)}
}

func codeOf(err error) dashcontract.ErrorCode {
	var ce *dashcontract.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func mustBucket(t *testing.T, tv *trove.Trove, name string) {
	t.Helper()
	if err := tv.CreateBucket(context.Background(), name); err != nil {
		t.Fatalf("CreateBucket(%q): %v", name, err)
	}
}

func put(t *testing.T, tv *trove.Trove, bucket, key, body string, opts ...driver.PutOption) {
	t.Helper()
	if _, err := tv.Put(context.Background(), bucket, key, strings.NewReader(body), opts...); err != nil {
		t.Fatalf("Put(%q, %q): %v", bucket, key, err)
	}
}

func principalFor(subject string) dashcontract.Principal {
	return dashcontract.Principal{User: &dashauth.UserInfo{Subject: subject}}
}
