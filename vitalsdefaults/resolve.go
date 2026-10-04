package vitalsdefaults

import (
	"context"
	"fmt"

	policyEvaluation "github.com/croge130/archipelago/policy/evaluation"
	policyFacade "github.com/croge130/archipelago/policy/facade"
	policyStructure "github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
)

// PolicyKeyDefaultGroup is the one Policy definition this package
// resolves against — see doc.go for why there's only one, not
// Lighthouse's own four scope-kind-specific keys.
const PolicyKeyDefaultGroup = "vitals.default_group"

// ValueType is the shape every vitals.default_group PolicyInstance's
// Value takes: a full (ScopeType, ScopeID, GroupKey) triple, never a
// bare GroupKey — see doc.go for why the global fallback specifically
// needs this.
func ValueType() typedvalue.Definition {
	return typedvalue.Definition{
		Storage: typedvalue.StorageObject,
		Fields: map[string]typedvalue.Definition{
			"scope_type": {Storage: typedvalue.StorageString},
			"scope_id":   {Storage: typedvalue.StorageString},
			"group_key":  {Storage: typedvalue.StorageString},
		},
	}
}

// EnsurePolicyDefinition registers the vitals.default_group policy
// definition — override merge mode, since "the default" has no
// meaning for more than one simultaneous answer. Idempotent via
// policyFacade.EnsurePolicyDefinition, safe to call on every app
// startup.
func EnsurePolicyDefinition(ctx context.Context, policyReader policyFacade.Reader, policyWriter policyFacade.Writer, actor string) (policyStructure.PolicyDefinition, error) {
	def, err := policyFacade.EnsurePolicyDefinition(ctx, policyReader, policyWriter,
		PolicyKeyDefaultGroup, ValueType(), typeconstraints.Set{}, typeconstraints.MergeOverride,
		policyStructure.ActivationImmediate, policyStructure.BindingInheritedLive, actor,
	)
	if err != nil {
		return policyStructure.PolicyDefinition{}, fmt.Errorf("vitalsdefaults: ensure policy definition: %w", err)
	}
	return def, nil
}

func defaultGroupValue(groupScopeType, groupScopeID, groupKey string) any {
	return map[string]any{"scope_type": groupScopeType, "scope_id": groupScopeID, "group_key": groupKey}
}

// SetGlobalDefault points the archipelago-wide fallback default at
// the group (groupScopeType, groupScopeID, groupKey) — used when no
// scope-specific override applies. Idempotent by target, per
// policyFacade.SetPolicyInstance's own shape.
func SetGlobalDefault(ctx context.Context, policyReader policyFacade.Reader, policyWriter policyFacade.Writer, def policyStructure.PolicyDefinition, groupScopeType, groupScopeID, groupKey, actor string) (policyStructure.PolicyInstance, error) {
	inst, err := policyFacade.SetPolicyInstance(ctx, policyReader, policyWriter, def, policyStructure.TargetKindGlobal, nil, nil,
		defaultGroupValue(groupScopeType, groupScopeID, groupKey), policyStructure.BindingInheritedLive, actor, false)
	if err != nil {
		return policyStructure.PolicyInstance{}, fmt.Errorf("vitalsdefaults: set global default: %w", err)
	}
	return inst, nil
}

// SetScopedDefault points forScopeType/forScopeID's own default at the
// group (groupScopeType, groupScopeID, groupKey) — usually, but not
// required to be, the same scope. Overrides the archipelago-wide
// fallback for this one scope only.
func SetScopedDefault(ctx context.Context, policyReader policyFacade.Reader, policyWriter policyFacade.Writer, def policyStructure.PolicyDefinition, forScopeType, forScopeID, groupScopeType, groupScopeID, groupKey, actor string) (policyStructure.PolicyInstance, error) {
	ref := &policyStructure.Ref{Kind: forScopeType, Key: forScopeID}
	inst, err := policyFacade.SetPolicyInstance(ctx, policyReader, policyWriter, def, policyStructure.TargetKindRef, ref, nil,
		defaultGroupValue(groupScopeType, groupScopeID, groupKey), policyStructure.BindingInheritedLive, actor, false)
	if err != nil {
		return policyStructure.PolicyInstance{}, fmt.Errorf("vitalsdefaults: set scoped default: %w", err)
	}
	return inst, nil
}

// ResolveDefaultGroup resolves the default Vitals group for
// scopeType/scopeID. A caller that already has an explicit GroupID or
// (scopeType, scopeID, groupKey) in hand should look it up directly
// and never reach this function at all — this is purely the
// policy-driven fallback path, per the model doc's resolution order.
// found=false means no default is configured for this scope at all —
// a valid state, not an error.
func ResolveDefaultGroup(ctx context.Context, policyStore policyEvaluation.Store, vitalsReader vitalsFacade.Reader, scopeType, scopeID string) (structure.Group, bool, error) {
	refs := []policyStructure.Ref{{Kind: scopeType, Key: scopeID}}
	res, found, err := policyEvaluation.Resolve(ctx, policyStore, PolicyKeyDefaultGroup, refs)
	if err != nil {
		return structure.Group{}, false, fmt.Errorf("vitalsdefaults: resolve default group: %w", err)
	}
	if !found {
		return structure.Group{}, false, nil
	}

	value, ok := res.Value.(map[string]any)
	if !ok {
		return structure.Group{}, false, fmt.Errorf("vitalsdefaults: resolve default group: unexpected value shape %T", res.Value)
	}
	groupScopeType, _ := value["scope_type"].(string)
	groupScopeID, _ := value["scope_id"].(string)
	groupKey, _ := value["group_key"].(string)
	if groupScopeType == "" || groupScopeID == "" || groupKey == "" {
		return structure.Group{}, false, nil
	}

	return vitalsReader.GetGroupByScopeKey(ctx, groupScopeType, groupScopeID, groupKey)
}
