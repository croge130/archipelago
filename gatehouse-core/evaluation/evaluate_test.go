package evaluation

import (
	"context"
	"testing"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }

func globalPermissionGrant(subjectType structure.GrantSubjectType, subjectID uuid.UUID, key string, effect structure.GrantEffect) structure.Grant {
	return structure.Grant{
		GrantID:       uuid.New(),
		SubjectType:   subjectType,
		SubjectID:     subjectID,
		TargetType:    structure.GrantTargetTypePermission,
		PermissionKey: strPtr(key),
		Scope:         structure.GrantScopeGlobal,
		Effect:        effect,
		Status:        structure.GrantStatusActive,
		Origin:        structure.GrantOriginManual,
	}
}

func contextPermissionGrant(subjectType structure.GrantSubjectType, subjectID uuid.UUID, key, ctxType, ctxID string, effect structure.GrantEffect) structure.Grant {
	return structure.Grant{
		GrantID:       uuid.New(),
		SubjectType:   subjectType,
		SubjectID:     subjectID,
		TargetType:    structure.GrantTargetTypePermission,
		PermissionKey: strPtr(key),
		Scope:         structure.GrantScopeContext,
		ContextType:   strPtr(ctxType),
		ContextID:     strPtr(ctxID),
		Effect:        effect,
		Status:        structure.GrantStatusActive,
		Origin:        structure.GrantOriginManual,
	}
}

func standardPermission(key string) structure.PermissionDefinition {
	return structure.PermissionDefinition{PermissionKey: key, RequiredAuthorityLevel: structure.AuthorityLevelStandard}
}

func TestEvaluateExactAllow(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allow, got deny with reason %q", decision.Reason)
	}
	if decision.MatchedGrantID == nil {
		t.Error("expected MatchedGrantID to be set on an allow decision")
	}
}

func TestEvaluateExactDeny(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.delete"))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.delete", structure.GrantEffectDeny))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.delete",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected deny")
	}
	if decision.Reason != DenialReasonDenyGrantMatched {
		t.Errorf("Reason = %q, want %q", decision.Reason, DenialReasonDenyGrantMatched)
	}
}

func TestEvaluateDenyAlwaysWinsOverAllow(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", structure.GrantEffectAllow))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", structure.GrantEffectDeny))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected deny to win over allow, unconditionally, no specificity comparison")
	}
	if decision.Reason != DenialReasonDenyGrantMatched {
		t.Errorf("Reason = %q, want %q", decision.Reason, DenialReasonDenyGrantMatched)
	}
}

func TestEvaluateWildcardMatch(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.read",
		RequiredAuthorityLevel: structure.AuthorityLevelStandard,
		WildcardIncludable:     true,
	})
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.*", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected the wildcard grant to match, got deny with reason %q", decision.Reason)
	}
}

func TestEvaluateWildcardNotIncludable(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.read",
		RequiredAuthorityLevel: structure.AuthorityLevelStandard,
		WildcardIncludable:     false,
	})
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.*", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected the wildcard grant NOT to match a permission that isn't wildcard-includable")
	}
}

// TestEvaluateWildcardExcludesRecoveryAccess exercises Evaluation's own
// defense-in-depth exclusion directly, by constructing a
// PermissionDefinition that structure.PermissionDefinition.Validate
// would already reject — Evaluate must not trust WildcardIncludable
// blindly even if something upstream failed to call Validate.
func TestEvaluateWildcardExcludesRecoveryAccess(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.permissions["myapp.admin.wipe_everything"] = structure.PermissionDefinition{
		PermissionKey:          "myapp.admin.wipe_everything",
		RequiredAuthorityLevel: structure.AuthorityLevelRecoveryAccess,
		WildcardIncludable:     true, // deliberately invalid combination, bypassing Validate
	}
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.admin.*", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.admin.wipe_everything",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelRecoveryAccess,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("wildcards must never include recovery-access permissions, even if WildcardIncludable incorrectly says true")
	}
}

func TestEvaluateContextScopeExactMatch(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(contextPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", "myapp.readinglist", "42", structure.GrantEffectAllow))

	allowed, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeContext, ContextType: "myapp.readinglist", ContextID: "42",
		AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("expected the exact context to match, got deny with reason %q", allowed.Reason)
	}

	denied, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeContext, ContextType: "myapp.readinglist", ContextID: "99",
		AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if denied.Allowed {
		t.Fatal("a grant scoped to context 42 must not satisfy a request for context 99 — exact match only, no ancestor coverage")
	}
}

func TestEvaluateGlobalGrantCoversContextRequest(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeContext, ContextType: "myapp.readinglist", ContextID: "42",
		AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatal("a global grant should cover any context-scoped request for the same permission")
	}
}

func TestEvaluateContextGrantDoesNotCoverGlobalRequest(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(contextPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", "myapp.readinglist", "42", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("a context-scoped grant must not satisfy a global request — holding it for one context isn't holding it everywhere")
	}
}

func TestEvaluateGroupSourcedGrant(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	group := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGroupMember(group, principal)
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypeGroup, group, "myapp.readinglist.read", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected a group-sourced grant to apply, got deny with reason %q", decision.Reason)
	}
}

func TestEvaluateAuthorityLevelInsufficient(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.delete",
		RequiredAuthorityLevel: structure.AuthorityLevelElevated,
	})
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.delete", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.delete",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected deny: the session's authority level doesn't meet the permission's required minimum")
	}
	if decision.Reason != DenialReasonAuthorityLevelInsufficient {
		t.Errorf("Reason = %q, want %q", decision.Reason, DenialReasonAuthorityLevelInsufficient)
	}
	if decision.MatchedGrantID == nil {
		t.Error("expected MatchedGrantID still set, naming the grant that would have matched")
	}
}

func TestEvaluateAuthorityLevelElevatedSatisfiesElevatedRequirement(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.delete",
		RequiredAuthorityLevel: structure.AuthorityLevelElevated,
	})
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.delete", structure.GrantEffectAllow))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.delete",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelElevated,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allow once the session meets the required level, got deny with reason %q", decision.Reason)
	}
}

func TestEvaluateNoMatchingGrant(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.readinglist.read",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected deny: no grant at all")
	}
	if decision.Reason != DenialReasonNoMatchingGrant {
		t.Errorf("Reason = %q, want %q", decision.Reason, DenialReasonNoMatchingGrant)
	}
}

func TestEvaluateNoMatchingPermission(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()

	decision, err := Evaluate(context.Background(), store, Request{
		PrincipalID: principal, PermissionKey: "myapp.nonexistent.key",
		Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected deny: permission isn't registered at all")
	}
	if decision.Reason != DenialReasonNoMatchingPermission {
		t.Errorf("Reason = %q, want %q", decision.Reason, DenialReasonNoMatchingPermission)
	}
}

func TestRequireWrapsEvaluate(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", structure.GrantEffectAllow))

	allowedReq := Request{PrincipalID: principal, PermissionKey: "myapp.readinglist.read", Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard}
	if err := Require(context.Background(), store, allowedReq); err != nil {
		t.Fatalf("expected Require to return nil for an allowed request, got: %v", err)
	}

	deniedReq := Request{PrincipalID: principal, PermissionKey: "myapp.readinglist.delete", Scope: ScopeGlobal, AuthorityLevel: AuthorityLevelStandard}
	store.addPermission(standardPermission("myapp.readinglist.delete"))
	if err := Require(context.Background(), store, deniedReq); err == nil {
		t.Fatal("expected Require to return an error for a denied request")
	}
}
