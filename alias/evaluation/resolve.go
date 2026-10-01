package evaluation

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/alias/structure"
)

// Store is everything Resolve needs to read.
type Store interface {
	// GetAlias looks up an alias by its (table, name) primary key,
	// regardless of lifecycle — Resolve is what decides whether a
	// released alias counts as "found."
	GetAlias(ctx context.Context, table, name string) (structure.Alias, bool, error)
}

// Resolve looks up table+name and returns its target, but only if the
// alias is currently active — a released alias resolves as not found,
// the same way it would if it had never existed. Target is never
// cleared on release (see structure.Alias), but that's for debugging
// an alias's history, not for letting a released handle keep resolving.
func Resolve(ctx context.Context, store Store, table, name string) (target string, found bool, err error) {
	a, found, err := store.GetAlias(ctx, table, name)
	if err != nil {
		return "", false, fmt.Errorf("evaluation: resolve: %w", err)
	}
	if !found || a.Lifecycle != structure.LifecycleActive {
		return "", false, nil
	}
	return a.Target, true, nil
}
