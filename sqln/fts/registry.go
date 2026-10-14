package fts

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// registry holds the indexes declared with MustIndex, keyed by table and name.
var registry = struct {
	sync.Mutex
	byKey map[string]*Index
}{byKey: map[string]*Index{}}

// register records idx, declared at package level by MustIndex. A second
// declaration of the same table and name must have the same definition:
// two packages searching one index differently would each pass the check
// against their own migration only.
func register(idx *Index) error {
	registry.Lock()
	defer registry.Unlock()
	key := idx.def.Table + "\x00" + idx.def.Name
	if have, ok := registry.byKey[key]; ok {
		if !reflect.DeepEqual(have, idx) {
			return fmt.Errorf("%w: %s on %s is declared twice with different definitions", ErrInvalidIndex, idx.def.Name, idx.def.Table)
		}
		return nil
	}
	registry.byKey[key] = idx
	return nil
}

// Registered returns the indexes declared with MustIndex in the packages
// linked into the binary, sorted by table and name. gofi's database
// component checks them when the service starts.
func Registered() []*Index {
	registry.Lock()
	defer registry.Unlock()
	out := make([]*Index, 0, len(registry.byKey))
	for _, idx := range registry.byKey {
		out = append(out, idx)
	}
	slices.SortFunc(out, func(a, b *Index) int {
		return strings.Compare(a.def.Table+"\x00"+a.def.Name, b.def.Table+"\x00"+b.def.Name)
	})
	return out
}
