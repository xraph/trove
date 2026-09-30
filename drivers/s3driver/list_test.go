package s3driver

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

const s3PageOne = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>data</Name><Prefix></Prefix><KeyCount>2</KeyCount><MaxKeys>2</MaxKeys><Delimiter>/</Delimiter><IsTruncated>true</IsTruncated><NextContinuationToken>opaque-token-2</NextContinuationToken><Contents><Key>a.txt</Key><LastModified>2026-09-30T00:00:00.000Z</LastModified><ETag>&quot;e1&quot;</ETag><Size>3</Size><StorageClass>STANDARD</StorageClass></Contents><CommonPrefixes><Prefix>logs/</Prefix></CommonPrefixes></ListBucketResult>`

const s3PageTwo = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>data</Name><Prefix></Prefix><KeyCount>1</KeyCount><MaxKeys>2</MaxKeys><Delimiter>/</Delimiter><IsTruncated>false</IsTruncated><Contents><Key>z.txt</Key><LastModified>2026-09-30T00:00:00.000Z</LastModified><ETag>&quot;e2&quot;</ETag><Size>5</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`

// fakeS3 answers ListObjectsV2 with page one, then page two once the
// request carries page one's continuation token, and records every query.
func fakeS3(t *testing.T) (*S3Driver, func() []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var queries []url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("continuation-token") == "opaque-token-2" {
			fmt.Fprint(w, s3PageTwo)
			return
		}
		fmt.Fprint(w, s3PageOne)
	}))
	t.Cleanup(srv.Close)

	drv := New()
	dsn := "s3://AKIDTEST:SECRETTEST@us-east-1/data?endpoint=" + url.QueryEscape(srv.URL) + "&path_style=true"
	require.NoError(t, drv.Open(context.Background(), dsn))

	return drv, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values{}, queries...)
	}
}

func TestList_PagesWithContinuationTokenAndReportsPrefixes(t *testing.T) {
	drv, queries := fakeS3(t)
	ctx := context.Background()

	it, err := drv.List(ctx, "data", driver.WithDelimiter("/"), driver.WithMaxKeys(2))
	require.NoError(t, err)
	objects, err := it.All(ctx)
	require.NoError(t, err)
	require.Len(t, objects, 1)
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

	q := queries()
	require.Len(t, q, 2)
	assert.Equal(t, "/", q[0].Get("delimiter"))
	assert.Equal(t, "opaque-token-2", q[1].Get("continuation-token"))
	assert.Empty(t, q[1].Get("start-after"), "the cursor is a token, never a key")
}

func TestList_MaxKeysZeroUsesDefault(t *testing.T) {
	drv, queries := fakeS3(t)

	_, err := drv.List(context.Background(), "data", driver.WithMaxKeys(0))
	require.NoError(t, err)

	q := queries()
	require.Len(t, q, 1)
	assert.Equal(t, "1000", q[0].Get("max-keys"))
}
