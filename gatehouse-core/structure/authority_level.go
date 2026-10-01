package structure

// AuthorityLevel is the active posture of a session or request,
// independent of containment — used by both Session (what level a
// session is currently operating at) and PermissionDefinition (the
// minimum level a permission requires).
type AuthorityLevel string

const (
	AuthorityLevelStandard AuthorityLevel = "standard"
	AuthorityLevelElevated AuthorityLevel = "elevated"
	// AuthorityLevelRecoveryAccess is reserved vocabulary: the value
	// exists so nothing has to be renamed later, but the recovery-
	// activation mechanism itself is undesigned and deferred.
	AuthorityLevelRecoveryAccess AuthorityLevel = "recovery_access"
)

func (l AuthorityLevel) Valid() bool {
	switch l {
	case AuthorityLevelStandard, AuthorityLevelElevated, AuthorityLevelRecoveryAccess:
		return true
	default:
		return false
	}
}

// rank gives AuthorityLevel a total order so Evaluation can compare
// "does the session meet this permission's required minimum" — standard
// < elevated < recovery_access.
func (l AuthorityLevel) rank() int {
	switch l {
	case AuthorityLevelStandard:
		return 0
	case AuthorityLevelElevated:
		return 1
	case AuthorityLevelRecoveryAccess:
		return 2
	default:
		return -1
	}
}

// Meets reports whether l satisfies a required minimum level — e.g.
// an elevated session Meets a standard requirement, but a standard
// session does not Meet an elevated requirement.
func (l AuthorityLevel) Meets(required AuthorityLevel) bool {
	return l.rank() >= required.rank()
}
