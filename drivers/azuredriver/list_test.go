package azuredriver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

const azPageOne = `<?xml version="1.0" encoding="utf-8"?><EnumerationResults ServiceEndpoint="http://127.0.0.1/" ContainerName="data"><Delimiter>/</Delimiter><MaxResults>2</MaxResults><Blobs><Blob><Name>a.txt</Name><Properties><Last-Modified>Wed, 30 Sep 2026 00:00:00 GMT</Last-Modified><Etag>0x8D1</Etag><Content-Length>3</Content-Length><Content-Type>text/plain</Content-Type><BlobType>BlockBlob</BlobType></Properties></Blob><BlobPrefix><Name>logs/</Name></BlobPrefix></Blobs><NextMarker>opaque-marker-2</NextMarker></EnumerationResults>`

const azPageTwo = `<?xml version="1.0" encoding="utf-8"?><EnumerationResults ServiceEndpoint="http://127.0.0.1/" ContainerName="data"><Delimiter>/</Delimiter><MaxResults>2</MaxResults><Blobs><Blob><Name>z.txt</Name><Properties><Last-Modified>Wed, 30 Sep 2026 00:00:00 GMT</Last-Modified><Etag>0x8D2</Etag><Content-Length>5</Content-Length><Content-Type>text/plain</Content-Type><BlobType>BlockBlob</BlobType></Properties></Blob></Blobs><NextMarker/></EnumerationResults>`

func TestList_PagesWithMarkerAndReportsPrefixes(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("marker") == "opaque-marker-2" {
			fmt.Fprint(w, azPageTwo)
			return
		}
		fmt.Fprint(w, azPageOne)
	}))
	t.Cleanup(srv.Close)

	drv := New()
	ctx := context.Background()
	require.NoError(t, drv.Open(ctx, "azure://devaccount/data?endpoint="+url.QueryEscape(srv.URL)))

	it, err := drv.List(ctx, "data", driver.WithDelimiter("/"), driver.WithMaxKeys(2))
	require.NoError(t, err)
	objects, err := it.All(ctx)
	require.NoError(t, err)
	require.Len(t, objects, 1)
	assert.Equal(t, "a.txt", objects[0].Key)
	assert.Equal(t, []string{"logs/"}, it.CommonPrefixes())
	assert.Equal(t, "opaque-marker-2", it.NextToken())

	it2, err := drv.List(ctx, "data", driver.WithDelimiter("/"), driver.WithMaxKeys(2), driver.WithCursor(it.NextToken()))
	require.NoError(t, err)
	objects2, err := it2.All(ctx)
	require.NoError(t, err)
	require.Len(t, objects2, 1)
	assert.Equal(t, "z.txt", objects2[0].Key)
	assert.Empty(t, it2.NextToken())

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, queries, 2)
	assert.Equal(t, "/", queries[0].Get("delimiter"))
	assert.Equal(t, "2", queries[0].Get("maxresults"))
	assert.Equal(t, "opaque-marker-2", queries[1].Get("marker"))
}
