package structure

import "fmt"

// PermissionDefinition is deliberately minimal — only the two fields
// Authority level and wildcard matching need a home for. Lighthouse's
// richer shape (risk class, audit policy, evaluation path, break-glass)
// isn't decided here; see the model doc's "What stays explicitly
// deferred" list.
type PermissionDefinition struct {
	PermissionKey          string
	RequiredAuthorityLevel AuthorityLevel
	WildcardIncludable     bool
}

// Validate rejects a PermissionDefinition that could never be
// satisfied consistently: wildcards never include recovery-access
// permissions, no exceptions, so a definition can't claim both at
// once. This is the Structure-level half of that invariant; Evaluation
// must also enforce it independently when matching a wildcard grant,
// rather than trusting this flag alone — defense in depth, not either/or.
func (p PermissionDefinition) Validate() error {
	if p.PermissionKey == "" {
		return fmt.Errorf("structure: permission definition: PermissionKey is required")
	}
	if !p.RequiredAuthorityLevel.Valid() {
		return fmt.Errorf("structure: permission definition: invalid RequiredAuthorityLevel %q", p.RequiredAuthorityLevel)
	}
	if p.WildcardIncludable && p.RequiredAuthorityLevel == AuthorityLevelRecoveryAccess {
		return fmt.Errorf("structure: permission definition: wildcards never include recovery-access permissions, WildcardIncludable and RequiredAuthorityLevel=recovery_access are mutually exclusive")
	}
	return nil
}
