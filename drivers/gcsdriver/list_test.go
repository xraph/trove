package gcsdriver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

const gcsPageOne = `{"kind":"storage#objects","prefixes":["logs/"],"items":[{"kind":"storage#object","name":"a.txt","bucket":"data","size":"3","etag":"e1","contentType":"text/plain","updated":"2026-09-30T00:00:00Z"}],"nextPageToken":"opaque-token-2"}`

const gcsPageTwo = `{"kind":"storage#objects","items":[{"kind":"storage#object","name":"z.txt","bucket":"data","size":"5","etag":"e2","contentType":"text/plain","updated":"2026-09-30T00:00:00Z"}]}`

func TestList_PagesWithPageTokenAndReportsPrefixes(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/b/data/o") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "opaque-token-2" {
			fmt.Fprint(w, gcsPageTwo)
			return
		}
		fmt.Fprint(w, gcsPageOne)
	}))
	t.Cleanup(srv.Close)

	drv := New()
	ctx := context.Background()
	require.NoError(t, drv.Open(ctx, "gcs://test-project/data?endpoint="+url.QueryEscape(srv.URL+"/storage/v1/")))

	it, err := drv.List(ctx, "data", driver.WithDelimiter("/"), driver.WithMaxKeys(2))
	require.NoError(t, err)
	objects, err := it.All(ctx)
	require.NoError(t, err)
	require.Len(t, objects, 1, "a prefix must not arrive as an empty-key object")
	assert.Equal(t, "a.txt", objects[0].Key)
	assert.Equal(t, []string{"logs/"}, it.CommonPrefixes())
	assert.Equal(t, "opaque-token-2", it.NextToken())

	it2, err := drv.List(ctx, "data", driver.WithDelimiter("/"), driver.WithMaxKeys(2), driver.WithCursor(it.NextToken()))
	require.NoError(t, err)
	objects2, err := it2.All(ctx)
	require.NoError(t, err)
	require.Len(t, objects2, 1)
	assert.Equal(t, "z.txt", objects2[0].Key)
	assert.Empty(t, it2.NextToken())

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, queries)
	assert.Equal(t, "/", queries[0].Get("delimiter"))
	assert.Equal(t, "opaque-token-2", queries[len(queries)-1].Get("pageToken"))
}
