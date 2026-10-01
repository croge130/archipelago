package evaluation

import (
	"context"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// effectiveEntry is one (permission pattern, effect) pair contributed
// either directly by a Grant or by expanding a Role it targets.
type effectiveEntry struct {
	PermissionPattern string
	Effect            structure.GrantEffect
}

// expandRole flattens a role's own permissions plus everything it
// inherits, transitively, into one pool — no separate "child overrides
// parent" rule, per the model doc's Role section. The same
// deny-always-wins resolution that applies to ordinary grants applies
// uniformly to this pool afterward; expandRole itself only collects
// entries, it never resolves them.
//
// Cycle-checked: a role already visited in this expansion is not
// walked again, which both prevents infinite recursion and bounds the
// work to the number of distinct roles reachable, however the
// inheritance graph is shaped.
func expandRole(ctx context.Context, store Store, roleID uuid.UUID) ([]effectiveEntry, error) {
	visited := make(map[uuid.UUID]bool)
	var entries []effectiveEntry

	var walk func(uuid.UUID) error
	walk = func(id uuid.UUID) error {
		if visited[id] {
			return nil
		}
		visited[id] = true

		perms, err := store.RolePermissions(ctx, id)
		if err != nil {
			return err
		}
		for _, p := range perms {
			if p.IsInheritanceEdge() {
				if err := walk(*p.ChildRoleID); err != nil {
					return err
				}
				continue
			}
			entries = append(entries, effectiveEntry{
				PermissionPattern: *p.PermissionKey,
				Effect:            p.Effect,
			})
		}
		return nil
	}

	if err := walk(roleID); err != nil {
		return nil, err
	}
	return entries, nil
}
