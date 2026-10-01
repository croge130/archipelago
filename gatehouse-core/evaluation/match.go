package evaluation

import (
	"strings"

	"github.com/croge130/archipelago/gatehouse-core/structure"
)

// matchesPermissionKey reports whether pattern (an exact key or a
// wildcard like "myapp.readinglist.*") covers requested. Wildcard
// matching is structural — a prefix check — never an expansion of
// every possible descendant key.
//
// Wildcards never include recovery-access permissions, no exceptions:
// a wildcard pattern simply cannot match a requested key whose own
// PermissionDefinition requires recovery_access, regardless of the
// definition's WildcardIncludable flag. This is the Evaluation-side
// half of the invariant structure.PermissionDefinition.Validate
// already enforces at construction time — defense in depth, not
// either/or.
func matchesPermissionKey(pattern, requested string, def structure.PermissionDefinition) bool {
	if pattern == requested {
		return true
	}
	prefix, isWildcard := strings.CutSuffix(pattern, "*")
	if !isWildcard {
		return false
	}
	if !def.WildcardIncludable {
		return false
	}
	if def.RequiredAuthorityLevel == structure.AuthorityLevelRecoveryAccess {
		return false
	}
	return strings.HasPrefix(requested, prefix)
}

// matchesScope reports whether grant's scope covers req. A global
// grant covers any request for the permission, context-scoped or not
// — holding a permission unconditionally is a superset of holding it
// for any one context. A context-scoped grant only covers a request
// scoped to that exact same context; Context v0 is exact-match only,
// so there's no ancestor/descendant relationship to consider.
func matchesScope(grant structure.Grant, req Request) bool {
	if grant.Scope == structure.GrantScopeGlobal {
		return true
	}
	if req.Scope != ScopeContext {
		return false
	}
	return grant.ContextType != nil && *grant.ContextType == req.ContextType &&
		grant.ContextID != nil && *grant.ContextID == req.ContextID
}
