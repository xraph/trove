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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/go-utils/log"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/middleware"
	"github.com/xraph/trove/middleware/compress"
	"github.com/xraph/trove/middleware/scan"
)

// serve runs one bodyless request against the content handler.
func serve(t *testing.T, deps Deps, method, link string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, link, nil)
	rec := httptest.NewRecorder()
	deps.Content.Handler(deps.Stores, nil).ServeHTTP(rec, req)
	return rec
}

// issueToken mints a ticket and returns the bare token.
func issueToken(t *testing.T, deps Deps, tk Ticket, ttl time.Duration) string {
	t.Helper()
	tok, _, err := deps.Content.Signer.Issue(tk, ttl)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return tok
}

// issue mints a ticket and returns the download link that carries it.
func issue(t *testing.T, deps Deps, tk Ticket, ttl time.Duration) string {
	t.Helper()
	return deps.Content.URL(issueToken(t, deps, tk, ttl))
}

// serveUpload PUTs body to the bare content path with the ticket in the
// X-Trove-Ticket header, which is the only place an upload ticket goes.
func serveUpload(t *testing.T, deps Deps, logger forge.Logger, token string, body io.Reader, contentLength int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, deps.Content.Path, body)
	// Always set: -1 is how a test sends a body whose length is hidden,
	// which is how a client that lies about its size looks to the server.
	req.ContentLength = contentLength
	req.Header.Set(TicketHeader, token)
	rec := httptest.NewRecorder()
	deps.Content.Handler(deps.Stores, logger).ServeHTTP(rec, req)
	return rec
}

// uploadTicket mints an upload ticket and returns the bare token.
func uploadTicket(t *testing.T, deps Deps, tk Ticket) string {
	t.Helper()
	tk.Op = OpUpload
	return issueToken(t, deps, tk, time.Minute)
}

func TestContentGet_DownloadHeaders(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		mustBucket(t, tv, "data")
		put(t, tv, "data", "docs/page.html", "<script>alert(1)</script>")
		deps := testDeps(t, newStores(tv))
		link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "docs/page.html", Op: OpDownload}, time.Minute)
		rec := serve(t, deps, http.MethodGet, link)
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
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t)
		mustBucket(t, tv, "data")
		key := "reports/q3 résumé #1 ?x=%20.txt"
		put(t, tv, "data", key, "bytes")
		deps := testDeps(t, newStores(tv))
		link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: key, Op: OpDownload}, time.Minute)
		rec := serve(t, deps, http.MethodGet, link)
		if rec.Code != http.StatusOK || rec.Body.String() != "bytes" {
			t.Fatalf("status %d body %q", rec.Code, rec.Body)
		}
		want := "attachment; filename*=UTF-8''" + url.PathEscape("q3 résumé #1 ?x=%20.txt")
		if got := rec.Header().Get("Content-Disposition"); got != want {
			t.Fatalf("Content-Disposition = %q, want %q", got, want)
		}
	})
}

func TestContentGet_CompressedRoundTripOmitsLength(t *testing.T) {
	tv := openMem(t, trove.WithMiddleware(compress.New()))
	mustBucket(t, tv, "data")
	body := strings.Repeat("hello ", 400)
	put(t, tv, "data", "big.txt", body)
	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "big.txt", Op: OpDownload}, time.Minute)
	rec := serve(t, deps, http.MethodGet, link)
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
	rec := serve(t, deps, http.MethodGet, link)
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
	if rec := serve(t, deps, http.MethodGet, expired); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired = %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, deps, http.MethodGet, deps.Content.Path+"?t=garbage"); rec.Code != http.StatusForbidden {
		t.Errorf("garbage = %d", rec.Code)
	}
	upload := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "a.txt", Op: OpUpload, Size: 1}, time.Minute)
	if rec := serve(t, deps, http.MethodGet, upload); rec.Code != http.StatusForbidden {
		t.Errorf("upload ticket used for GET = %d", rec.Code)
	}
	missing := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "gone.txt", Op: OpDownload}, time.Minute)
	if rec := serve(t, deps, http.MethodGet, missing); rec.Code != http.StatusNotFound {
		t.Errorf("missing object = %d", rec.Code)
	}
	if rec := serve(t, deps, http.MethodDelete, missing); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d", rec.Code)
	}
}

func TestContentPut_StoresThroughMiddleware(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t, trove.WithMiddleware(compress.New()))
		mustBucket(t, tv, "data")
		deps := testDeps(t, newStores(tv))
		body := strings.Repeat("abc ", 1000)
		tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "up.txt",
			Size: int64(len(body)), ContentType: "text/plain"})
		rec := serveUpload(t, deps, nil, tok, strings.NewReader(body), int64(len(body)))
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

// TestContentPut_BodyLargerThanTicketIsRefused runs with and without a
// write middleware: with compress registered the body reaches the driver
// through a pipe, and the cap's error has to survive that trip.
func TestContentPut_BodyLargerThanTicketIsRefused(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		for _, mw := range []struct {
			name string
			opts []trove.Option
		}{{"plain", nil}, {"compress", []trove.Option{trove.WithMiddleware(compress.New())}}} {
			t.Run(mw.name, func(t *testing.T) {
				tv := open(t, mw.opts...)
				mustBucket(t, tv, "data")
				deps := testDeps(t, newStores(tv))
				tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "lie.bin", Size: 4})

				if rec := serveUpload(t, deps, nil, tok, bytes.NewReader(make([]byte, 64)), 64); rec.Code != http.StatusRequestEntityTooLarge {
					t.Errorf("declared 64 against a 4-byte ticket = %d %s", rec.Code, rec.Body)
				}
				// A client that hides the length: the body itself must be capped.
				if rec := serveUpload(t, deps, nil, tok, bytes.NewReader(make([]byte, 64)), -1); rec.Code != http.StatusRequestEntityTooLarge {
					t.Errorf("undeclared 64 against a 4-byte ticket = %d %s", rec.Code, rec.Body)
				}
				if _, err := tv.Head(context.Background(), "data", "lie.bin"); err == nil {
					t.Fatal("an oversized upload was stored")
				}
			})
		}
	})
}

func TestContentPut_RefusesADownloadTicket(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	for _, op := range []string{OpDownload, OpPreview} {
		tok := issueToken(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "sneak.txt", Op: op, Size: 3, Limit: 3}, time.Minute)
		if rec := serveUpload(t, deps, nil, tok, strings.NewReader("new"), 3); rec.Code != http.StatusForbidden {
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
	tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "race.txt", Size: 3})
	put(t, tv, "data", "race.txt", "old")
	if rec := serveUpload(t, deps, nil, tok, strings.NewReader("new"), 3); rec.Code != http.StatusConflict {
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
	tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "up.txt", Size: 3, Subject: "user_3"})
	rec := serveUpload(t, deps, logger, tok, strings.NewReader("abc"), 3)
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
		tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "typo", Key: "a.txt", Size: 3, Overwrite: true})
		if rec := serveUpload(t, deps, nil, tok, strings.NewReader("abc"), 3); rec.Code != http.StatusNotFound {
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
	tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "typo", Key: "a.txt", Size: 3, Overwrite: true})
	if rec := serveUpload(t, deps, nil, tok, strings.NewReader("abc"), 3); rec.Code != http.StatusNotFound {
		t.Fatalf("put = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "typo")); !os.IsNotExist(err) {
		t.Fatalf("the typo directory exists (stat err %v)", err)
	}
}

// TestContentPut_TicketOnlyFromTheHeader keeps upload tickets out of URLs:
// forge's tracing records the query string, and an upload ticket lives 15
// minutes and may allow an overwrite.
func TestContentPut_TicketOnlyFromTheHeader(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	deps := testDeps(t, newStores(tv))
	tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "up.txt", Size: 3})
	handler := deps.Content.Handler(deps.Stores, nil)

	for _, c := range []struct {
		name   string
		target string
		header string
	}{
		{"query only", deps.Content.URL(tok), ""},
		{"no ticket", deps.Content.Path, ""},
		{"header and query", deps.Content.URL(tok), tok},
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, c.target, strings.NewReader("abc"))
		if c.header != "" {
			req.Header.Set(TicketHeader, c.header)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "X-Trove-Ticket") {
			t.Errorf("%s: PUT = %d %s, want 403 naming the X-Trove-Ticket header", c.name, rec.Code, rec.Body)
		}
	}
	if _, err := tv.Head(context.Background(), "data", "up.txt"); err == nil {
		t.Fatal("a PUT with its ticket in the URL stored an object")
	}
	if rec := serveUpload(t, deps, nil, tok, strings.NewReader("abc"), 3); rec.Code != http.StatusOK {
		t.Fatalf("PUT with the ticket in the header = %d %s", rec.Code, rec.Body)
	}
}

func TestContentGet_LogsWhoReadWhat(t *testing.T) {
	tv := openMem(t)
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "hello")
	deps := testDeps(t, newStores(tv))
	logger := log.NewTestLogger()
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "a.txt", Op: OpDownload, Subject: "user_7"}, time.Minute)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, link, nil)
	rec := httptest.NewRecorder()
	deps.Content.Handler(deps.Stores, logger).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	entries := logger.(*log.TestLogger).GetLogsByLevel("INFO")
	if len(entries) != 1 {
		t.Fatalf("info entries = %d, want 1", len(entries))
	}
	want := map[string]any{"subject": "user_7", "op": OpDownload, "store": SingleStoreName, "bucket": "data", "key": "a.txt", "bytes": int64(5)}
	for k, v := range want {
		if got, ok := entries[0].Field(k); !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
}

// ctxFlag marks a request context for flagScope.
type ctxFlag struct{}

// appendRead is a read middleware that adds one byte to what it serves,
// so the stored size is no longer the served size.
type appendRead struct{}

func (appendRead) Name() string                    { return "append-read" }
func (appendRead) Direction() middleware.Direction { return middleware.DirectionRead }

func (appendRead) WrapReader(_ context.Context, r io.ReadCloser, _ *driver.ObjectInfo) (io.ReadCloser, error) {
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(r, strings.NewReader("!")), r}, nil
}

// TestContentGet_LengthFollowsThePipelineGetUses registers a read
// middleware whose scope depends on the request context. The resolver
// caches a key's pipeline, so once a flagged request has resolved it, Get
// runs the middleware for every later request too. Content-Length must
// follow that pipeline, not a fresh evaluation of the scopes.
func TestContentGet_LengthFollowsThePipelineGetUses(t *testing.T) {
	flagged := middleware.When(func(ctx context.Context, _, _ string) bool { return ctx.Value(ctxFlag{}) != nil })
	tv := openMem(t, trove.WithScopedMiddleware(flagged, appendRead{}))
	mustBucket(t, tv, "data")
	put(t, tv, "data", "a.txt", "x")
	tv.Resolver().ResolveRead(context.WithValue(context.Background(), ctxFlag{}, true), "data", "a.txt")

	deps := testDeps(t, newStores(tv))
	link := issue(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "a.txt", Op: OpDownload}, time.Minute)
	rec := serve(t, deps, http.MethodGet, link)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "" && cl != strconv.Itoa(rec.Body.Len()) {
		t.Fatalf("Content-Length = %s but %d bytes were served (%q)", cl, rec.Body.Len(), rec.Body)
	}
}

// blockAll is a scan provider that finds a threat in everything.
type blockAll struct{}

func (blockAll) Scan(context.Context, io.Reader) (*scan.Result, error) {
	return &scan.Result{Clean: false, Threat: "EICAR-Test-File"}, nil
}

func TestContentPut_ScanBlockIs422AndStoresNothing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, open opener) {
		tv := open(t, trove.WithMiddleware(scan.New(scan.WithProvider(blockAll{}))))
		mustBucket(t, tv, "data")
		deps := testDeps(t, newStores(tv))
		tok := uploadTicket(t, deps, Ticket{Store: SingleStoreName, Bucket: "data", Key: "eicar.txt", Size: 3})
		rec := serveUpload(t, deps, nil, tok, strings.NewReader("bad"), 3)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("blocked upload = %d %s, want 422", rec.Code, rec.Body)
		}
		if _, err := tv.Head(context.Background(), "data", "eicar.txt"); err == nil {
			t.Fatal("a blocked upload was stored")
		}
	})
}
