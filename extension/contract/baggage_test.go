package contract

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/baggage"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
)

// baggageSpy is a presigning driver that remembers which of its calls saw
// baggage on their context.
type baggageSpy struct {
	driver.Driver

	mu  sync.Mutex
	saw []string
}

func (s *baggageSpy) note(ctx context.Context, method string) {
	if baggage.FromContext(ctx).Len() == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saw = append(s.saw, method)
}

func (s *baggageSpy) List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	s.note(ctx, "List")
	return s.Driver.List(ctx, bucket, opts...)
}

func (s *baggageSpy) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	s.note(ctx, "Head")
	return s.Driver.Head(ctx, bucket, key)
}

func (s *baggageSpy) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	s.note(ctx, "Put")
	return s.Driver.Put(ctx, bucket, key, r, opts...)
}

func (s *baggageSpy) DeleteBucket(ctx context.Context, name string) error {
	s.note(ctx, "DeleteBucket")
	return s.Driver.DeleteBucket(ctx, name)
}

func (s *baggageSpy) PresignGet(ctx context.Context, _, _ string, _ time.Duration) (string, error) {
	s.note(ctx, "PresignGet")
	return "https://signed.example/object", nil
}

func (s *baggageSpy) PresignPut(ctx context.Context, _, _ string, _ time.Duration) (string, error) {
	s.note(ctx, "PresignPut")
	return "https://signed.example/object", nil
}

// TestDirectDriverCallsDropBaggage covers the handlers that reach a driver
// without the facade (the CAS listing, the emptiness check before a bucket
// delete, and presign): none of them hands the caller's baggage on.
func TestDirectDriverCallsDropBaggage(t *testing.T) {
	mem := memdriver.New()
	if err := mem.Open(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	spy := &baggageSpy{Driver: mem}
	tv := openTrove(t, spy, trove.WithCAS(cas.AlgSHA256))
	for _, b := range []string{"cas", "docs", "empty"} {
		if err := tv.CreateBucket(context.Background(), b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tv.Put(context.Background(), "docs", "a.txt", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}

	member, err := baggage.NewMemberRaw("user.email", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx := baggage.ContextWithBaggage(context.Background(), bag)
	deps := testDeps(t, newStores(tv))
	p := principalFor("u")

	if _, err := casListHandler(deps)(ctx, casListInput{}, p); err != nil {
		t.Fatalf("cas.list: %v", err)
	}
	if _, err := bucketsDeleteHandler(deps)(ctx, bucketInput{Name: "empty"}, p); err != nil {
		t.Fatalf("buckets.delete: %v", err)
	}
	if _, err := objectsPresignHandler(deps)(ctx, objectsPresignInput{Bucket: "docs", Key: "a.txt"}, p); err != nil {
		t.Fatalf("objects.presign: %v", err)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.saw) > 0 {
		t.Fatalf("driver calls carried the caller's baggage: %v", spy.saw)
	}
}
