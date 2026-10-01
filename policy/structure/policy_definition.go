package structure

import (
	"fmt"
	"time"

	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

// PolicyDefinition describes one named, typed, constrained value —
// serving config and security policy as the same mechanism, per
// 10-typedvalue-and-policy-model.md's scope correction. There is no
// separate "default value" field: the platform-wide default is an
// ordinary PolicyInstance with TargetKind = global, one mechanism for
// "what applies absent an override" rather than two places a default
// could live.
type PolicyDefinition struct {
	PolicyDefinitionID uuid.UUID
	PolicyKey          string // namespaced, e.g. myapp.rate_limit.max_requests
	ValueType          typedvalue.Definition
	Constraints        typeconstraints.Set
	Merge              typeconstraints.MergeMode
	Activation         ActivationMode
	DefaultBinding     BindingMode
	Lifecycle          Lifecycle
	CreatedBy          string // opaque identifier; Policy has no principal model of its own
	UpdatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (d PolicyDefinition) Validate() error {
	if d.PolicyDefinitionID == uuid.Nil {
		return fmt.Errorf("structure: policy definition: PolicyDefinitionID is required")
	}
	if d.PolicyKey == "" {
		return fmt.Errorf("structure: policy definition: PolicyKey is required")
	}
	if err := d.ValueType.Validate(); err != nil {
		return fmt.Errorf("structure: policy definition: value type: %w", err)
	}
	if err := d.Constraints.Validate(d.ValueType); err != nil {
		return fmt.Errorf("structure: policy definition: constraints: %w", err)
	}
	if !d.Merge.Valid() {
		return fmt.Errorf("structure: policy definition: invalid Merge %q", d.Merge)
	}
	if err := d.Merge.ValidateForType(d.ValueType); err != nil {
		return fmt.Errorf("structure: policy definition: %w", err)
	}
	if !d.Activation.Valid() {
		return fmt.Errorf("structure: policy definition: invalid Activation %q", d.Activation)
	}
	if !d.DefaultBinding.Valid() {
		return fmt.Errorf("structure: policy definition: invalid DefaultBinding %q", d.DefaultBinding)
	}
	if !d.Lifecycle.Valid() {
		return fmt.Errorf("structure: policy definition: invalid Lifecycle %q", d.Lifecycle)
	}
	return nil
}
