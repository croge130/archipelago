package evaluation

import (
	"context"
	"testing"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

func roleTargetGrant(subjectType structure.GrantSubjectType, subjectID, roleID uuid.UUID) structure.Grant {
	return structure.Grant{
		GrantID:     uuid.New(),
		SubjectType: subjectType,
		SubjectID:   subjectID,
		TargetType:  structure.GrantTargetTypeRole,
		RoleID:      &roleID,
		Scope:       structure.GrantScopeGlobal,
		Effect:      structure.GrantEffectAllow,
		Status:      structure.GrantStatusActive,
		Origin:      structure.GrantOriginManual,
	}
}

func TestEvaluateRoleExpansionDirect(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	role := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addRolePermission(structure.RolePermission{RoleID: role, PermissionKey: strPtr("myapp.readinglist.read"), Effect: structure.GrantEffectAllow})
	store.addGrant(roleTargetGrant(structure.GrantSubjectTypePrincipal, principal, role))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected the role's direct permission to apply, got deny with reason %q", decision.Reason)
	}
}

func TestEvaluateRoleExpansionInherited(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	employee, supportAgent := uuid.New(), uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addRolePermission(structure.RolePermission{RoleID: employee, PermissionKey: strPtr("myapp.readinglist.read"), Effect: structure.GrantEffectAllow})
	// supportAgent inherits employee, with no direct permissions of its own.
	store.addRolePermission(structure.RolePermission{RoleID: supportAgent, ChildRoleID: &employee, Effect: structure.GrantEffectAllow})
	store.addGrant(roleTargetGrant(structure.GrantSubjectTypePrincipal, principal, supportAgent))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected the inherited permission to apply, got deny with reason %q", decision.Reason)
	}
}

func TestEvaluateRoleExpansionDenyWithinInheritedRole(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	employee, supportAgent := uuid.New(), uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addPermission(standardPermission("myapp.billing.view"))
	// employee grants both; support-agent inherits employee but
	// explicitly denies billing — the role-section scenario from the
	// model doc.
	store.addRolePermission(structure.RolePermission{RoleID: employee, PermissionKey: strPtr("myapp.readinglist.read"), Effect: structure.GrantEffectAllow})
	store.addRolePermission(structure.RolePermission{RoleID: employee, PermissionKey: strPtr("myapp.billing.view"), Effect: structure.GrantEffectAllow})
	store.addRolePermission(structure.RolePermission{RoleID: supportAgent, ChildRoleID: &employee, Effect: structure.GrantEffectAllow})
	store.addRolePermission(structure.RolePermission{RoleID: supportAgent, PermissionKey: strPtr("myapp.billing.view"), Effect: structure.GrantEffectDeny})
	store.addGrant(roleTargetGrant(structure.GrantSubjectTypePrincipal, principal, supportAgent))

	readDecision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !readDecision.Allowed {
		t.Fatal("expected the inherited read permission to still be allowed")
	}

	billingDecision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.billing.view",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if billingDecision.Allowed {
		t.Fatal("expected support-agent's explicit deny to override the inherited allow — one flattened pool, deny always wins")
	}
}

func TestEvaluateRoleExpansionCycleDoesNotInfiniteLoop(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	roleA, roleB := uuid.New(), uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	// roleA inherits roleB; roleB inherits roleA back — a cycle.
	store.addRolePermission(structure.RolePermission{RoleID: roleA, ChildRoleID: &roleB, Effect: structure.GrantEffectAllow})
	store.addRolePermission(structure.RolePermission{RoleID: roleA, PermissionKey: strPtr("myapp.readinglist.read"), Effect: structure.GrantEffectAllow})
	store.addRolePermission(structure.RolePermission{RoleID: roleB, ChildRoleID: &roleA, Effect: structure.GrantEffectAllow})
	store.addGrant(roleTargetGrant(structure.GrantSubjectTypePrincipal, principal, roleA))

	done := make(chan struct{})
	var decision Decision
	var evalErr error
	go func() {
		decision, evalErr = Evaluate(context.Background(), store, Request{
			PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
			Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("role expansion did not terminate — cycle detection failed")
	}
	if evalErr != nil {
		t.Fatalf("Evaluate: %v", evalErr)
	}
	if !decision.Allowed {
		t.Fatalf("expected the directly-held permission to still apply despite the cycle, got deny with reason %q", decision.Reason)
	}
}
