package evaluation

import "github.com/google/uuid"

// Request is what Evaluate checks. Scope mirrors Grant's own two-way
// scope: a Global request asks "does this principal hold the
// permission unconditionally," a Context request asks "does this
// principal hold it for this exact resource instance."
type Request struct {
	PrincipalID    uuid.UUID
	PermissionKey  string
	Scope          Scope
	ContextType    string // required iff Scope == ScopeContext
	ContextID      string // required iff Scope == ScopeContext
	AuthorityLevel AuthorityLevel
}

type Scope int

const (
	ScopeGlobal Scope = iota
	ScopeContext
)

// AuthorityLevel mirrors structure.AuthorityLevel; Request takes its
// own copy rather than importing structure's constants directly so a
// caller building a Request doesn't need to reach into structure just
// to say "standard" — Evaluate converts internally.
type AuthorityLevel string

const (
	AuthorityLevelStandard       AuthorityLevel = "standard"
	AuthorityLevelElevated       AuthorityLevel = "elevated"
	AuthorityLevelRecoveryAccess AuthorityLevel = "recovery_access"
)

// DenialReason names why a Decision denied, per the model doc's
// explainability section — specific, not generic.
type DenialReason string

const (
	DenialReasonNone                       DenialReason = ""
	DenialReasonNoMatchingPermission       DenialReason = "no_matching_permission"
	DenialReasonDenyGrantMatched           DenialReason = "deny_grant_matched"
	DenialReasonNoMatchingGrant            DenialReason = "no_matching_grant"
	DenialReasonAuthorityLevelInsufficient DenialReason = "authority_level_insufficient"
)

// Decision is Evaluate's result. MatchedGrantID, when set, names the
// grant that actually produced the outcome (allow or deny) — the thing
// the model doc's Deny section says falls out "for free": a denial
// names the deny grant that matched the same way an allow names the
// grant that matched.
type Decision struct {
	Allowed        bool
	Reason         DenialReason
	MatchedGrantID *uuid.UUID
}
