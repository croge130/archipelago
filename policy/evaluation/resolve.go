package evaluation

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/google/uuid"
)

// Resolution is Resolve's result — rich enough to debug a surprising
// value, the same reasoning behind every other Decision-shaped type
// in this design: ContributingInstanceIDs names exactly which
// instances fed the merge, in the order they were merged.
type Resolution struct {
	Value                   any
	Definition              structure.PolicyDefinition
	ContributingInstanceIDs []uuid.UUID
}

// Resolve is the one chokepoint every policy/config read goes
// through. It takes a set of Refs rather than one — see the package
// doc — and combines whatever applies (the global default, any
// instance targeting a Ref directly, any instance targeting a
// PolicyContext any Ref belongs to) with the definition's own
// MergeMode, never ranking sources by "direct beats group" or "group
// beats global": that graduated-specificity trap is the one Grant's
// deny-always-wins rule already sidestepped once in this design, and
// merge_mode's own math sidesteps it again here.
//
// found is false when the key is registered but nothing — no global
// default, no override — currently applies; that's a legitimate
// state (nobody's set a default yet), not an error. An unregistered
// policyKey is an error, not found=false, since asking for a key that
// was never defined at all is a caller mistake, not a normal "nothing
// here yet" outcome.
func Resolve(ctx context.Context, store Store, policyKey string, refs []structure.Ref) (Resolution, bool, error) {
	def, found, err := store.GetPolicyDefinition(ctx, policyKey)
	if err != nil {
		return Resolution{}, false, fmt.Errorf("evaluation: resolve: get policy definition: %w", err)
	}
	if !found {
		return Resolution{}, false, fmt.Errorf("evaluation: resolve: unknown policy key %q", policyKey)
	}

	var globalValue any
	var globalID *uuid.UUID
	if global, found, err := store.GlobalInstance(ctx, def.PolicyDefinitionID); err != nil {
		return Resolution{}, false, fmt.Errorf("evaluation: resolve: global instance: %w", err)
	} else if found && global.Lifecycle == structure.LifecycleActive {
		globalValue = global.Value
		id := global.PolicyInstanceID
		globalID = &id
	}

	var overrideValues []any
	var overrideIDs []uuid.UUID

	if len(refs) > 0 {
		refInstances, err := store.RefInstances(ctx, def.PolicyDefinitionID, refs)
		if err != nil {
			return Resolution{}, false, fmt.Errorf("evaluation: resolve: ref instances: %w", err)
		}
		for _, inst := range refInstances {
			if inst.Lifecycle != structure.LifecycleActive {
				continue
			}
			overrideValues = append(overrideValues, inst.Value)
			overrideIDs = append(overrideIDs, inst.PolicyInstanceID)
		}

		contextIDs, err := store.PolicyContextIDsForRefs(ctx, refs)
		if err != nil {
			return Resolution{}, false, fmt.Errorf("evaluation: resolve: policy context ids: %w", err)
		}
		if len(contextIDs) > 0 {
			contextInstances, err := store.PolicyContextInstances(ctx, def.PolicyDefinitionID, contextIDs)
			if err != nil {
				return Resolution{}, false, fmt.Errorf("evaluation: resolve: policy context instances: %w", err)
			}
			for _, inst := range contextInstances {
				if inst.Lifecycle != structure.LifecycleActive {
					continue
				}
				overrideValues = append(overrideValues, inst.Value)
				overrideIDs = append(overrideIDs, inst.PolicyInstanceID)
			}
		}
	}

	// Non-commutative modes (override, object_merge) must never see
	// more than one applicable override here — Policy's write path
	// enforces that exclusivity at the source; this is
	// belt-and-suspenders, catching a violation rather than silently
	// picking a winner the way a precedence rule would.
	if !def.Merge.Commutative() && len(overrideValues) > 1 {
		return Resolution{}, false, fmt.Errorf(
			"evaluation: resolve: merge_mode %q is non-commutative but %d overrides apply simultaneously — write-time exclusivity was violated",
			def.Merge, len(overrideValues),
		)
	}

	var values []any
	var contributing []uuid.UUID
	if globalValue != nil {
		values = append(values, globalValue)
		contributing = append(contributing, *globalID)
	}
	values = append(values, overrideValues...)
	contributing = append(contributing, overrideIDs...)

	if len(values) == 0 {
		return Resolution{Definition: def}, false, nil
	}

	merged, err := typeconstraints.Merge(def.Merge, def.ValueType, values...)
	if err != nil {
		return Resolution{}, false, fmt.Errorf("evaluation: resolve: merge: %w", err)
	}

	return Resolution{Value: merged, Definition: def, ContributingInstanceIDs: contributing}, true, nil
}
