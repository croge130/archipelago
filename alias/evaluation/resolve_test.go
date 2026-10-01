package evaluation

import (
	"context"
	"testing"
	"time"

	"github.com/croge130/archipelago/alias/structure"
)

type memStore struct {
	aliases map[string]structure.Alias
}

func newMemStore() *memStore {
	return &memStore{aliases: make(map[string]structure.Alias)}
}

func key(table, name string) string { return table + "\x00" + name }

func (m *memStore) put(a structure.Alias) {
	m.aliases[key(a.Table, a.Name)] = a
}

func (m *memStore) GetAlias(ctx context.Context, table, name string) (structure.Alias, bool, error) {
	a, ok := m.aliases[key(table, name)]
	return a, ok, nil
}

func TestResolveActiveAlias(t *testing.T) {
	store := newMemStore()
	now := time.Now()
	store.put(structure.Alias{Table: "documents", Name: "readme", Target: "doc-42", Lifecycle: structure.LifecycleActive, CreatedAt: now, UpdatedAt: now})

	target, found, err := Resolve(context.Background(), store, "documents", "readme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || target != "doc-42" {
		t.Fatalf("Resolve = (%q, %v), want (doc-42, true)", target, found)
	}
}

func TestResolveReleasedAliasNotFound(t *testing.T) {
	store := newMemStore()
	now := time.Now()
	store.put(structure.Alias{Table: "documents", Name: "readme", Target: "doc-42", Lifecycle: structure.LifecycleReleased, CreatedAt: now, UpdatedAt: now, ReleasedAt: &now})

	_, found, err := Resolve(context.Background(), store, "documents", "readme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if found {
		t.Fatal("expected a released alias to resolve as not found, same as if it never existed")
	}
}

func TestResolveMissingAlias(t *testing.T) {
	store := newMemStore()
	_, found, err := Resolve(context.Background(), store, "documents", "nonexistent")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if found {
		t.Fatal("expected a nonexistent alias to resolve as not found")
	}
}
