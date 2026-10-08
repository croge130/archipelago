package evaluation

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// candidate is one (grant, resolved pattern/effect) pair under
// consideration — either a direct permission grant, or one entry from
// expanding a role-target grant. Grant is kept alongside so
// matchesScope can check it, and GrantID so a Decision can name which
// grant actually produced the outcome.
type candidate struct {
	GrantID uuid.UUID
	Grant   structure.Grant
	Pattern string
	Effect  structure.GrantEffect
}

// Evaluate is the one chokepoint every privileged operation goes
// through, per the model doc's Evaluation facade section. It never
// reimplements matching, deny precedence, or authority-level checking
// anywhere else — this is the only place that logic exists.
func Evaluate(ctx context.Context, store Store, req Request) (Decision, error) {
	def, found, err := store.GetPermissionDefinition(ctx, req.PermissionKey)
	if err != nil {
		return Decision{}, fmt.Errorf("evaluation: get permission definition: %w", err)
	}
	if !found {
		return Decision{Allowed: false, Reason: DenialReasonNoMatchingPermission}, nil
	}

	candidates, err := gatherCandidates(ctx, store, req.PrincipalID)
	if err != nil {
		return Decision{}, fmt.Errorf("evaluation: gather candidates: %w", err)
	}

	var denyMatch, allowMatch *candidate
	for i := range candidates {
		c := &candidates[i]
		if !matchesScope(c.Grant, req) {
			continue
		}
		if !matchesPermissionKey(c.Pattern, req.PermissionKey, def) {
			continue
		}
		switch c.Effect {
		case structure.GrantEffectDeny:
			if denyMatch == nil {
				denyMatch = c
			}
		case structure.GrantEffectAllow:
			if allowMatch == nil {
				allowMatch = c
			}
		}
	}

	// Deny always wins, unconditionally, no specificity comparison —
	// the model doc's Deny section, adopted from AWS IAM's own rule.
	if denyMatch != nil {
		id := denyMatch.GrantID
		return Decision{Allowed: false, Reason: DenialReasonDenyGrantMatched, MatchedGrantID: &id}, nil
	}

	if allowMatch == nil {
		return Decision{Allowed: false, Reason: DenialReasonNoMatchingGrant}, nil
	}

	id := allowMatch.GrantID
	// "Who's been granted this" and "is the session elevated enough to
	// use it" are two genuinely separate checks — folding authority
	// level into grant matching would reintroduce the graduated-
	// specificity problem deny-always-wins exists specifically to avoid.
	if !structure.AuthorityLevel(req.AuthorityLevel).Meets(def.RequiredAuthorityLevel) {
		return Decision{Allowed: false, Reason: DenialReasonAuthorityLevelInsufficient, MatchedGrantID: &id}, nil
	}

	return Decision{Allowed: true, MatchedGrantID: &id}, nil
}

// ErrDenied is wrapped by Require when the evaluator says no, so a caller
// can tell a denial from a store failure with errors.Is. The message is
// unchanged: "evaluation: denied: <reason>".
var ErrDenied = errors.New("evaluation: denied")

// Require is Evaluate plus "return an error if not allowed" — a
// convenience wrapper, never a second evaluator, same as any other
// facade in this design.
func Require(ctx context.Context, store Store, req Request) error {
	decision, err := Evaluate(ctx, store, req)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", ErrDenied, decision.Reason)
	}
	return nil
}

// gatherCandidates collects every (grant, pattern, effect) tuple that
// could possibly apply to principalID — its own direct grants, the
// grants held by every group it belongs to, and role-target grants
// expanded into their own entries. It applies no filtering of its own;
// Evaluate does all the matching.
func gatherCandidates(ctx context.Context, store Store, principalID uuid.UUID) ([]candidate, error) {
	var grants []structure.Grant

	direct, err := store.ActiveGrantsForSubject(ctx, structure.GrantSubjectTypePrincipal, principalID)
	if err != nil {
		return nil, fmt.Errorf("direct grants: %w", err)
	}
	grants = append(grants, direct...)

	groupIDs, err := store.GroupIDsForPrincipal(ctx, principalID)
	if err != nil {
		return nil, fmt.Errorf("group memberships: %w", err)
	}
	for _, gid := range groupIDs {
		groupGrants, err := store.ActiveGrantsForSubject(ctx, structure.GrantSubjectTypeGroup, gid)
		if err != nil {
			return nil, fmt.Errorf("group grants for %s: %w", gid, err)
		}
		grants = append(grants, groupGrants...)
	}

	var candidates []candidate
	for _, g := range grants {
		switch g.TargetType {
		case structure.GrantTargetTypePermission:
			candidates = append(candidates, candidate{
				GrantID: g.GrantID,
				Grant:   g,
				Pattern: *g.PermissionKey,
				Effect:  g.Effect,
			})
		case structure.GrantTargetTypeRole:
			entries, err := expandRole(ctx, store, *g.RoleID)
			if err != nil {
				return nil, fmt.Errorf("expand role %s: %w", *g.RoleID, err)
			}
			for _, e := range entries {
				candidates = append(candidates, candidate{
					GrantID: g.GrantID,
					Grant:   g,
					Pattern: e.PermissionPattern,
					Effect:  e.Effect,
				})
			}
		}
	}
	return candidates, nil
}
