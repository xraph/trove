package contract

import (
	"fmt"
	"strings"

	"github.com/xraph/trove"
)

// SingleStoreName is the name the only store answers to in single-store mode.
const SingleStoreName = "default"

// Flags records which protections a store's configuration asked for. It is
// what an operator configured, not what is applied: system.status compares
// the two.
type Flags struct {
	Encryption  bool
	Compression bool
	CAS         bool
}

// Store is one named Trove the dashboard can address.
type Store struct {
	Name       string
	Trove      *trove.Trove
	Configured Flags
}

// Stores resolves the optional `store` field every intent takes.
type Stores struct {
	multi  bool
	def    string
	list   []Store
	byName map[string]int
}

// NewSingleStore wraps the one Trove of a single-store deployment. It
// answers to "" and to SingleStoreName.
func NewSingleStore(t *trove.Trove, configured Flags) *Stores {
	return &Stores{
		def:    SingleStoreName,
		list:   []Store{{Name: SingleStoreName, Trove: t, Configured: configured}},
		byName: map[string]int{SingleStoreName: 0},
	}
}

// NewStores wraps the named Troves of a multi-store deployment, in the
// order given. An empty defaultName makes the first store the default.
func NewStores(defaultName string, list []Store) (*Stores, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("trove/contract: no stores")
	}
	s := &Stores{multi: true, list: make([]Store, 0, len(list)), byName: map[string]int{}}
	for _, st := range list {
		if strings.TrimSpace(st.Name) == "" {
			return nil, fmt.Errorf("trove/contract: a store has a blank name")
		}
		if st.Trove == nil {
			return nil, fmt.Errorf("trove/contract: store %q has no trove", st.Name)
		}
		if _, dup := s.byName[st.Name]; dup {
			return nil, fmt.Errorf("trove/contract: duplicate store %q", st.Name)
		}
		s.byName[st.Name] = len(s.list)
		s.list = append(s.list, st)
	}
	s.def = defaultName
	if s.def == "" {
		s.def = list[0].Name
	}
	if _, ok := s.byName[s.def]; !ok {
		return nil, fmt.Errorf("trove/contract: default store %q is not in the list", s.def)
	}
	return s, nil
}

// Multi reports whether this deployment runs more than one named store.
func (s *Stores) Multi() bool { return s.multi }

// DefaultName is the store an absent `store` field resolves to.
func (s *Stores) DefaultName() string { return s.def }

// All returns every store in declared order.
func (s *Stores) All() []Store {
	out := make([]Store, len(s.list))
	copy(out, s.list)
	return out
}

// Resolve maps a request's `store` field to a store. Empty means the
// default. A value that is blank once trimmed is a bad request: it was sent
// and cannot be used, and treating it as absent would answer with the
// default store's objects. An unknown name is not found, never the default.
func (s *Stores) Resolve(name string) (*Store, error) {
	if name == "" {
		name = s.def
	} else if strings.TrimSpace(name) == "" {
		return nil, badRequest("store is blank")
	}
	i, ok := s.byName[name]
	if !ok {
		return nil, notFound(fmt.Sprintf("no store named %q", name))
	}
	return &s.list[i], nil
}
