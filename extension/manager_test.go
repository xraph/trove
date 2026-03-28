package extension

import (
	"context"
	"testing"

	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/memdriver"
)

func newTestTrove(t *testing.T) *trove.Trove {
	t.Helper()
	drv := memdriver.New()
	if err := drv.Open(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	tv, err := trove.Open(drv)
	if err != nil {
		t.Fatal(err)
	}
	return tv
}

func TestTroveManager_Add_And_Get(t *testing.T) {
	mgr := NewTroveManager()
	tv := newTestTrove(t)

	mgr.Add("primary", tv, nil)

	got, err := mgr.Get("primary")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != tv {
		t.Fatal("Get returned wrong instance")
	}
}

func TestTroveManager_Get_NotFound(t *testing.T) {
	mgr := NewTroveManager()

	_, err := mgr.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent store")
	}
}

func TestTroveManager_Default_FirstEntry(t *testing.T) {
	mgr := NewTroveManager()
	tv1 := newTestTrove(t)
	tv2 := newTestTrove(t)

	mgr.Add("first", tv1, nil)
	mgr.Add("second", tv2, nil)

	got, err := mgr.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if got != tv1 {
		t.Fatal("Default should return first added entry")
	}
	if mgr.DefaultName() != "first" {
		t.Fatalf("DefaultName: got %q, want %q", mgr.DefaultName(), "first")
	}
}

func TestTroveManager_SetDefault(t *testing.T) {
	mgr := NewTroveManager()
	tv1 := newTestTrove(t)
	tv2 := newTestTrove(t)

	mgr.Add("first", tv1, nil)
	mgr.Add("second", tv2, nil)

	if err := mgr.SetDefault("second"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	got, err := mgr.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if got != tv2 {
		t.Fatal("Default should return second after SetDefault")
	}
}

func TestTroveManager_SetDefault_NotFound(t *testing.T) {
	mgr := NewTroveManager()

	if err := mgr.SetDefault("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent store")
	}
}

func TestTroveManager_All(t *testing.T) {
	mgr := NewTroveManager()
	tv1 := newTestTrove(t)
	tv2 := newTestTrove(t)

	mgr.Add("a", tv1, nil)
	mgr.Add("b", tv2, nil)

	all := mgr.All()
	if len(all) != 2 {
		t.Fatalf("All: got %d, want 2", len(all))
	}
	if all["a"] != tv1 || all["b"] != tv2 {
		t.Fatal("All returned wrong instances")
	}
}

func TestTroveManager_Len(t *testing.T) {
	mgr := NewTroveManager()
	if mgr.Len() != 0 {
		t.Fatalf("Len: got %d, want 0", mgr.Len())
	}

	mgr.Add("x", newTestTrove(t), nil)
	if mgr.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", mgr.Len())
	}
}

func TestTroveManager_Close(t *testing.T) {
	mgr := NewTroveManager()
	mgr.Add("a", newTestTrove(t), nil)
	mgr.Add("b", newTestTrove(t), nil)

	if err := mgr.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTroveManager_Default_Empty(t *testing.T) {
	mgr := NewTroveManager()

	_, err := mgr.Default()
	if err == nil {
		t.Fatal("expected error for empty manager")
	}
}

func TestTroveManager_DefaultStore(t *testing.T) {
	mgr := NewTroveManager()
	tv := newTestTrove(t)

	// Without metadata store — DefaultStore should error.
	mgr.Add("primary", tv, nil)

	_, err := mgr.DefaultStore()
	if err == nil {
		t.Fatal("expected error when no metadata store registered")
	}
}

func TestTroveManager_GetStore_NotFound(t *testing.T) {
	mgr := NewTroveManager()

	_, err := mgr.GetStore("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent metadata store")
	}
}
