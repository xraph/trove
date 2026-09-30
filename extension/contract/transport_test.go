package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// TestCommandInvalidatesReachTheClient sends real commands through forge's
// HTTP transport and checks the response carries the manifest's
// invalidates. The React shell refreshes through that and nothing else;
// forge v1.10.0 dropped it.
func TestCommandInvalidatesReachTheClient(t *testing.T) {
	tests := []struct {
		intent  string
		payload string
	}{
		{"buckets.create", `{"name":"fresh"}`},
		{"objects.delete", `{"bucket":"data","key":"a.txt"}`},
		{"objects.completeUpload", `{"bucket":"data","key":"b.txt"}`},
	}
	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			tv := openMem(t)
			mustBucket(t, tv, "data")
			put(t, tv, "data", "a.txt", "x")
			put(t, tv, "data", "b.txt", "y")

			reg := dashcontract.NewRegistry()
			wreg := dashcontract.NewWardenRegistry()
			d := dispatcher.New(nil)
			if err := Register(d, reg, wreg, testDeps(t, newStores(tv))); err != nil {
				t.Fatalf("Register: %v", err)
			}
			var want []string
			for _, in := range loadManifest(t).Intents {
				if in.Name == tt.intent {
					want = in.Invalidates
				}
			}

			body := `{"envelope":"v1","kind":"command","contributor":"trove","intent":"` + tt.intent + `",` +
				`"csrf":"test","idempotencyKey":"test-` + tt.intent + `","payload":` + tt.payload + `}`
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/dashboard/v1", strings.NewReader(body))
			rec := httptest.NewRecorder()
			transport.NewHandler(reg, wreg, d, nil).ServeHTTP(rec, req)

			var resp dashcontract.Response
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v (%s)", err, rec.Body)
			}
			if !resp.OK {
				t.Fatalf("%s failed: %s", tt.intent, rec.Body)
			}
			if !reflect.DeepEqual(resp.Meta.Invalidates, want) {
				t.Fatalf("meta.invalidates = %v, want %v", resp.Meta.Invalidates, want)
			}
		})
	}
}
