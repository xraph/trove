package contract

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
)

func ticketFrom(t *testing.T, deps Deps, link string) Ticket {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	if u.Path != deps.Content.Path {
		t.Fatalf("link path = %q, want %q", u.Path, deps.Content.Path)
	}
	tk, err := deps.Content.Signer.Verify(u.Query().Get("t"))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return tk
}

func TestContentURL_DownloadAndPreview(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	deps := testDeps(t, newStores(tv))
	ctx := context.Background()
	h := objectsContentURLHandler(deps)

	out, err := h(ctx, contentURLInput{Bucket: "data", Key: "a.txt"}, principalFor("user_1"))
	if err != nil {
		t.Fatalf("contentUrl: %v", err)
	}
	tk := ticketFrom(t, deps, out.URL)
	if tk.Op != OpDownload || tk.Key != "a.txt" || tk.Store != SingleStoreName || tk.Subject != "user_1" || out.ExpiresAt == "" {
		t.Fatalf("download ticket = %+v, out = %+v", tk, out)
	}

	out, err = h(ctx, contentURLInput{Bucket: "data", Key: "a.txt", Purpose: "preview"}, principalFor("u"))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if tk := ticketFrom(t, deps, out.URL); tk.Op != OpPreview || tk.Limit != maxPreviewBytes {
		t.Fatalf("preview ticket = %+v", tk)
	}
	out, err = h(ctx, contentURLInput{Bucket: "data", Key: "a.txt", Purpose: "preview", Limit: 1 << 30}, principalFor("u"))
	if err != nil || ticketFrom(t, deps, out.URL).Limit != maxPreviewBytes {
		t.Fatalf("preview over the cap = %+v, %v", out, err)
	}
}

func TestContentURL_BadInput(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	h := objectsContentURLHandler(deps)
	ctx := context.Background()
	if _, err := h(ctx, contentURLInput{Bucket: "data", Key: "nope"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Errorf("missing object = %v", err)
	}
	put(t, tv, "data", "a.txt", "x")
	if _, err := h(ctx, contentURLInput{Bucket: "data", Key: "a.txt", Purpose: "stream"}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("unknown purpose = %v", err)
	}
	if _, err := h(ctx, contentURLInput{Bucket: "data", Key: "a.txt", Purpose: "preview", Limit: -1}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("negative limit = %v", err)
	}
}

func TestBeginUpload(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "taken.txt", "x")
	deps := testDeps(t, newStores(tv))
	h := objectsBeginUploadHandler(deps)
	ctx := context.Background()

	out, err := h(ctx, beginUploadInput{Bucket: "data", Key: "new.csv", Size: 10, ContentType: "text/csv"}, principalFor("u"))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	tk := ticketFrom(t, deps, out.URL)
	if tk.Op != OpUpload || tk.Size != 10 || tk.ContentType != "text/csv" || tk.Overwrite {
		t.Fatalf("upload ticket = %+v", tk)
	}

	if _, err := h(ctx, beginUploadInput{Bucket: "data", Key: "taken.txt", Size: 1}, principalFor("u")); codeOf(err) != "CONFLICT" {
		t.Errorf("existing key without overwrite = %v", err)
	}
	if _, err := h(ctx, beginUploadInput{Bucket: "data", Key: "taken.txt", Size: 1, Overwrite: true}, principalFor("u")); err != nil {
		t.Errorf("existing key with overwrite = %v", err)
	}
	if _, err := h(ctx, beginUploadInput{Bucket: "data", Key: "big.bin", Size: deps.Content.MaxUploadBytes + 1}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("over the cap = %v", err)
	}
	if _, err := h(ctx, beginUploadInput{Bucket: "data", Key: "neg.bin", Size: -1}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("negative size = %v", err)
	}
	if _, err := h(ctx, beginUploadInput{Bucket: "data", Size: 1}, principalFor("u")); codeOf(err) != "BAD_REQUEST" {
		t.Errorf("no key = %v", err)
	}
}

func TestBeginUpload_RefusesTheCASBucket(t *testing.T) {
	tv := openMem(t, trove.WithCAS(cas.AlgSHA256))
	mustBucket(t, tv, "cas")
	_, err := objectsBeginUploadHandler(testDeps(t, newStores(tv)))(context.Background(),
		beginUploadInput{Bucket: "cas", Key: "sha256:abc", Size: 1}, principalFor("u"))
	if codeOf(err) != "CONFLICT" {
		t.Fatalf("upload into the CAS bucket = %v", err)
	}
}

func TestCompleteUpload(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	h := objectsCompleteUploadHandler(deps)
	if _, err := h(context.Background(), objectKeyInput{Bucket: "data", Key: "late.txt"}, principalFor("u")); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("complete before the bytes landed = %v", err)
	}
	put(t, tv, "data", "late.txt", strings.Repeat("z", 5))
	out, err := h(context.Background(), objectKeyInput{Bucket: "data", Key: "late.txt"}, principalFor("u"))
	if err != nil || out.Key != "late.txt" || out.StoredSize != 5 {
		t.Fatalf("complete = %+v, %v", out, err)
	}
}
