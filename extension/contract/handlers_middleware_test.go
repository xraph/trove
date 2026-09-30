package contract

import (
	"bytes"
	"context"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/middleware/compress"
	"github.com/xraph/trove/middleware/encrypt"
)

func hasWarning(out middlewareListOutput, code string) bool {
	for _, w := range out.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestMiddlewareList_NoneRegistered(t *testing.T) {
	deps := testDeps(t, newStores(openMem(t)))
	out, err := middlewareListHandler(deps)(context.Background(), middlewareListInput{}, principalFor("u"))
	if err != nil {
		t.Fatalf("middleware.list: %v", err)
	}
	if out.Registrations == nil || len(out.Registrations) != 0 || len(out.Warnings) != 0 {
		t.Fatalf("out = %+v, want empty registrations as [] and no warnings", out)
	}
}

func TestMiddlewareList_MatchesAKeyAndWarns(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	tv := openMem(t,
		trove.WithScopedMiddleware(middleware.ForBuckets("reports"), compress.New()),
		trove.WithMiddleware(encrypt.New(encrypt.WithKeyProvider(encrypt.NewStaticKeyProvider(key)))),
	)
	deps := testDeps(t, newStores(tv))
	out, err := middlewareListHandler(deps)(context.Background(),
		middlewareListInput{Bucket: "logs", Key: "a.txt"}, principalFor("u"))
	if err != nil {
		t.Fatalf("middleware.list: %v", err)
	}
	if len(out.Registrations) != 2 {
		t.Fatalf("registrations = %+v", out.Registrations)
	}
	for _, r := range out.Registrations {
		if r.MatchesWrite == nil || r.MatchesRead == nil {
			t.Fatalf("%s: matches not computed for a bucket and key", r.Name)
		}
		wantMatch := r.Name == "encrypt"
		if *r.MatchesWrite != wantMatch || *r.MatchesRead != wantMatch {
			t.Errorf("%s matches logs/a.txt: write=%v read=%v, want %v", r.Name, *r.MatchesWrite, *r.MatchesRead, wantMatch)
		}
	}
	for _, code := range []string{"read-order", "bypass"} {
		if !hasWarning(out, code) {
			t.Errorf("missing warning %q in %+v", code, out.Warnings)
		}
	}
}

func TestMiddlewareList_WithoutKeyLeavesMatchesNull(t *testing.T) {
	tv := openMem(t, trove.WithMiddleware(compress.New()))
	out, err := middlewareListHandler(testDeps(t, newStores(tv)))(context.Background(),
		middlewareListInput{Bucket: "logs"}, principalFor("u"))
	if err != nil {
		t.Fatalf("middleware.list: %v", err)
	}
	if out.Registrations[0].MatchesWrite != nil || out.Registrations[0].MatchesRead != nil {
		t.Fatalf("matches computed without a key: %+v", out.Registrations[0])
	}
}

func TestMatching_RunOrderAndDirection(t *testing.T) {
	tv := openMem(t,
		trove.WithMiddlewareAt(10, compress.New()),
		trove.WithWriteMiddleware(compress.New()),
	)
	ctx := context.Background()
	write := matching(ctx, tv, "b", "k", middleware.DirectionWrite)
	read := matching(ctx, tv, "b", "k", middleware.DirectionRead)
	if len(write) != 2 || write[0].Priority != 0 || write[1].Priority != 10 {
		t.Fatalf("write pipeline = %+v, want priority 0 then 10", write)
	}
	if len(read) != 1 || read[0].Priority != 10 {
		t.Fatalf("read pipeline = %+v, want only the read-write one", read)
	}
	if got := matchingAny(ctx, tv, "b", "k"); len(got) != 2 {
		t.Fatalf("matchingAny = %+v, want each registration once", got)
	}
}

func TestMiddlewareList_ScopeWarningsFollowTheScopeType(t *testing.T) {
	ctx := context.Background()
	list := func(scope middleware.Scope) middlewareListOutput {
		t.Helper()
		tv := openMem(t, trove.WithScopedMiddleware(scope, compress.New()))
		out, err := middlewareListHandler(testDeps(t, newStores(tv)))(ctx, middlewareListInput{}, principalFor("u"))
		if err != nil {
			t.Fatalf("middleware.list: %v", err)
		}
		return out
	}
	always := func(context.Context, string, string) bool { return true }

	for name, scope := range map[string]middleware.Scope{
		"When":     middleware.When(always),
		"WhenDesc": middleware.WhenDesc("tenant a only", always),
		"nested":   &middleware.ScopeAnd{Scopes: []middleware.Scope{middleware.ForBuckets("b"), &middleware.ScopeNot{Inner: middleware.When(always)}}},
	} {
		if out := list(scope); !hasWarning(out, "cached-custom-scope") {
			t.Errorf("%s: missing cached-custom-scope in %+v", name, out.Warnings)
		}
	}
	if out := list(middleware.ForBuckets("custom")); hasWarning(out, "cached-custom-scope") {
		t.Errorf("a bucket named custom warned: %+v", out.Warnings)
	}
	if out := list(middleware.ForContentTypes("image/*")); !hasWarning(out, "content-type-scope") {
		t.Errorf("missing content-type-scope in %+v", out.Warnings)
	}
	if out := list(middleware.ForBuckets("content-type(")); hasWarning(out, "content-type-scope") {
		t.Errorf("a bucket named like a content-type scope warned: %+v", out.Warnings)
	}
}
