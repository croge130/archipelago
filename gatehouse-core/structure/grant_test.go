package structure

import (
	"testing"

	"github.com/google/uuid"
)

func validPermissionGrant() Grant {
	key := "myapp.readinglist.read"
	return Grant{
		GrantID:       uuid.New(),
		SubjectType:   GrantSubjectTypePrincipal,
		SubjectID:     uuid.New(),
		TargetType:    GrantTargetTypePermission,
		PermissionKey: &key,
		Scope:         GrantScopeGlobal,
		Effect:        GrantEffectAllow,
		Status:        GrantStatusActive,
		Origin:        GrantOriginManual,
	}
}

func TestGrantValidateOK(t *testing.T) {
	if err := validPermissionGrant().Validate(); err != nil {
		t.Fatalf("expected a valid grant to validate, got: %v", err)
	}
}

func TestGrantValidateRejectsBothPermissionKeyAndRoleID(t *testing.T) {
	g := validPermissionGrant()
	roleID := uuid.New()
	g.RoleID = &roleID
	if err := g.Validate(); err == nil {
		t.Fatal("expected an error when both PermissionKey and RoleID are set")
	}
}

func TestGrantValidateRejectsNeitherPermissionKeyNorRoleID(t *testing.T) {
	g := validPermissionGrant()
	g.PermissionKey = nil
	if err := g.Validate(); err == nil {
		t.Fatal("expected an error when neither PermissionKey nor RoleID is set")
	}
}

func TestGrantValidateRoleTarget(t *testing.T) {
	roleID := uuid.New()
	g := Grant{
		GrantID:     uuid.New(),
		SubjectType: GrantSubjectTypeGroup,
		SubjectID:   uuid.New(),
		TargetType:  GrantTargetTypeRole,
		RoleID:      &roleID,
		Scope:       GrantScopeGlobal,
		Effect:      GrantEffectDeny,
		Status:      GrantStatusActive,
		Origin:      GrantOriginProvisioned,
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("expected a valid role-target grant to validate, got: %v", err)
	}
}

func TestGrantValidateContextScopeRequiresContext(t *testing.T) {
	g := validPermissionGrant()
	g.Scope = GrantScopeContext
	if err := g.Validate(); err == nil {
		t.Fatal("expected an error for context scope with no ContextType/ContextID")
	}
	ctxType, ctxID := "myapp.readinglist", "42"
	g.ContextType = &ctxType
	g.ContextID = &ctxID
	if err := g.Validate(); err != nil {
		t.Fatalf("expected a valid context-scoped grant to validate, got: %v", err)
	}
}

func TestGrantValidateGlobalScopeRejectsContext(t *testing.T) {
	g := validPermissionGrant()
	ctxType, ctxID := "myapp.readinglist", "42"
	g.ContextType = &ctxType
	g.ContextID = &ctxID
	if err := g.Validate(); err == nil {
		t.Fatal("expected an error for global scope carrying a context")
	}
}

func TestGrantEffectAllowsDeny(t *testing.T) {
	g := validPermissionGrant()
	g.Effect = GrantEffectDeny
	if err := g.Validate(); err != nil {
		t.Fatalf("expected deny to be a valid effect, got: %v", err)
	}
}
