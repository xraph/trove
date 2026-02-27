//go:build bench

package bench

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/drivers/memdriver"
)

// randomData returns n bytes of deterministic pseudo-random data.
func randomData(n int) []byte {
	r := rand.New(rand.NewSource(42))
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(r.Intn(256))
	}
	return buf
}

// setupMemDriver creates a Trove instance backed by memdriver.
func setupMemDriver(b *testing.B) *trove.Trove {
	b.Helper()
	ctx := context.Background()

	drv := memdriver.New()
	if err := drv.Open(ctx, "mem://"); err != nil {
		b.Fatal(err)
	}

	t, err := trove.Open(drv)
	if err != nil {
		b.Fatal(err)
	}

	if err := t.CreateBucket(ctx, "bench"); err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { t.Close(ctx) })
	return t
}

// setupLocalDriver creates a Trove instance backed by localdriver in a temp dir.
func setupLocalDriver(b *testing.B) *trove.Trove {
	b.Helper()
	ctx := context.Background()
	dir := b.TempDir()

	drv := localdriver.New()
	if err := drv.Open(ctx, "file://"+dir); err != nil {
		b.Fatal(err)
	}

	t, err := trove.Open(drv)
	if err != nil {
		b.Fatal(err)
	}

	if err := t.CreateBucket(ctx, "bench"); err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { t.Close(ctx) })
	return t
}

// --- Put Benchmarks ---

func benchPut(b *testing.B, t *trove.Trove, size int) {
	data := randomData(size)
	ctx := context.Background()
	b.SetBytes(int64(size))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("obj-%d", i)
		_, err := t.Put(ctx, "bench", key, bytes.NewReader(data), driver.WithContentType("application/octet-stream"))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPut_Mem_1KB(b *testing.B)    { benchPut(b, setupMemDriver(b), 1<<10) }
func BenchmarkPut_Mem_1MB(b *testing.B)    { benchPut(b, setupMemDriver(b), 1<<20) }
func BenchmarkPut_Mem_10MB(b *testing.B)   { benchPut(b, setupMemDriver(b), 10<<20) }
func BenchmarkPut_Local_1KB(b *testing.B)  { benchPut(b, setupLocalDriver(b), 1<<10) }
func BenchmarkPut_Local_1MB(b *testing.B)  { benchPut(b, setupLocalDriver(b), 1<<20) }
func BenchmarkPut_Local_10MB(b *testing.B) { benchPut(b, setupLocalDriver(b), 10<<20) }

// --- Get Benchmarks ---

func benchGet(b *testing.B, t *trove.Trove, size int) {
	data := randomData(size)
	ctx := context.Background()

	// Seed one object.
	_, err := t.Put(ctx, "bench", "get-target", bytes.NewReader(data), driver.WithContentType("application/octet-stream"))
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(size))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader, err := t.Get(ctx, "bench", "get-target")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			b.Fatal(err)
		}
		reader.Close()
	}
}

func BenchmarkGet_Mem_1KB(b *testing.B)    { benchGet(b, setupMemDriver(b), 1<<10) }
func BenchmarkGet_Mem_1MB(b *testing.B)    { benchGet(b, setupMemDriver(b), 1<<20) }
func BenchmarkGet_Mem_10MB(b *testing.B)   { benchGet(b, setupMemDriver(b), 10<<20) }
func BenchmarkGet_Local_1KB(b *testing.B)  { benchGet(b, setupLocalDriver(b), 1<<10) }
func BenchmarkGet_Local_1MB(b *testing.B)  { benchGet(b, setupLocalDriver(b), 1<<20) }
func BenchmarkGet_Local_10MB(b *testing.B) { benchGet(b, setupLocalDriver(b), 10<<20) }

// --- Head Benchmarks ---

func benchHead(b *testing.B, t *trove.Trove) {
	ctx := context.Background()
	data := randomData(1024)

	_, err := t.Put(ctx, "bench", "head-target", bytes.NewReader(data), driver.WithContentType("text/plain"))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := t.Head(ctx, "bench", "head-target")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHead_Mem(b *testing.B)   { benchHead(b, setupMemDriver(b)) }
func BenchmarkHead_Local(b *testing.B) { benchHead(b, setupLocalDriver(b)) }

// --- Delete Benchmarks ---

func benchDelete(b *testing.B, setup func(b *testing.B) *trove.Trove) {
	ctx := context.Background()
	data := randomData(1024)

	// Re-seed before each iteration by using StopTimer/StartTimer.
	t := setup(b)
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("del-%d", i)
		_, err := t.Put(ctx, "bench", key, bytes.NewReader(data), driver.WithContentType("application/octet-stream"))
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("del-%d", i)
		if err := t.Delete(ctx, "bench", key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDelete_Mem(b *testing.B)   { benchDelete(b, setupMemDriver) }
func BenchmarkDelete_Local(b *testing.B) { benchDelete(b, setupLocalDriver) }

// --- Copy Benchmarks ---

func benchCopy(b *testing.B, t *trove.Trove) {
	ctx := context.Background()
	data := randomData(1 << 20) // 1MB

	_, err := t.Put(ctx, "bench", "copy-src", bytes.NewReader(data), driver.WithContentType("application/octet-stream"))
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dst := fmt.Sprintf("copy-dst-%d", i)
		_, err := t.Copy(ctx, "bench", "copy-src", "bench", dst)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCopy_Mem(b *testing.B)   { benchCopy(b, setupMemDriver(b)) }
func BenchmarkCopy_Local(b *testing.B) { benchCopy(b, setupLocalDriver(b)) }

// --- List Benchmarks ---

func benchList(b *testing.B, t *trove.Trove, count int) {
	ctx := context.Background()
	data := randomData(64)

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("list-%04d", i)
		_, err := t.Put(ctx, "bench", key, bytes.NewReader(data), driver.WithContentType("text/plain"))
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		iter, err := t.List(ctx, "bench", driver.WithPrefix("list-"))
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		for {
			obj, err := iter.Next(ctx)
			if err != nil {
				break
			}
			_ = obj
			n++
		}
		if n < count {
			b.Fatalf("expected at least %d objects, got %d", count, n)
		}
	}
}

func BenchmarkList_Mem_10(b *testing.B)     { benchList(b, setupMemDriver(b), 10) }
func BenchmarkList_Mem_100(b *testing.B)    { benchList(b, setupMemDriver(b), 100) }
func BenchmarkList_Mem_1000(b *testing.B)   { benchList(b, setupMemDriver(b), 1000) }
func BenchmarkList_Local_10(b *testing.B)   { benchList(b, setupLocalDriver(b), 10) }
func BenchmarkList_Local_100(b *testing.B)  { benchList(b, setupLocalDriver(b), 100) }
func BenchmarkList_Local_1000(b *testing.B) { benchList(b, setupLocalDriver(b), 1000) }
