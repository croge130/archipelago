package evaluation

import (
	"context"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// Store is everything Evaluate needs to read. Defined here, in the
// consuming package, rather than in structure or storage — the
// dependency points from a concrete Storage implementation toward this
// interface, never the other way around, which is what lets Storage be
// swapped later without Evaluation or any calling code noticing.
type Store interface {
	// GetPermissionDefinition looks up a registered permission key.
	// found is false if the key isn't registered at all (which is a
	// "no matching permission" denial, not an error).
	GetPermissionDefinition(ctx context.Context, permissionKey string) (def structure.PermissionDefinition, found bool, err error)

	// ActiveGrantsForSubject returns active grants whose subject is
	// exactly this (subjectType, subjectID) pair — used once for the
	// requesting principal directly, and once per group it belongs to.
	ActiveGrantsForSubject(ctx context.Context, subjectType structure.GrantSubjectType, subjectID uuid.UUID) ([]structure.Grant, error)

	// GroupIDsForPrincipal returns every group the principal currently
	// belongs to.
	GroupIDsForPrincipal(ctx context.Context, principalID uuid.UUID) ([]uuid.UUID, error)

	// RolePermissions returns a role's own direct entries (permission
	// grants and inheritance edges alike) — non-recursive; expandRole
	// in this package does the recursive, cycle-checked walk.
	RolePermissions(ctx context.Context, roleID uuid.UUID) ([]structure.RolePermission, error)
}
