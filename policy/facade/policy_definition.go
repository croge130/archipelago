package facade

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

// ErrConflict is returned by EnsurePolicyDefinition when policyKey
// already names a definition with a different shape. "Ensure" means
// idempotently create-if-absent, never silently redefine an existing
// key's meaning — the same rule EnsureAlias already follows for
// targets.
var ErrConflict = errors.New("facade: policy definition exists with a different shape")

// ErrNotFound is returned when an operation names a definition,
// instance, or policy context that was never created.
var ErrNotFound = errors.New("facade: not found")

// EnsurePolicyDefinition is idempotent by PolicyKey: calling it again
// with the identical shape is a no-op; calling it with a different
// ValueType/Constraints/Merge/Activation/DefaultBinding is ErrConflict,
// not a silent redefinition.
//
// Two known limitations, named rather than hidden:
//   - Re-registering under a previously archived key is also treated
//     as ErrConflict for now rather than reactivation — nothing yet
//     needs the reactivation case, so it isn't built.
//   - Same shape as EnsurePrincipal's own: this is a read then a
//     write, not an atomic upsert, so two concurrent first-time
//     registrations of the same brand-new key can race.
func EnsurePolicyDefinition(
	ctx context.Context, r Reader, w Writer,
	policyKey string,
	valueType typedvalue.Definition,
	constraints typeconstraints.Set,
	merge typeconstraints.MergeMode,
	activation structure.ActivationMode,
	defaultBinding structure.BindingMode,
	actor string,
) (structure.PolicyDefinition, error) {
	existing, found, err := r.GetPolicyDefinition(ctx, policyKey)
	if err != nil {
		return structure.PolicyDefinition{}, fmt.Errorf("facade: ensure policy definition: %w", err)
	}
	if found {
		if sameShape(existing, valueType, constraints, merge, activation, defaultBinding) {
			return existing, nil
		}
		return structure.PolicyDefinition{}, ErrConflict
	}

	now := time.Now().Truncate(time.Microsecond)
	def := structure.PolicyDefinition{
		PolicyDefinitionID: uuid.New(),
		PolicyKey:          policyKey,
		ValueType:          valueType,
		Constraints:        constraints,
		Merge:              merge,
		Activation:         activation,
		DefaultBinding:     defaultBinding,
		Lifecycle:          structure.LifecycleActive,
		CreatedBy:          actor,
		UpdatedBy:          actor,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := w.CreatePolicyDefinition(ctx, def); err != nil {
		return structure.PolicyDefinition{}, fmt.Errorf("facade: ensure policy definition: %w", err)
	}
	return def, nil
}

func sameShape(
	existing structure.PolicyDefinition,
	valueType typedvalue.Definition,
	constraints typeconstraints.Set,
	merge typeconstraints.MergeMode,
	activation structure.ActivationMode,
	defaultBinding structure.BindingMode,
) bool {
	return existing.Lifecycle == structure.LifecycleActive &&
		existing.Merge == merge &&
		existing.Activation == activation &&
		existing.DefaultBinding == defaultBinding &&
		reflect.DeepEqual(existing.ValueType, valueType) &&
		reflect.DeepEqual(existing.Constraints, constraints)
}
