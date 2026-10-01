package structure

import (
	"testing"

	"github.com/google/uuid"
)

func TestRoleValidate(t *testing.T) {
	r := Role{RoleID: uuid.New(), Key: "support-agent"}
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid role to validate, got: %v", err)
	}
}

func TestRolePermissionDirectEntry(t *testing.T) {
	key := "myapp.readinglist.delete"
	p := RolePermission{
		RoleID:        uuid.New(),
		PermissionKey: &key,
		Effect:        GrantEffectDeny,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid direct role permission to validate, got: %v", err)
	}
	if p.IsInheritanceEdge() {
		t.Error("a direct permission entry should not report as an inheritance edge")
	}
}

func TestRolePermissionInheritanceEdge(t *testing.T) {
	childID := uuid.New()
	p := RolePermission{
		RoleID:      uuid.New(),
		ChildRoleID: &childID,
		Effect:      GrantEffectAllow,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid inheritance edge to validate, got: %v", err)
	}
	if !p.IsInheritanceEdge() {
		t.Error("an entry with ChildRoleID set should report as an inheritance edge")
	}
}

func TestRolePermissionRejectsBothOrNeither(t *testing.T) {
	roleID := uuid.New()
	p := RolePermission{RoleID: roleID, Effect: GrantEffectAllow}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error when neither PermissionKey nor ChildRoleID is set")
	}
	key := "myapp.x"
	childID := uuid.New()
	p.PermissionKey = &key
	p.ChildRoleID = &childID
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error when both PermissionKey and ChildRoleID are set")
	}
}

func TestRolePermissionRejectsSelfInheritance(t *testing.T) {
	roleID := uuid.New()
	p := RolePermission{RoleID: roleID, ChildRoleID: &roleID, Effect: GrantEffectAllow}
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for a role inheriting from itself directly")
	}
}

func TestGroupValidate(t *testing.T) {
	g := Group{GroupID: uuid.New(), Key: "admins"}
	if err := g.Validate(); err != nil {
		t.Fatalf("expected valid group to validate, got: %v", err)
	}
}

func TestGroupMembershipValidate(t *testing.T) {
	m := GroupMembership{GroupID: uuid.New(), PrincipalID: uuid.New()}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid membership to validate, got: %v", err)
	}
}

func TestTemplateValidate(t *testing.T) {
	tmpl := Template{TemplateID: uuid.New(), Key: "bootstrap-app"}
	if err := tmpl.Validate(); err != nil {
		t.Fatalf("expected valid template to validate, got: %v", err)
	}
}
