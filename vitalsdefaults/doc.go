// Package vitalsdefaults is the Vitals + Policy integration named in
// docs/architecture/14-vitals-model.md: resolving a scope's default
// Vitals group via a Policy pointer, reusing the exact same
// (ScopeType, ScopeID) pair vitalsauth already reuses as a Gatehouse-
// core Context — here instead as a Policy Ref{Kind: ScopeType, Key:
// ScopeID}. One Policy definition (vitals.default_group) replaces
// Lighthouse's own four separate scope-kind-specific policy keys,
// since Archipelago's scope isn't a closed enum with four cases to
// give each its own key — it's an open pair, and Policy's own
// Resolve already takes a set of Refs.
//
// The resolved value is a full (ScopeType, ScopeID, GroupKey) triple,
// not a bare GroupKey string: the archipelago-wide fallback (the
// global PolicyInstance, no Ref) has no inherent scope of its own to
// borrow a GroupKey's meaning from, so it has to name the group it
// points to completely, the same way a scope-specific override could
// — though in practice usually doesn't need to — point at a group
// living in some other scope entirely. override merge_mode (not a
// commutative one) is the right choice for the same reason it's right
// for a Lease's holder: "the default" has no meaning for more than
// one simultaneous answer.
//
// Resolution order, per the model doc: an explicit UUID/key lookup
// (a caller's own direct GetGroup/GetGroupByScopeKey call) always
// wins and is checked before this package is reached at all; Policy's
// own override-merge math then picks a scope-specific instance over
// the global fallback for free (see 10-typedvalue-and-policy-model.md
// — Merge orders values least-specific first and override returns the
// last one), never something this package ranks itself. found=false
// means no default is configured for this scope at all — a valid
// state, not an error, the same as Policy's own Resolve already
// treats it.
package vitalsdefaults
