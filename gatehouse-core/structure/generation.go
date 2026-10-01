package structure

import "time"

// AuthorityGeneration is two monotonic counters, not Lighthouse's three
// — policy_generation belongs to the Policy base, tracked there;
// Gatehouse-core never imports it. principal_grant_generation is one
// invariant covering direct grant changes, group membership changes,
// and role changes (permissions or inheritance edges) uniformly — see
// the model doc for why that's stated as one rule rather than three
// separate bump points.
type AuthorityGeneration struct {
	PermissionSchemaGeneration int64
	PrincipalGrantGeneration   int64
	GeneratedAt                time.Time
}

// Freshness compares a generation snapshot against the current one.
// Caches never override revocation: this only ever tells a caller
// whether a cached decision's generation stamp is stale, it never
// itself decides whether a cached decision may still be honored —
// that's Evaluation's call, and the model doc's standing invariant is
// that a revocation always takes effect immediately regardless of what
// this reports.
type Freshness struct {
	SchemaStale bool
	SchemaGap   int64
	GrantStale  bool
	GrantGap    int64
}

func (g AuthorityGeneration) CompareTo(current AuthorityGeneration) Freshness {
	schemaGap := current.PermissionSchemaGeneration - g.PermissionSchemaGeneration
	grantGap := current.PrincipalGrantGeneration - g.PrincipalGrantGeneration
	return Freshness{
		SchemaStale: schemaGap > 0,
		SchemaGap:   schemaGap,
		GrantStale:  grantGap > 0,
		GrantGap:    grantGap,
	}
}

// Stale reports whether either counter has advanced.
func (f Freshness) Stale() bool {
	return f.SchemaStale || f.GrantStale
}
