package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Role is a live bundle of permissions, with inheritance. Evaluation
// flattens a role's own permissions plus everything inherited into one
// pool before resolving, applying the same deny-always-wins rule Grant
// evaluation uses — never a separate precedence mechanism for role
// conflicts specifically.
type Role struct {
	RoleID      uuid.UUID
	Key         string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r Role) Validate() error {
	if r.RoleID == uuid.Nil {
		return fmt.Errorf("structure: role: RoleID is required")
	}
	if r.Key == "" {
		return fmt.Errorf("structure: role: Key is required")
	}
	return nil
}

// RolePermission is either a direct permission grant on the role, or
// (when ChildRoleID is set instead of PermissionKey) an inheritance
// edge to another role. Effect exists here for the same reason it
// exists on Grant: a role's bundle isn't purely additive once deny is
// real — a role can inherit a broad allow set and still explicitly
// deny one permission from it.
type RolePermission struct {
	RoleID        uuid.UUID
	PermissionKey *string
	ChildRoleID   *uuid.UUID
	Effect        GrantEffect
}

func (p RolePermission) Validate() error {
	if p.RoleID == uuid.Nil {
		return fmt.Errorf("structure: role permission: RoleID is required")
	}
	havePermissionKey := p.PermissionKey != nil && *p.PermissionKey != ""
	haveChildRoleID := p.ChildRoleID != nil && *p.ChildRoleID != uuid.Nil
	if havePermissionKey == haveChildRoleID {
		return fmt.Errorf("structure: role permission: exactly one of PermissionKey or ChildRoleID must be set")
	}
	if haveChildRoleID && *p.ChildRoleID == p.RoleID {
		return fmt.Errorf("structure: role permission: ChildRoleID cannot reference the same role (direct self-inheritance)")
	}
	if !p.Effect.Valid() {
		return fmt.Errorf("structure: role permission: invalid Effect %q", p.Effect)
	}
	return nil
}

// IsInheritanceEdge reports whether this row is an inheritance edge to
// another role rather than a direct permission entry.
func (p RolePermission) IsInheritanceEdge() bool {
	return p.ChildRoleID != nil
}

// Group is a live bundle of principals. Membership has no Effect of
// its own — a principal either is or isn't a member.
type Group struct {
	GroupID     uuid.UUID
	Key         string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (g Group) Validate() error {
	if g.GroupID == uuid.Nil {
		return fmt.Errorf("structure: group: GroupID is required")
	}
	if g.Key == "" {
		return fmt.Errorf("structure: group: Key is required")
	}
	return nil
}

type GroupMembership struct {
	GroupID     uuid.UUID
	PrincipalID uuid.UUID
	CreatedAt   time.Time
}

func (m GroupMembership) Validate() error {
	if m.GroupID == uuid.Nil {
		return fmt.Errorf("structure: group membership: GroupID is required")
	}
	if m.PrincipalID == uuid.Nil {
		return fmt.Errorf("structure: group membership: PrincipalID is required")
	}
	return nil
}

// Template is an explicitly-applied creation recipe — never live
// authority. Changing a template never silently changes what it
// already created; Recipe's exact shape (what operations it actually
// describes) isn't decided beyond "opaque data applied explicitly,"
// which is as far as the model doc goes.
type Template struct {
	TemplateID  uuid.UUID
	Key         string
	Description string
	Recipe      json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (t Template) Validate() error {
	if t.TemplateID == uuid.Nil {
		return fmt.Errorf("structure: template: TemplateID is required")
	}
	if t.Key == "" {
		return fmt.Errorf("structure: template: Key is required")
	}
	return nil
}
