package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// GrantPermission creates a global-scope, allow-effect grant with sane
// defaults filled in (ID, timestamps, active status, manual origin) —
// the common case. Writer.CreateGrant stays available directly for
// anything needing deny, context scope, a role target, or a different
// origin; this never becomes a second way to construct a Grant, just a
// shortcut for the shape most callers want.
func GrantPermission(ctx context.Context, writer Writer, subjectType structure.GrantSubjectType, subjectID uuid.UUID, permissionKey string) (structure.Grant, error) {
	now := time.Now()
	key := permissionKey
	g := structure.Grant{
		GrantID:       uuid.New(),
		SubjectType:   subjectType,
		SubjectID:     subjectID,
		TargetType:    structure.GrantTargetTypePermission,
		PermissionKey: &key,
		Scope:         structure.GrantScopeGlobal,
		Effect:        structure.GrantEffectAllow,
		Status:        structure.GrantStatusActive,
		Origin:        structure.GrantOriginManual,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := g.Validate(); err != nil {
		return structure.Grant{}, fmt.Errorf("facade: grant permission: %w", err)
	}
	if err := writer.CreateGrant(ctx, g); err != nil {
		return structure.Grant{}, fmt.Errorf("facade: grant permission: %w", err)
	}
	return g, nil
}
