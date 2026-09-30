package contract

import (
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
)

func TestStores_SingleResolvesEmptyAndDefault(t *testing.T) {
	tv := openMem(t)
	s := NewSingleStore(tv, Flags{CAS: true})
	for _, name := range []string{"", SingleStoreName} {
		st, err := s.Resolve(name)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", name, err)
		}
		if st.Trove != tv || st.Name != SingleStoreName || !st.Configured.CAS {
			t.Fatalf("Resolve(%q) = %+v", name, st)
		}
	}
	if s.Multi() {
		t.Error("a single store reports multi mode")
	}
}

func TestStores_ResolveUnknownIsNotFound(t *testing.T) {
	s := NewSingleStore(openMem(t), Flags{})
	if _, err := s.Resolve("archive"); codeOf(err) != dashcontract.CodeNotFound {
		t.Fatalf("Resolve(archive) = %v, want NOT_FOUND", err)
	}
}

func TestStores_ResolveBlankIsBadRequest(t *testing.T) {
	s := NewSingleStore(openMem(t), Flags{})
	for _, name := range []string{" ", "\t", "  \n"} {
		if _, err := s.Resolve(name); codeOf(err) != dashcontract.CodeBadRequest {
			t.Errorf("Resolve(%q) = %v, want BAD_REQUEST", name, err)
		}
	}
}

func TestStores_MultiResolvesByIdentity(t *testing.T) {
	a, b := openMem(t), openMem(t)
	s, err := NewStores("b", []Store{{Name: "a", Trove: a}, {Name: "b", Trove: b}})
	if err != nil {
		t.Fatalf("NewStores: %v", err)
	}
	if !s.Multi() || s.DefaultName() != "b" {
		t.Fatalf("Multi=%v DefaultName=%q", s.Multi(), s.DefaultName())
	}
	got, err := s.Resolve("a")
	if err != nil || got.Trove != a {
		t.Fatalf("Resolve(a) = %+v, %v", got, err)
	}
	def, err := s.Resolve("")
	if err != nil || def.Trove != b {
		t.Fatalf("Resolve(\"\") = %+v, %v; want the default store b", def, err)
	}
	names := make([]string, 0, len(s.All()))
	for _, st := range s.All() {
		names = append(names, st.Name)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("All() = %v, want [a b] in declared order", names)
	}
}

func TestNewStores_Validates(t *testing.T) {
	tv := openMem(t)
	cases := map[string]struct {
		def  string
		list []Store
	}{
		"empty list":      {"", nil},
		"blank name":      {"", []Store{{Name: " ", Trove: tv}}},
		"duplicate name":  {"", []Store{{Name: "a", Trove: tv}, {Name: "a", Trove: tv}}},
		"missing default": {"z", []Store{{Name: "a", Trove: tv}}},
		"nil trove":       {"", []Store{{Name: "a"}}},
	}
	for name, c := range cases {
		if _, err := NewStores(c.def, c.list); err == nil {
			t.Errorf("%s: NewStores accepted it", name)
		}
	}
}

func TestNewStores_EmptyDefaultIsFirst(t *testing.T) {
	a, b := openMem(t), openMem(t)
	s, err := NewStores("", []Store{{Name: "a", Trove: a}, {Name: "b", Trove: b}})
	if err != nil {
		t.Fatalf("NewStores: %v", err)
	}
	if s.DefaultName() != "a" {
		t.Fatalf("DefaultName = %q, want a", s.DefaultName())
	}
}
