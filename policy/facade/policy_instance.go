package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

// SetPolicyInstance creates or updates the value for one target
// (global, a Ref, or a PolicyContext) — idempotent by target: calling
// it again for the same exact target updates that instance's value in
// place rather than creating a redundant second one.
//
// Non-commutative merge modes (override, object_merge) get a
// conservative, definition-wide exclusivity rule rather than a true
// membership-overlap check: creating a brand-new non-global instance
// is rejected outright if *any* other active non-global instance
// already exists for the same definition, regardless of whether their
// Refs/PolicyContexts could ever actually overlap. This can reject a
// configuration that would in fact never be ambiguous (two override
// instances whose Ref sets never intersect) — a deliberate, documented
// simplification, the same "simpler until a real case proves
// otherwise" choice this design makes elsewhere (one global generation
// counter instead of per-principal, for instance). Relaxing it into a
// real overlap check is a real feature to build later if this
// restriction ever actually bites someone, not a bug to fix now.
//
// The exclusivity check itself runs through
// Writer.CreatePolicyInstanceExclusive, not a separate read-then-write
// here — see that method's own doc comment for the concurrency bug a
// naive check-then-create would reintroduce. Updating an *existing*
// instance's value (the branch just below) stays an ordinary read-
// then-write: last-write-wins under a genuine race there is benign
// (no invariant to violate, just whichever update actually lands),
// unlike creating a second instance where one shouldn't exist at all.
//
// clamp controls what happens when value fails def.Constraints: false
// rejects it outright; true coerces a numeric value into bounds via
// Constraints.Clamp instead (see typeconstraints.Set.Clamp).
func SetPolicyInstance(
	ctx context.Context, r Reader, w Writer,
	def structure.PolicyDefinition,
	targetKind structure.TargetKind,
	ref *structure.Ref,
	policyContextID *uuid.UUID,
	value any,
	binding structure.BindingMode,
	actor string,
	clamp bool,
) (structure.PolicyInstance, error) {
	normalized, err := typedvalue.NormalizeValue(def.ValueType, value)
	if err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
	}
	if def.ValueType.Storage == typedvalue.StorageEnum {
		if err := typedvalue.ValidateValue(def.ValueType, normalized); err != nil {
			return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
		}
	}
	if err := def.Constraints.Check(normalized); err != nil {
		if !clamp {
			return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
		}
		clamped, wasClamped, clampErr := def.Constraints.Clamp(normalized)
		if clampErr != nil {
			return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", clampErr)
		}
		if wasClamped {
			normalized = clamped
		}
	}

	existing, err := findExistingInstance(ctx, r, def.PolicyDefinitionID, targetKind, ref, policyContextID)
	if err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
	}

	now := time.Now().Truncate(time.Microsecond)

	if existing != nil {
		inst := *existing
		inst.Value = normalized
		inst.Binding = binding
		inst.UpdatedBy = actor
		inst.UpdatedAt = now
		if err := w.UpdatePolicyInstance(ctx, inst); err != nil {
			return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
		}
		return inst, nil
	}

	inst := structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         targetKind,
		Ref:                ref,
		PolicyContextID:    policyContextID,
		Value:              normalized,
		Binding:            binding,
		Lifecycle:          structure.LifecycleActive,
		CreatedBy:          actor,
		UpdatedBy:          actor,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// Non-commutative merge modes go through the atomic exclusive-create
	// path — a separate "check others, then create" here would be the
	// exact TOCTOU race UpsertGroupMember's own cycle check had to be
	// hardened against: two concurrent creates for different new
	// targets under the same definition could each see "no others
	// exist" against the other's pre-commit state and both succeed.
	if targetKind != structure.TargetKindGlobal && !def.Merge.Commutative() {
		ok, err := w.CreatePolicyInstanceExclusive(ctx, inst)
		if err != nil {
			return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
		}
		if !ok {
			return structure.PolicyInstance{}, fmt.Errorf(
				"facade: set policy instance: merge_mode %q is non-commutative and another active override already exists for this definition; archive it first",
				def.Merge,
			)
		}
		return inst, nil
	}

	if err := w.CreatePolicyInstance(ctx, inst); err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("facade: set policy instance: %w", err)
	}
	return inst, nil
}

// ArchivePolicyInstance retires an instance — the global default
// reverting to unset, a Ref/PolicyContext override no longer applying
// — so Resolve stops seeing it immediately, per the model doc's
// "caches never override revocation" invariant.
func ArchivePolicyInstance(ctx context.Context, r Reader, w Writer, instanceID uuid.UUID, actor string) (structure.PolicyInstance, error) {
	inst, found, err := r.GetPolicyInstance(ctx, instanceID)
	if err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("facade: archive policy instance: %w", err)
	}
	if !found {
		return structure.PolicyInstance{}, ErrNotFound
	}

	inst.Lifecycle = structure.LifecycleArchived
	inst.UpdatedBy = actor
	inst.UpdatedAt = time.Now().Truncate(time.Microsecond)
	if err := w.UpdatePolicyInstance(ctx, inst); err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("facade: archive policy instance: %w", err)
	}
	return inst, nil
}

func findExistingInstance(
	ctx context.Context, r Reader,
	defID uuid.UUID,
	targetKind structure.TargetKind,
	ref *structure.Ref,
	policyContextID *uuid.UUID,
) (*structure.PolicyInstance, error) {
	switch targetKind {
	case structure.TargetKindGlobal:
		inst, found, err := r.GlobalInstance(ctx, defID)
		if err != nil || !found {
			return nil, err
		}
		return &inst, nil
	case structure.TargetKindRef:
		if ref == nil {
			return nil, fmt.Errorf("facade: TargetKind ref requires a Ref")
		}
		instances, err := r.RefInstances(ctx, defID, []structure.Ref{*ref})
		if err != nil || len(instances) == 0 {
			return nil, err
		}
		return &instances[0], nil
	case structure.TargetKindPolicyContext:
		if policyContextID == nil {
			return nil, fmt.Errorf("facade: TargetKind policy_context requires a PolicyContextID")
		}
		instances, err := r.PolicyContextInstances(ctx, defID, []uuid.UUID{*policyContextID})
		if err != nil || len(instances) == 0 {
			return nil, err
		}
		return &instances[0], nil
	default:
		return nil, fmt.Errorf("facade: invalid TargetKind %q", targetKind)
	}
}
