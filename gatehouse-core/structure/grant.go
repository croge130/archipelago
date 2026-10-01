package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type GrantSubjectType string

const (
	GrantSubjectTypePrincipal GrantSubjectType = "principal"
	GrantSubjectTypeGroup     GrantSubjectType = "group"
)

func (t GrantSubjectType) Valid() bool {
	return t == GrantSubjectTypePrincipal || t == GrantSubjectTypeGroup
}

type GrantTargetType string

const (
	GrantTargetTypePermission GrantTargetType = "permission"
	GrantTargetTypeRole       GrantTargetType = "role"
)

func (t GrantTargetType) Valid() bool {
	return t == GrantTargetTypePermission || t == GrantTargetTypeRole
}

// GrantScope is two-way (global/context), not Lighthouse's three-way
// (lighthouse/realm/context) — the direct, mechanical consequence of
// one level of containment instead of two. See the model doc's Grant
// section.
type GrantScope string

const (
	GrantScopeGlobal  GrantScope = "global"
	GrantScopeContext GrantScope = "context"
)

func (s GrantScope) Valid() bool {
	return s == GrantScopeGlobal || s == GrantScopeContext
}

// GrantEffect: deny always wins, unconditionally, no graduated
// specificity — see the model doc's Deny section for why this is safe
// to build from day one rather than deferred the way Lighthouse left it.
type GrantEffect string

const (
	GrantEffectAllow GrantEffect = "allow"
	GrantEffectDeny  GrantEffect = "deny"
)

func (e GrantEffect) Valid() bool {
	return e == GrantEffectAllow || e == GrantEffectDeny
}

type GrantStatus string

const (
	GrantStatusActive  GrantStatus = "active"
	GrantStatusRevoked GrantStatus = "revoked"
)

func (s GrantStatus) Valid() bool {
	return s == GrantStatusActive || s == GrantStatusRevoked
}

// GrantOrigin: provisioned is what Lighthouse called app_managed — a
// template or automation created this grant, not a human clicking a
// button.
type GrantOrigin string

const (
	GrantOriginBuiltin     GrantOrigin = "builtin"
	GrantOriginManual      GrantOrigin = "manual"
	GrantOriginProvisioned GrantOrigin = "provisioned"
)

func (o GrantOrigin) Valid() bool {
	switch o {
	case GrantOriginBuiltin, GrantOriginManual, GrantOriginProvisioned:
		return true
	default:
		return false
	}
}

type Grant struct {
	GrantID       uuid.UUID
	SubjectType   GrantSubjectType
	SubjectID     uuid.UUID
	TargetType    GrantTargetType
	PermissionKey *string
	RoleID        *uuid.UUID
	Scope         GrantScope
	ContextType   *string
	ContextID     *string
	Effect        GrantEffect
	Status        GrantStatus
	Origin        GrantOrigin
	Metadata      json.RawMessage
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (g Grant) Validate() error {
	if g.GrantID == uuid.Nil {
		return fmt.Errorf("structure: grant: GrantID is required")
	}
	if !g.SubjectType.Valid() {
		return fmt.Errorf("structure: grant: invalid SubjectType %q", g.SubjectType)
	}
	if g.SubjectID == uuid.Nil {
		return fmt.Errorf("structure: grant: SubjectID is required")
	}
	if !g.TargetType.Valid() {
		return fmt.Errorf("structure: grant: invalid TargetType %q", g.TargetType)
	}

	havePermissionKey := g.PermissionKey != nil && *g.PermissionKey != ""
	haveRoleID := g.RoleID != nil && *g.RoleID != uuid.Nil
	if havePermissionKey == haveRoleID {
		return fmt.Errorf("structure: grant: exactly one of PermissionKey or RoleID must be set")
	}
	if g.TargetType == GrantTargetTypePermission && !havePermissionKey {
		return fmt.Errorf("structure: grant: TargetType permission requires PermissionKey")
	}
	if g.TargetType == GrantTargetTypeRole && !haveRoleID {
		return fmt.Errorf("structure: grant: TargetType role requires RoleID")
	}

	if !g.Scope.Valid() {
		return fmt.Errorf("structure: grant: invalid Scope %q", g.Scope)
	}
	haveContext := g.ContextType != nil && *g.ContextType != "" && g.ContextID != nil && *g.ContextID != ""
	if g.Scope == GrantScopeContext && !haveContext {
		return fmt.Errorf("structure: grant: Scope context requires ContextType and ContextID")
	}
	if g.Scope == GrantScopeGlobal && haveContext {
		return fmt.Errorf("structure: grant: Scope global must not carry a ContextType/ContextID")
	}

	if !g.Effect.Valid() {
		return fmt.Errorf("structure: grant: invalid Effect %q", g.Effect)
	}
	// A role-target grant's own Effect has no independent meaning: the
	// role's RolePermission entries already carry their own effects,
	// and a deny here would be ambiguous (deny the whole role? deny
	// just what it would have allowed?) rather than resolved. Rather
	// than pick an arbitrary answer, this is disallowed outright —
	// Evaluation always expands a role-target grant using the role's
	// own entries as-is.
	if g.TargetType == GrantTargetTypeRole && g.Effect != GrantEffectAllow {
		return fmt.Errorf("structure: grant: a role-target grant's Effect must be allow; the role's own RolePermission entries carry the real effects")
	}
	if !g.Status.Valid() {
		return fmt.Errorf("structure: grant: invalid Status %q", g.Status)
	}
	if !g.Origin.Valid() {
		return fmt.Errorf("structure: grant: invalid Origin %q", g.Origin)
	}
	return nil
}
