package contract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/go-utils/log"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/middleware"
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

// failingRead is a read middleware whose reader yields n bytes and then
// fails, which is how a corrupt compressed or encrypted object looks.
type failingRead struct{ n int }

func (failingRead) Name() string                    { return "failing-read" }
func (failingRead) Direction() middleware.Direction { return middleware.DirectionRead }

func (f failingRead) WrapReader(_ context.Context, r io.ReadCloser, _ *driver.ObjectInfo) (io.ReadCloser, error) {
	return &failAfter{ReadCloser: r, left: f.n}, nil
}

type failAfter struct {
	io.ReadCloser
	left int
}

func (f *failAfter) Read(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, errors.New("decrypt: message authentication failed")
	}
	if len(p) > f.left {
		p = p[:f.left]
	}
	for i := range p {
		p[i] = 'x'
	}
	f.left -= len(p)
	return len(p), nil
}

func TestContentGet_MidStreamFailureAbortsTheResponse(t *testing.T) {
	tv := openMem(t, trove.WithMiddleware(failingRead{n: 128 << 10}))
	mustBucket(t, tv, "data")
	put(t, tv, "data", "big.bin", "stored bytes")
	deps := testDeps(t, newStores(tv))
	logger := log.NewTestLogger()
	srv := httptest.NewServer(deps.Content.Handler(deps.Stores, logger))
	t.Cleanup(srv.Close)

	for _, op := range []string{OpDownload, OpPreview} {
		tk := Ticket{Store: SingleStoreName, Bucket: "data", Key: "big.bin", Op: op, Subject: "user_9", Limit: 1 << 20}
		tok, _, err := deps.Content.Signer.Issue(tk, time.Minute)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+deps.Content.URL(tok), nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		if err == nil {
			t.Fatalf("%s: a download that failed on the storage side read as complete", op)
		}
	}

	tl := logger.(*log.TestLogger)
	entries := tl.GetLogsByLevel("ERROR")
	if len(entries) == 0 {
		t.Fatal("the failure was not logged at error level")
	}
	for _, name := range []string{"subject", "op", "store", "bucket", "key"} {
		if _, ok := entries[0].Field(name); !ok {
			t.Errorf("error entry has no %q field", name)
		}
	}
	if v, _ := entries[0].Field("subject"); v != "user_9" {
		t.Errorf("subject = %v", v)
	}
}

func TestContentPut_LogsWhoUploadedWhat(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	logger := log.NewTestLogger()
	deps.Logger = logger
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "up.txt", Op: OpUpload, Size: 3, Subject: "user_3"}, time.Minute)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, link, strings.NewReader("abc"))
	rec := httptest.NewRecorder()
	deps.Content.Handler(deps.Stores, logger).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s", rec.Code, rec.Body)
	}
	entries := logger.(*log.TestLogger).GetLogsByLevel("INFO")
	if len(entries) != 1 {
		t.Fatalf("info entries = %d, want 1", len(entries))
	}
	want := map[string]any{"subject": "user_3", "op": OpUpload, "store": SingleStoreName, "bucket": "data", "key": "up.txt", "storedSize": int64(3)}
	for k, v := range want {
		if got, ok := entries[0].Field(k); !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
}

func TestContentPut_OverwriteTicketIntoMissingBucketIs404(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		deps := testDeps(t, newStores(tv))
		link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "typo", Key: "a.txt", Op: OpUpload, Size: 3, Overwrite: true}, time.Minute)
		if rec := serve(t, deps, http.MethodPut, link, strings.NewReader("abc"), 3); rec.Code != http.StatusNotFound {
			t.Fatalf("put into a missing bucket = %d %s", rec.Code, rec.Body)
		}
	})
}

func TestContentPut_OverwriteDoesNotCreateABucketDirectory(t *testing.T) {
	root := t.TempDir()
	drv := localdriver.New()
	if err := drv.Open(context.Background(), "file://"+root); err != nil {
		t.Fatalf("open localdriver: %v", err)
	}
	tv := openTrove(t, drv)
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "typo", Key: "a.txt", Op: OpUpload, Size: 3, Overwrite: true}, time.Minute)
	if rec := serve(t, deps, http.MethodPut, link, strings.NewReader("abc"), 3); rec.Code != http.StatusNotFound {
		t.Fatalf("put = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "typo")); !os.IsNotExist(err) {
		t.Fatalf("the typo directory exists (stat err %v)", err)
	}
}
