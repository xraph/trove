package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/localdriver"
)

// The HTTP surface is where a driver's error classification is finally spent.
// A rejection the driver classified as a malformed path must reach the client
// as 400, and an error the driver did not classify must not reach the client
// at all — os-level failures carry absolute server filesystem paths.
//
// Traversal is exercised through percent-encoded request targets because that
// is the only form that survives the mux: net/http cleans a literal "../" out
// of the path and answers 307, while "%2e%2e%2f" is matched escaped and then
// unescaped into the path value, arriving at the driver intact.

// newTestHandler returns a handler backed by a real local driver rooted at a
// temp dir, holding one bucket "data" with one object "present".
func newTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()

	root := t.TempDir()
	drv := localdriver.New()
	drv.SetRootDir(root)

	ctx := context.Background()
	if err := drv.CreateBucket(ctx, "data"); err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Put(ctx, "data", "present", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}

	tv, err := trove.Open(drv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tv.Close(ctx) })

	return New(tv, nil, nil), root
}

func TestObjectHandler_StatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		want   int
	}{
		// Malformed paths are rejected by the driver's containment guard.
		// The request is unanswerable, not the resource missing, so these are
		// 400 — answering 404 would both misdescribe the failure and let a
		// client use rejected keys to probe which objects exist.
		{"put traversal key", http.MethodPut, "/buckets/data/objects/%2e%2e%2f%2e%2e%2fetc%2fpasswd", http.StatusBadRequest},
		{"put cross-bucket key", http.MethodPut, "/buckets/data/objects/%2e%2e%2fother%2fsecret", http.StatusBadRequest},
		{"put traversal bucket", http.MethodPut, "/buckets/%2e%2e%2fevil/objects/x.txt", http.StatusBadRequest},
		{"put absolute key", http.MethodPut, "/buckets/data/objects/%2fetc%2fpasswd", http.StatusBadRequest},
		{"get traversal key", http.MethodGet, "/buckets/data/objects/%2e%2e%2f%2e%2e%2fetc%2fpasswd", http.StatusBadRequest},
		{"delete traversal key", http.MethodDelete, "/buckets/data/objects/%2e%2e%2f%2e%2e%2fetc%2fpasswd", http.StatusBadRequest},
		{"head traversal key", http.MethodHead, "/buckets/data/objects/%2e%2e%2f%2e%2e%2fetc%2fpasswd", http.StatusBadRequest},
		{"list traversal bucket", http.MethodGet, "/buckets/%2e%2e%2fevil/list", http.StatusBadRequest},

		// A well-formed path naming something that is not there is 404, on
		// every method — including the ones that answered 500 before.
		{"get missing object", http.MethodGet, "/buckets/data/objects/absent", http.StatusNotFound},
		{"get missing bucket", http.MethodGet, "/buckets/nosuch/objects/present", http.StatusNotFound},
		{"head missing object", http.MethodHead, "/buckets/data/objects/absent", http.StatusNotFound},
		// Delete is idempotent in the driver — removing something that is
		// already gone is a success, as it is in S3. Pinned here because the
		// status table would otherwise read as if it were an oversight.
		{"delete missing object", http.MethodDelete, "/buckets/data/objects/absent", http.StatusNoContent},
		{"list missing bucket", http.MethodGet, "/buckets/nosuch/list", http.StatusNotFound},

		// The control: the happy paths must survive the new mapping.
		{"get present object", http.MethodGet, "/buckets/data/objects/present", http.StatusOK},
		{"head present object", http.MethodHead, "/buckets/data/objects/present", http.StatusOK},
		{"list existing bucket", http.MethodGet, "/buckets/data/list", http.StatusOK},
		{"put valid object", http.MethodPut, "/buckets/data/objects/new.txt", http.StatusCreated},
		{"put nested object", http.MethodPut, "/buckets/data/objects/a/b/c.txt", http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestHandler(t)

			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader("body"))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Errorf("%s %s = %d, want %d (body: %s)",
					tt.method, tt.target, rec.Code, tt.want, strings.TrimSpace(rec.Body.String()))
			}
		})
	}
}

// TestBucketHandler_StatusMapping covers the sibling routes. A bucket name is
// a path segment in the same driver guard, so DELETE /buckets/{bucket} is
// reachable with the same encoded traversal as the object routes.
func TestBucketHandler_StatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		body   string
		want   int
	}{
		{"delete traversal bucket", http.MethodDelete, "/buckets/%2e%2e%2fevil", "", http.StatusBadRequest},
		{"delete bucket naming the root", http.MethodDelete, "/buckets/%2e", "", http.StatusBadRequest},
		{"create traversal bucket", http.MethodPost, "/buckets", `{"name":"../evil"}`, http.StatusBadRequest},
		{"create absolute bucket", http.MethodPost, "/buckets", `{"name":"/tmp"}`, http.StatusBadRequest},

		// Controls.
		{"create valid bucket", http.MethodPost, "/buckets", `{"name":"fresh"}`, http.StatusCreated},
		{"delete existing bucket", http.MethodDelete, "/buckets/data", "", http.StatusNoContent},
		{"create without a name", http.MethodPost, "/buckets", `{}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestHandler(t)

			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Errorf("%s %s = %d, want %d (body: %s)",
					tt.method, tt.target, rec.Code, tt.want, strings.TrimSpace(rec.Body.String()))
			}
		})
	}
}

// TestObjectHandler_UnclassifiedErrorIsOpaque is the leak test. An os-level
// failure the driver did not classify carries an absolute server path in its
// message; that message must stay on the server.
func TestObjectHandler_UnclassifiedErrorIsOpaque(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: the read-only directory below would not deny the write")
	}

	h, root := newTestHandler(t)

	// Make the bucket unwritable so Put fails inside os.MkdirAll, which
	// reports the absolute path it could not create. The driver does not
	// classify this, so it is exactly the case that used to be echoed.
	bucketDir := filepath.Join(root, "data")
	if err := os.Chmod(bucketDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bucketDir, 0o750) })

	req := httptest.NewRequest(http.MethodPut, "/buckets/data/objects/nested/obj.txt", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body: %s)",
			rec.Code, http.StatusInternalServerError, strings.TrimSpace(rec.Body.String()))
	}

	body := rec.Body.String()
	if strings.Contains(body, root) {
		t.Errorf("500 response leaked the server filesystem root %q:\n%s", root, body)
	}
	// The temp root is the giveaway, but assert on the shape too: nothing from
	// the error chain should reach the client on an unclassified failure.
	for _, leak := range []string{"localdriver", "mkdir", "permission denied", string(filepath.Separator) + "var", "/tmp"} {
		if strings.Contains(body, leak) {
			t.Errorf("500 response leaked %q from the error chain:\n%s", leak, body)
		}
	}
}

// TestObjectHandler_ClassifiedErrorKeepsItsMessage is the other half of the
// disclosure rule: a classified rejection stays diagnosable. Without this, the
// leak test above would pass just as well against a handler that replaced
// every message with a constant, and a client could not tell which key it got
// wrong.
func TestObjectHandler_ClassifiedErrorKeepsItsMessage(t *testing.T) {
	h, root := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/buckets/data/objects/%2e%2e%2f%2e%2e%2fetc%2fpasswd", strings.NewReader("x"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	msg := got["error"]
	if !strings.Contains(msg, "../../etc/passwd") {
		t.Errorf("400 response does not name the rejected segment: %q", msg)
	}
	// Classified messages are published, so the driver contract that they
	// carry no server-side path is enforced here as well as at the driver.
	if strings.Contains(msg, root) {
		t.Errorf("400 response leaked the server filesystem root %q: %q", root, msg)
	}
}

// TestNoRawErrorTextInResponses asserts the wiring rather than the mapping.
// The handlers' failure paths are many and mostly need a broken backend to
// reach, so nothing else here would notice a new handler — or a future edit to
// an existing one — that went back to putting err.Error() straight into the
// response. That is the regression this change exists to fix, and it is cheap
// to detect at the source level.
//
// The rule: an error's own text reaches a client only through classify, which
// publishes it when a driver has classified the error and logs it otherwise.
func TestNoRawErrorTextInResponses(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	// writeError with an error's text as the message. classify is the only
	// sanctioned way for driver text to reach a response body.
	raw := regexp.MustCompile(`writeError\([^)]*(err\.Error\(\)|%v", err|%s", err|%w", err)`)

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		src, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if matches := raw.FindAllString(string(src), -1); len(matches) > 0 {
			t.Errorf("%s writes raw error text into a response: %q\n"+
				"route it through h.writeOpError so an unclassified error is logged, not published",
				name, matches)
		}
	}
}

// TestObjectHandler_GetStreamsBody guards the control case the status table
// cannot see: a 200 must still carry the object.
func TestObjectHandler_GetStreamsBody(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/buckets/data/objects/present", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}
}
