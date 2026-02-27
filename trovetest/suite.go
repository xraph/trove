package trovetest

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

// RunDriverSuite runs the full conformance test suite against a driver.
// The factory function must return a fresh, opened driver instance for
// each test. Every Trove driver must pass this suite.
func RunDriverSuite(t *testing.T, factory func(t *testing.T) driver.Driver) {
	t.Helper()

	t.Run("Bucket", func(t *testing.T) {
		t.Run("CreateBucket", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			err := drv.CreateBucket(ctx, "test-bucket")
			require.NoError(t, err)

			buckets, err := drv.ListBuckets(ctx)
			require.NoError(t, err)
			assert.Len(t, buckets, 1)
			assert.Equal(t, "test-bucket", buckets[0].Name)
		})

		t.Run("CreateBucket_Duplicate", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			err := drv.CreateBucket(ctx, "dup-bucket")
			require.NoError(t, err)

			err = drv.CreateBucket(ctx, "dup-bucket")
			assert.Error(t, err)
		})

		t.Run("DeleteBucket", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			err := drv.CreateBucket(ctx, "delete-me")
			require.NoError(t, err)

			err = drv.DeleteBucket(ctx, "delete-me")
			require.NoError(t, err)

			buckets, err := drv.ListBuckets(ctx)
			require.NoError(t, err)
			assert.Empty(t, buckets)
		})

		t.Run("DeleteBucket_NotFound", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			err := drv.DeleteBucket(ctx, "nonexistent")
			assert.Error(t, err)
		})

		t.Run("ListBuckets", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			for _, name := range []string{"alpha", "beta", "gamma"} {
				err := drv.CreateBucket(ctx, name)
				require.NoError(t, err)
			}

			buckets, err := drv.ListBuckets(ctx)
			require.NoError(t, err)
			assert.Len(t, buckets, 3)
			// Should be sorted.
			assert.Equal(t, "alpha", buckets[0].Name)
			assert.Equal(t, "beta", buckets[1].Name)
			assert.Equal(t, "gamma", buckets[2].Name)
		})
	})

	t.Run("PutGet", func(t *testing.T) {
		t.Run("PutAndGet", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			data := []byte("hello world")
			info, err := drv.Put(ctx, "data", "greeting.txt", bytes.NewReader(data),
				driver.WithContentType("text/plain"))
			require.NoError(t, err)
			assert.Equal(t, "greeting.txt", info.Key)
			assert.Equal(t, int64(len(data)), info.Size)
			assert.Equal(t, "text/plain", info.ContentType)

			reader, err := drv.Get(ctx, "data", "greeting.txt")
			require.NoError(t, err)
			defer reader.Close()

			got, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, data, got)
			assert.Equal(t, "greeting.txt", reader.Info.Key)
		})

		t.Run("PutOverwrite", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			_, err := drv.Put(ctx, "data", "file.txt", strings.NewReader("v1"))
			require.NoError(t, err)

			_, err = drv.Put(ctx, "data", "file.txt", strings.NewReader("v2 longer"))
			require.NoError(t, err)

			reader, err := drv.Get(ctx, "data", "file.txt")
			require.NoError(t, err)
			got := ReadAll(t, reader)
			assert.Equal(t, []byte("v2 longer"), got)
		})

		t.Run("PutWithMetadata", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			meta := map[string]string{"author": "test", "version": "1.0"}
			info, err := drv.Put(ctx, "data", "doc.txt", strings.NewReader("content"),
				driver.WithMetadata(meta))
			require.NoError(t, err)
			assert.Equal(t, meta, info.Metadata)

			head, err := drv.Head(ctx, "data", "doc.txt")
			require.NoError(t, err)
			assert.Equal(t, "test", head.Metadata["author"])
			assert.Equal(t, "1.0", head.Metadata["version"])
		})

		t.Run("PutWithContentType", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			info, err := drv.Put(ctx, "data", "image.png", strings.NewReader("fake-png"),
				driver.WithContentType("image/png"))
			require.NoError(t, err)
			assert.Equal(t, "image/png", info.ContentType)
		})

		t.Run("Get_BucketNotFound", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			_, err := drv.Get(ctx, "nonexistent", "key")
			assert.Error(t, err)
		})

		t.Run("Get_ObjectNotFound", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			_, err := drv.Get(ctx, "data", "missing")
			assert.Error(t, err)
		})

		t.Run("PutLargeObject", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			data := RandomData(1024 * 1024) // 1MB
			info, err := drv.Put(ctx, "data", "large.bin", bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, int64(len(data)), info.Size)

			reader, err := drv.Get(ctx, "data", "large.bin")
			require.NoError(t, err)
			got := ReadAll(t, reader)
			assert.Equal(t, data, got)
		})
	})

	t.Run("Delete", func(t *testing.T) {
		t.Run("DeleteExisting", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))
			_, err := drv.Put(ctx, "data", "delete-me.txt", strings.NewReader("bye"))
			require.NoError(t, err)

			err = drv.Delete(ctx, "data", "delete-me.txt")
			require.NoError(t, err)

			_, err = drv.Get(ctx, "data", "delete-me.txt")
			assert.Error(t, err)
		})

		t.Run("DeleteNonExistent", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			// Delete of non-existent key should be idempotent (no error).
			err := drv.Delete(ctx, "data", "ghost.txt")
			assert.NoError(t, err)
		})
	})

	t.Run("Head", func(t *testing.T) {
		t.Run("HeadExisting", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))
			data := []byte("head test")
			_, err := drv.Put(ctx, "data", "head.txt", bytes.NewReader(data),
				driver.WithContentType("text/plain"))
			require.NoError(t, err)

			info, err := drv.Head(ctx, "data", "head.txt")
			require.NoError(t, err)
			assert.Equal(t, "head.txt", info.Key)
			assert.Equal(t, int64(len(data)), info.Size)
			assert.Equal(t, "text/plain", info.ContentType)
			assert.NotEmpty(t, info.ETag)
		})

		t.Run("HeadNonExistent", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			_, err := drv.Head(ctx, "data", "missing.txt")
			assert.Error(t, err)
		})
	})

	t.Run("List", func(t *testing.T) {
		t.Run("ListEmpty", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "empty"))

			iter, err := drv.List(ctx, "empty")
			require.NoError(t, err)

			objects, err := iter.All(ctx)
			require.NoError(t, err)
			assert.Empty(t, objects)
		})

		t.Run("ListMultiple", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			for _, key := range []string{"a.txt", "b.txt", "c.txt"} {
				_, err := drv.Put(ctx, "data", key, strings.NewReader("content"))
				require.NoError(t, err)
			}

			iter, err := drv.List(ctx, "data")
			require.NoError(t, err)

			objects, err := iter.All(ctx)
			require.NoError(t, err)
			assert.Len(t, objects, 3)
			// Should be sorted.
			assert.Equal(t, "a.txt", objects[0].Key)
			assert.Equal(t, "b.txt", objects[1].Key)
			assert.Equal(t, "c.txt", objects[2].Key)
		})

		t.Run("ListWithPrefix", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			for _, key := range []string{"images/a.png", "images/b.png", "docs/c.txt"} {
				_, err := drv.Put(ctx, "data", key, strings.NewReader("content"))
				require.NoError(t, err)
			}

			iter, err := drv.List(ctx, "data", driver.WithPrefix("images/"))
			require.NoError(t, err)

			objects, err := iter.All(ctx)
			require.NoError(t, err)
			assert.Len(t, objects, 2)
			assert.Equal(t, "images/a.png", objects[0].Key)
			assert.Equal(t, "images/b.png", objects[1].Key)
		})

		t.Run("ListPagination", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			for i := range 5 {
				key := string(rune('a'+i)) + ".txt"
				_, err := drv.Put(ctx, "data", key, strings.NewReader("content"))
				require.NoError(t, err)
			}

			// Page 1: max 2 items.
			iter, err := drv.List(ctx, "data", driver.WithMaxKeys(2))
			require.NoError(t, err)

			objects, err := iter.All(ctx)
			require.NoError(t, err)
			assert.Len(t, objects, 2)
			assert.Equal(t, "a.txt", objects[0].Key)
			assert.Equal(t, "b.txt", objects[1].Key)

			// Page 2: using cursor.
			cursor := iter.NextToken()
			assert.NotEmpty(t, cursor)

			iter2, err := drv.List(ctx, "data", driver.WithMaxKeys(2), driver.WithCursor(cursor))
			require.NoError(t, err)

			objects2, err := iter2.All(ctx)
			require.NoError(t, err)
			assert.Len(t, objects2, 2)
			assert.Equal(t, "c.txt", objects2[0].Key)
			assert.Equal(t, "d.txt", objects2[1].Key)
		})

		t.Run("ListBucketNotFound", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			_, err := drv.List(ctx, "nonexistent")
			assert.Error(t, err)
		})
	})

	t.Run("Copy", func(t *testing.T) {
		t.Run("CopyWithinBucket", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "data"))

			data := []byte("copy me")
			_, err := drv.Put(ctx, "data", "original.txt", bytes.NewReader(data),
				driver.WithContentType("text/plain"))
			require.NoError(t, err)

			info, err := drv.Copy(ctx, "data", "original.txt", "data", "copy.txt")
			require.NoError(t, err)
			assert.Equal(t, "copy.txt", info.Key)
			assert.Equal(t, int64(len(data)), info.Size)

			reader, err := drv.Get(ctx, "data", "copy.txt")
			require.NoError(t, err)
			got := ReadAll(t, reader)
			assert.Equal(t, data, got)
		})

		t.Run("CopyAcrossBuckets", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "src"))
			require.NoError(t, drv.CreateBucket(ctx, "dst"))

			data := []byte("cross-bucket copy")
			_, err := drv.Put(ctx, "src", "file.txt", bytes.NewReader(data))
			require.NoError(t, err)

			info, err := drv.Copy(ctx, "src", "file.txt", "dst", "file.txt")
			require.NoError(t, err)
			assert.Equal(t, "file.txt", info.Key)

			reader, err := drv.Get(ctx, "dst", "file.txt")
			require.NoError(t, err)
			got := ReadAll(t, reader)
			assert.Equal(t, data, got)
		})

		t.Run("CopySourceNotFound", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "src"))
			require.NoError(t, drv.CreateBucket(ctx, "dst"))

			_, err := drv.Copy(ctx, "src", "missing.txt", "dst", "copy.txt")
			assert.Error(t, err)
		})
	})

	t.Run("Concurrency", func(t *testing.T) {
		t.Run("ConcurrentPuts", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "concurrent"))

			var wg sync.WaitGroup
			errCh := make(chan error, 20)

			for i := range 20 {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					key := strings.Repeat(string(rune('a'+idx%26)), 5) + ".txt"
					data := RandomData(1024)
					_, err := drv.Put(ctx, "concurrent", key, bytes.NewReader(data))
					if err != nil {
						errCh <- err
					}
				}(i)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Errorf("concurrent put error: %v", err)
			}
		})

		t.Run("ConcurrentReads", func(t *testing.T) {
			drv := factory(t)
			ctx := context.Background()

			require.NoError(t, drv.CreateBucket(ctx, "concurrent"))

			data := []byte("shared content")
			_, err := drv.Put(ctx, "concurrent", "shared.txt", bytes.NewReader(data))
			require.NoError(t, err)

			var wg sync.WaitGroup
			errCh := make(chan error, 20)

			for range 20 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					reader, err := drv.Get(ctx, "concurrent", "shared.txt")
					if err != nil {
						errCh <- err
						return
					}
					got, err := io.ReadAll(reader)
					reader.Close()
					if err != nil {
						errCh <- err
						return
					}
					if !bytes.Equal(data, got) {
						errCh <- err
					}
				}()
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Errorf("concurrent read error: %v", err)
			}
		})
	})

	t.Run("Ping", func(t *testing.T) {
		drv := factory(t)
		ctx := context.Background()

		err := drv.Ping(ctx)
		assert.NoError(t, err)
	})
}
