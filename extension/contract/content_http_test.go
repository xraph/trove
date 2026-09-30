package contract

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xraph/trove"
	"github.com/xraph/trove/middleware/compress"
)

// serve runs one request against the content handler.
func serve(t *testing.T, deps Deps, method, link string, body io.Reader, contentLength int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, link, body)
	// Always set: -1 is how a test sends a body whose length is hidden,
	// which is how a client that lies about its size looks to the server.
	req.ContentLength = contentLength
	rec := httptest.NewRecorder()
	deps.Content.Handler(deps.Stores, nil).ServeHTTP(rec, req)
	return rec
}

func issue(t *testing.T, deps Deps, tk Ticket, ttl time.Duration) string {
	t.Helper()
	tok, _, err := deps.Content.Signer.Issue(tk, ttl)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return deps.Content.URL(tok)
}

func TestContentGet_DownloadHeaders(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		mustBucket(t, tv, "data")
		put(t, tv, "data", "docs/page.html", "<script>alert(1)</script>")
		deps := testDeps(t, newStores(tv))
		link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "docs/page.html", Op: OpDownload}, time.Minute)
		rec := serve(t, deps, http.MethodGet, link, nil, -1)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		h := rec.Header()
		if got := h.Get("Content-Disposition"); got != "attachment; filename*=UTF-8''page.html" {
			t.Errorf("Content-Disposition = %q", got)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != "sandbox" || h.Get("Cache-Control") != "no-store" {
			t.Errorf("headers = %v", h)
		}
		if h.Get("Content-Length") != "25" {
			t.Errorf("Content-Length = %q, want 25 with no middleware", h.Get("Content-Length"))
		}
		if rec.Body.String() != "<script>alert(1)</script>" {
			t.Errorf("body = %q", rec.Body)
		}
	})
}

func TestContentGet_KeyWithAwkwardCharacters(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	key := "reports/q3 résumé #1 ?x=%20.txt"
	put(t, tv, "data", key, "bytes")
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: key, Op: OpDownload}, time.Minute)
	rec := serve(t, deps, http.MethodGet, link, nil, -1)
	if rec.Code != http.StatusOK || rec.Body.String() != "bytes" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
	want := "attachment; filename*=UTF-8''" + url.PathEscape("q3 résumé #1 ?x=%20.txt")
	if got := rec.Header().Get("Content-Disposition"); got != want {
		t.Fatalf("Content-Disposition = %q, want %q", got, want)
	}
}

func TestContentGet_CompressedRoundTripOmitsLength(t *testing.T) {
	tv := openMem(t, trove.WithMiddleware(compress.New()))
	mustBucket(t, tv, "data")
	body := strings.Repeat("hello ", 400)
	put(t, tv, "data", "big.txt", body)
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "big.txt", Op: OpDownload}, time.Minute)
	rec := serve(t, deps, http.MethodGet, link, nil, -1)
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("status %d, body length %d, want the original %d bytes", rec.Code, rec.Body.Len(), len(body))
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatalf("Content-Length = %q with read middleware; the stored size is not what is served", rec.Header().Get("Content-Length"))
	}
}

func TestContentGet_PreviewStopsAtTheLimit(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "0123456789")
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "a.txt", Op: OpPreview, Limit: 4}, time.Minute)
	rec := serve(t, deps, http.MethodGet, link, nil, -1)
	if rec.Code != http.StatusOK || rec.Body.String() != "0123" {
		t.Fatalf("preview = %d %q", rec.Code, rec.Body)
	}
}

func TestContentGet_RefusesBadTickets(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	deps := testDeps(t, newStores(tv))

	expired := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "a.txt", Op: OpDownload}, -time.Second)
	if rec := serve(t, deps, http.MethodGet, expired, nil, -1); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired = %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, deps, http.MethodGet, deps.Content.Path+"?t=garbage", nil, -1); rec.Code != http.StatusForbidden {
		t.Errorf("garbage = %d", rec.Code)
	}
	upload := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "a.txt", Op: OpUpload, Size: 1}, time.Minute)
	if rec := serve(t, deps, http.MethodGet, upload, nil, -1); rec.Code != http.StatusForbidden {
		t.Errorf("upload ticket used for GET = %d", rec.Code)
	}
	missing := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "gone.txt", Op: OpDownload}, time.Minute)
	if rec := serve(t, deps, http.MethodGet, missing, nil, -1); rec.Code != http.StatusNotFound {
		t.Errorf("missing object = %d", rec.Code)
	}
	if rec := serve(t, deps, http.MethodDelete, missing, nil, -1); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d", rec.Code)
	}
}

func TestContentPut_StoresThroughMiddleware(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t, trove.WithMiddleware(compress.New()))
		mustBucket(t, tv, "data")
		deps := testDeps(t, newStores(tv))
		body := strings.Repeat("abc ", 1000)
		link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "up.txt", Op: OpUpload,
			Size: int64(len(body)), ContentType: "text/plain"}, time.Minute)
		rec := serve(t, deps, http.MethodPut, link, strings.NewReader(body), int64(len(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("put = %d %s", rec.Code, rec.Body)
		}
		obj, err := tv.Get(context.Background(), "data", "up.txt")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer obj.Close()
		got, _ := io.ReadAll(obj)
		if string(got) != body {
			t.Fatal("read back differs from the upload")
		}
		info, _ := tv.Head(context.Background(), "data", "up.txt")
		if info.Size >= int64(len(body)) || info.ContentType != "text/plain" {
			t.Fatalf("stored = %+v, want compressed and text/plain", info)
		}
	})
}

func TestContentPut_BodyLargerThanTicketIsRefused(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "lie.bin", Op: OpUpload, Size: 4}, time.Minute)

	if rec := serve(t, deps, http.MethodPut, link, bytes.NewReader(make([]byte, 64)), 64); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("declared 64 against a 4-byte ticket = %d", rec.Code)
	}
	// A client that hides the length: the body itself must be capped.
	if rec := serve(t, deps, http.MethodPut, link, bytes.NewReader(make([]byte, 64)), -1); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("undeclared 64 against a 4-byte ticket = %d", rec.Code)
	}
	if _, err := tv.Head(context.Background(), "data", "lie.bin"); err == nil {
		t.Fatal("an oversized upload was stored")
	}
}

func TestContentPut_RefusesADownloadTicket(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	for _, op := range []string{OpDownload, OpPreview} {
		link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "sneak.txt", Op: op, Size: 3, Limit: 3}, time.Minute)
		if rec := serve(t, deps, http.MethodPut, link, strings.NewReader("new"), 3); rec.Code != http.StatusForbidden {
			t.Errorf("%s ticket used for PUT = %d", op, rec.Code)
		}
	}
	if _, err := tv.Head(context.Background(), "data", "sneak.txt"); err == nil {
		t.Fatal("a download ticket stored an object")
	}
}

func TestContentPut_OverwriteIsCheckedAgain(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "race.txt", Op: OpUpload, Size: 3}, time.Minute)
	put(t, tv, "data", "race.txt", "old")
	if rec := serve(t, deps, http.MethodPut, link, strings.NewReader("new"), 3); rec.Code != http.StatusConflict {
		t.Fatalf("put onto a key that appeared after begin = %d", rec.Code)
	}
}
