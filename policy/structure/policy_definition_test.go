package structure

import (
	"testing"
	"time"

	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

func maxRequestsDefinition() PolicyDefinition {
	now := time.Now()
	maxVal := 1000.0
	return PolicyDefinition{
		PolicyDefinitionID: uuid.New(),
		PolicyKey:          "myapp.rate_limit.max_requests",
		ValueType:          typedvalue.Count("request", "the rate limit ceiling"),
		Constraints:        typeconstraints.Set{Max: &maxVal},
		Merge:              typeconstraints.MergeMinimum,
		Activation:         ActivationImmediate,
		DefaultBinding:     BindingInheritedLive,
		Lifecycle:          LifecycleActive,
		CreatedBy:          "operator:christian",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestPolicyDefinitionValidateOK(t *testing.T) {
	if err := maxRequestsDefinition().Validate(); err != nil {
		t.Fatalf("expected a well-formed definition to validate, got: %v", err)
	}
}

func TestPolicyDefinitionValidateRejectsMissingKey(t *testing.T) {
	d := maxRequestsDefinition()
	d.PolicyKey = ""
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a missing PolicyKey")
	}
}

func TestPolicyDefinitionValidateRejectsIncoherentValueType(t *testing.T) {
	d := maxRequestsDefinition()
	d.ValueType = typedvalue.Definition{Storage: typedvalue.StorageType("nonsense")}
	if err := d.Validate(); err == nil {
		t.Fatal("expected an invalid ValueType to fail definition validation")
	}
}

func TestPolicyDefinitionValidateRejectsIncoherentConstraints(t *testing.T) {
	d := maxRequestsDefinition()
	d.ValueType = typedvalue.Bool("a flag")
	// Max (numeric) against a bool-typed value — exactly the
	// pairing typeconstraints.Set.Validate exists to reject.
	if err := d.Validate(); err == nil {
		t.Fatal("expected Max against a bool ValueType to fail")
	}
}

func TestPolicyDefinitionValidateRejectsIncoherentMergeMode(t *testing.T) {
	d := maxRequestsDefinition()
	d.Merge = typeconstraints.MergeBooleanAnd // requires bool; ValueType here is an int (count)
	if err := d.Validate(); err == nil {
		t.Fatal("expected boolean_and against a count-typed value to fail")
	}
}

func TestPolicyDefinitionValidateRejectsBadEnums(t *testing.T) {
	d := maxRequestsDefinition()
	d.Activation = ActivationMode("sometimes")
	if err := d.Validate(); err == nil {
		t.Fatal("expected an invalid Activation to fail")
	}
	d = maxRequestsDefinition()
	d.DefaultBinding = BindingMode("whenever")
	if err := d.Validate(); err == nil {
		t.Fatal("expected an invalid DefaultBinding to fail")
	}
	d = maxRequestsDefinition()
	d.Lifecycle = Lifecycle("deleted")
	if err := d.Validate(); err == nil {
		t.Fatal("expected an invalid Lifecycle to fail")
	}
}
