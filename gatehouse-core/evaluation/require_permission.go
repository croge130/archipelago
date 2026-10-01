package evaluation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// RequirePermission is the sane-defaults convenience wrapper
// 04-facades-and-ergonomics.md describes: a global, standard-authority
// check, for the common case that doesn't need Context scoping or an
// elevated session. It's a thin call into the same Require — never a
// second evaluator — and belongs here rather than in a facade package
// precisely because it needs nothing beyond what evaluation already
// has.
func RequirePermission(ctx context.Context, store Store, principalID uuid.UUID, permissionKey string) error {
	return Require(ctx, store, Request{
		PrincipalID:    principalID,
		PermissionKey:  permissionKey,
		Scope:          ScopeGlobal,
		AuthorityLevel: AuthorityLevelStandard,
	})
}

// RequireContextPermission is RequirePermission's context-scoped
// counterpart — still standard authority, still no elevation, just
// scoped to one specific resource instance instead of global.
func RequireContextPermission(ctx context.Context, store Store, principalID uuid.UUID, permissionKey, contextType, contextID string) error {
	if contextType == "" || contextID == "" {
		return fmt.Errorf("evaluation: RequireContextPermission: contextType and contextID are both required")
	}
	return Require(ctx, store, Request{
		PrincipalID:    principalID,
		PermissionKey:  permissionKey,
		Scope:          ScopeContext,
		ContextType:    contextType,
		ContextID:      contextID,
		AuthorityLevel: AuthorityLevelStandard,
	})
}
