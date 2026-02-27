package trove_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
)

func TestOpen(t *testing.T) {
	t.Run("with valid driver", func(t *testing.T) {
		tr, err := trove.Open(memdriver.New())
		require.NoError(t, err)
		require.NotNil(t, tr)
	})

	t.Run("with nil driver", func(t *testing.T) {
		_, err := trove.Open(nil)
		assert.ErrorIs(t, err, trove.ErrNilDriver)
	})

	t.Run("with options", func(t *testing.T) {
		tr, err := trove.Open(memdriver.New(),
			trove.WithDefaultBucket("default"),
			trove.WithChunkSize(4*1024*1024),
			trove.WithPoolSize(8),
			trove.WithChecksumAlgorithm(trove.Blake3),
			trove.WithStreamBufferSize(64*1024),
		)
		require.NoError(t, err)
		assert.Equal(t, "default", tr.Config().DefaultBucket)
		assert.Equal(t, int64(4*1024*1024), tr.Config().ChunkSize)
		assert.Equal(t, 8, tr.Config().PoolSize)
	})
}

func TestPutGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	drv := memdriver.New()

	tr, err := trove.Open(drv)
	require.NoError(t, err)
	defer tr.Close(ctx)

	require.NoError(t, tr.CreateBucket(ctx, "test"))

	// Put
	data := "hello world"
	info, err := tr.Put(ctx, "test", "greeting.txt", strings.NewReader(data),
		driver.WithContentType("text/plain"))
	require.NoError(t, err)
	assert.Equal(t, "greeting.txt", info.Key)
	assert.Equal(t, int64(len(data)), info.Size)

	// Get
	reader, err := tr.Get(ctx, "test", "greeting.txt")
	require.NoError(t, err)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, string(got))

	// Head
	head, err := tr.Head(ctx, "test", "greeting.txt")
	require.NoError(t, err)
	assert.Equal(t, "greeting.txt", head.Key)
	assert.Equal(t, int64(len(data)), head.Size)

	// Delete
	err = tr.Delete(ctx, "test", "greeting.txt")
	require.NoError(t, err)

	// Confirm deletion.
	_, err = tr.Get(ctx, "test", "greeting.txt")
	assert.Error(t, err)
}

func TestDefaultBucket(t *testing.T) {
	ctx := context.Background()
	drv := memdriver.New()

	tr, err := trove.Open(drv, trove.WithDefaultBucket("default"))
	require.NoError(t, err)
	defer tr.Close(ctx)

	require.NoError(t, tr.CreateBucket(ctx, "default"))

	// Put with empty bucket should use default.
	_, err = tr.Put(ctx, "", "file.txt", strings.NewReader("content"))
	require.NoError(t, err)

	// Get with empty bucket should use default.
	reader, err := tr.Get(ctx, "", "file.txt")
	require.NoError(t, err)
	reader.Close()
}

func TestEmptyBucketError(t *testing.T) {
	ctx := context.Background()
	tr, err := trove.Open(memdriver.New()) // no default bucket
	require.NoError(t, err)

	_, err = tr.Put(ctx, "", "file.txt", strings.NewReader("data"))
	assert.ErrorIs(t, err, trove.ErrBucketEmpty)

	_, err = tr.Get(ctx, "", "file.txt")
	assert.ErrorIs(t, err, trove.ErrBucketEmpty)

	err = tr.Delete(ctx, "", "file.txt")
	assert.ErrorIs(t, err, trove.ErrBucketEmpty)

	_, err = tr.Head(ctx, "", "file.txt")
	assert.ErrorIs(t, err, trove.ErrBucketEmpty)

	_, err = tr.List(ctx, "")
	assert.ErrorIs(t, err, trove.ErrBucketEmpty)
}

func TestEmptyKeyError(t *testing.T) {
	ctx := context.Background()
	tr, err := trove.Open(memdriver.New())
	require.NoError(t, err)

	_, err = tr.Put(ctx, "bucket", "", strings.NewReader("data"))
	assert.ErrorIs(t, err, trove.ErrKeyEmpty)

	_, err = tr.Get(ctx, "bucket", "")
	assert.ErrorIs(t, err, trove.ErrKeyEmpty)

	err = tr.Delete(ctx, "bucket", "")
	assert.ErrorIs(t, err, trove.ErrKeyEmpty)

	_, err = tr.Head(ctx, "bucket", "")
	assert.ErrorIs(t, err, trove.ErrKeyEmpty)
}

func TestMultiBackendRouting(t *testing.T) {
	ctx := context.Background()

	primaryDrv := memdriver.New()
	archiveDrv := memdriver.New()

	tr, err := trove.Open(primaryDrv,
		trove.WithBackend("archive", archiveDrv),
		trove.WithRoute("*.log", "archive"),
	)
	require.NoError(t, err)
	defer tr.Close(ctx)

	// Create buckets on both backends.
	require.NoError(t, primaryDrv.CreateBucket(ctx, "logs"))
	require.NoError(t, archiveDrv.CreateBucket(ctx, "logs"))

	// Put a .log file — should route to archive backend.
	_, err = tr.Put(ctx, "logs", "app.log", strings.NewReader("log data"))
	require.NoError(t, err)

	// The log file should be on the archive driver, not primary.
	_, err = archiveDrv.Get(ctx, "logs", "app.log")
	assert.NoError(t, err)

	_, err = primaryDrv.Get(ctx, "logs", "app.log")
	assert.Error(t, err)
}

func TestRouteFuncRouting(t *testing.T) {
	ctx := context.Background()

	primaryDrv := memdriver.New()
	complianceDrv := memdriver.New()

	tr, err := trove.Open(primaryDrv,
		trove.WithBackend("compliance", complianceDrv),
		trove.WithRouteFunc(func(bucket, _ string) string {
			if bucket == "compliance" {
				return "compliance"
			}
			return ""
		}),
	)
	require.NoError(t, err)

	require.NoError(t, complianceDrv.CreateBucket(ctx, "compliance"))
	require.NoError(t, primaryDrv.CreateBucket(ctx, "general"))

	_, err = tr.Put(ctx, "compliance", "secret.pdf", strings.NewReader("classified"))
	require.NoError(t, err)

	_, err = complianceDrv.Get(ctx, "compliance", "secret.pdf")
	assert.NoError(t, err)
}

func TestBackend(t *testing.T) {
	ctx := context.Background()

	primaryDrv := memdriver.New()
	namedDrv := memdriver.New()

	tr, err := trove.Open(primaryDrv,
		trove.WithBackend("named", namedDrv),
	)
	require.NoError(t, err)

	t.Run("existing backend", func(t *testing.T) {
		backend, err := tr.Backend("named")
		require.NoError(t, err)
		assert.Equal(t, namedDrv, backend.Driver())

		require.NoError(t, namedDrv.CreateBucket(ctx, "test"))
		_, err = backend.Put(ctx, "test", "file.txt", strings.NewReader("data"))
		require.NoError(t, err)
	})

	t.Run("non-existing backend", func(t *testing.T) {
		_, err := tr.Backend("nonexistent")
		assert.ErrorIs(t, err, trove.ErrBackendNotFound)
	})
}

func TestDriver(t *testing.T) {
	drv := memdriver.New()
	tr, err := trove.Open(drv)
	require.NoError(t, err)
	assert.Equal(t, drv, tr.Driver())
}

func TestClose(t *testing.T) {
	ctx := context.Background()
	drv := memdriver.New()

	tr, err := trove.Open(drv)
	require.NoError(t, err)

	err = tr.Close(ctx)
	assert.NoError(t, err)

	// Driver should be closed.
	err = drv.Ping(ctx)
	assert.Error(t, err)
}

func TestBucketOperations(t *testing.T) {
	ctx := context.Background()
	tr, err := trove.Open(memdriver.New())
	require.NoError(t, err)

	t.Run("CreateBucket", func(t *testing.T) {
		err := tr.CreateBucket(ctx, "new-bucket")
		require.NoError(t, err)
	})

	t.Run("CreateBucket_EmptyName", func(t *testing.T) {
		err := tr.CreateBucket(ctx, "")
		assert.ErrorIs(t, err, trove.ErrBucketEmpty)
	})

	t.Run("ListBuckets", func(t *testing.T) {
		buckets, err := tr.ListBuckets(ctx)
		require.NoError(t, err)
		assert.Len(t, buckets, 1)
	})

	t.Run("DeleteBucket", func(t *testing.T) {
		err := tr.DeleteBucket(ctx, "new-bucket")
		require.NoError(t, err)
	})

	t.Run("DeleteBucket_EmptyName", func(t *testing.T) {
		err := tr.DeleteBucket(ctx, "")
		assert.ErrorIs(t, err, trove.ErrBucketEmpty)
	})
}

func TestCopy(t *testing.T) {
	ctx := context.Background()
	tr, err := trove.Open(memdriver.New())
	require.NoError(t, err)

	require.NoError(t, tr.CreateBucket(ctx, "src"))
	require.NoError(t, tr.CreateBucket(ctx, "dst"))

	_, err = tr.Put(ctx, "src", "file.txt", strings.NewReader("data"))
	require.NoError(t, err)

	info, err := tr.Copy(ctx, "src", "file.txt", "dst", "copy.txt")
	require.NoError(t, err)
	assert.Equal(t, "copy.txt", info.Key)

	t.Run("empty bucket", func(t *testing.T) {
		_, err := tr.Copy(ctx, "", "file.txt", "dst", "copy.txt")
		assert.ErrorIs(t, err, trove.ErrBucketEmpty)
	})

	t.Run("empty key", func(t *testing.T) {
		_, err := tr.Copy(ctx, "src", "", "dst", "copy.txt")
		assert.ErrorIs(t, err, trove.ErrKeyEmpty)
	})
}

func TestList(t *testing.T) {
	ctx := context.Background()
	tr, err := trove.Open(memdriver.New())
	require.NoError(t, err)

	require.NoError(t, tr.CreateBucket(ctx, "data"))
	for _, key := range []string{"a.txt", "b.txt", "c.txt"} {
		_, err = tr.Put(ctx, "data", key, strings.NewReader("content"))
		require.NoError(t, err)
	}

	iter, err := tr.List(ctx, "data", driver.WithPrefix("a"))
	require.NoError(t, err)

	objects, err := iter.All(ctx)
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "a.txt", objects[0].Key)
}
