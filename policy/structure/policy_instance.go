package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PolicyInstance binds a value to a target — global (the platform-
// wide default), a single Ref, or a PolicyContext (a named set of
// Refs). Exactly one of Ref/PolicyContextID is set, matching
// TargetKind; Validate checks that pairing but never checks Value
// against a PolicyDefinition's ValueType — that cross-struct check
// is Evaluation's job, the same reason Grant.Validate() never checks
// that PermissionKey is actually registered.
type PolicyInstance struct {
	PolicyInstanceID   uuid.UUID
	PolicyDefinitionID uuid.UUID
	TargetKind         TargetKind
	Ref                *Ref       // required iff TargetKind == TargetKindRef
	PolicyContextID    *uuid.UUID // required iff TargetKind == TargetKindPolicyContext
	Value              any
	Binding            BindingMode
	Lifecycle          Lifecycle
	Metadata           json.RawMessage
	CreatedBy          string
	UpdatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (i PolicyInstance) Validate() error {
	if i.PolicyInstanceID == uuid.Nil {
		return fmt.Errorf("structure: policy instance: PolicyInstanceID is required")
	}
	if i.PolicyDefinitionID == uuid.Nil {
		return fmt.Errorf("structure: policy instance: PolicyDefinitionID is required")
	}
	if !i.TargetKind.Valid() {
		return fmt.Errorf("structure: policy instance: invalid TargetKind %q", i.TargetKind)
	}

	haveRef := i.Ref != nil
	havePolicyContext := i.PolicyContextID != nil && *i.PolicyContextID != uuid.Nil
	switch i.TargetKind {
	case TargetKindGlobal:
		if haveRef || havePolicyContext {
			return fmt.Errorf("structure: policy instance: TargetKind global must not carry a Ref or PolicyContextID")
		}
	case TargetKindRef:
		if !haveRef || havePolicyContext {
			return fmt.Errorf("structure: policy instance: TargetKind ref requires Ref and no PolicyContextID")
		}
		if err := i.Ref.Validate(); err != nil {
			return fmt.Errorf("structure: policy instance: %w", err)
		}
	case TargetKindPolicyContext:
		if !havePolicyContext || haveRef {
			return fmt.Errorf("structure: policy instance: TargetKind policy_context requires PolicyContextID and no Ref")
		}
	}

	if i.Value == nil {
		return fmt.Errorf("structure: policy instance: Value is required")
	}
	if !i.Binding.Valid() {
		return fmt.Errorf("structure: policy instance: invalid Binding %q", i.Binding)
	}
	if !i.Lifecycle.Valid() {
		return fmt.Errorf("structure: policy instance: invalid Lifecycle %q", i.Lifecycle)
	}
	return nil
}
