package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func baseInstance() PolicyInstance {
	now := time.Now()
	return PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: uuid.New(),
		TargetKind:         TargetKindGlobal,
		Value:              int64(1000),
		Binding:            BindingInheritedLive,
		Lifecycle:          LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestPolicyInstanceValidateGlobal(t *testing.T) {
	if err := baseInstance().Validate(); err != nil {
		t.Fatalf("expected a valid global instance to validate, got: %v", err)
	}
}

func TestPolicyInstanceValidateGlobalRejectsRefOrContext(t *testing.T) {
	i := baseInstance()
	i.Ref = &Ref{Kind: "service", Key: "gamebridge"}
	if err := i.Validate(); err == nil {
		t.Fatal("expected a global-target instance carrying a Ref to fail")
	}

	i = baseInstance()
	id := uuid.New()
	i.PolicyContextID = &id
	if err := i.Validate(); err == nil {
		t.Fatal("expected a global-target instance carrying a PolicyContextID to fail")
	}
}

func TestPolicyInstanceValidateRef(t *testing.T) {
	i := baseInstance()
	i.TargetKind = TargetKindRef
	i.Ref = &Ref{Kind: "service", Key: "gamebridge"}
	if err := i.Validate(); err != nil {
		t.Fatalf("expected a valid ref-target instance to validate, got: %v", err)
	}
}

func TestPolicyInstanceValidateRefRequiresRef(t *testing.T) {
	i := baseInstance()
	i.TargetKind = TargetKindRef
	if err := i.Validate(); err == nil {
		t.Fatal("expected a ref-target instance with no Ref to fail")
	}
}

func TestPolicyInstanceValidateRefRejectsInvalidRef(t *testing.T) {
	i := baseInstance()
	i.TargetKind = TargetKindRef
	i.Ref = &Ref{Kind: "service"} // missing Key
	if err := i.Validate(); err == nil {
		t.Fatal("expected an invalid Ref to fail instance validation")
	}
}

func TestPolicyInstanceValidatePolicyContext(t *testing.T) {
	i := baseInstance()
	i.TargetKind = TargetKindPolicyContext
	id := uuid.New()
	i.PolicyContextID = &id
	if err := i.Validate(); err != nil {
		t.Fatalf("expected a valid policy-context-target instance to validate, got: %v", err)
	}
}

func TestPolicyInstanceValidatePolicyContextRequiresID(t *testing.T) {
	i := baseInstance()
	i.TargetKind = TargetKindPolicyContext
	if err := i.Validate(); err == nil {
		t.Fatal("expected a policy-context-target instance with no PolicyContextID to fail")
	}
}

func TestPolicyInstanceValidatePolicyContextRejectsAlsoCarryingRef(t *testing.T) {
	i := baseInstance()
	i.TargetKind = TargetKindPolicyContext
	id := uuid.New()
	i.PolicyContextID = &id
	i.Ref = &Ref{Kind: "service", Key: "gamebridge"}
	if err := i.Validate(); err == nil {
		t.Fatal("expected an instance carrying both a PolicyContextID and a Ref to fail")
	}
}

func TestPolicyInstanceValidateRequiresValue(t *testing.T) {
	i := baseInstance()
	i.Value = nil
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing Value")
	}
}

func TestPolicyInstanceValidateRejectsBadEnums(t *testing.T) {
	i := baseInstance()
	i.Binding = BindingMode("whenever")
	if err := i.Validate(); err == nil {
		t.Fatal("expected an invalid Binding to fail")
	}
	i = baseInstance()
	i.Lifecycle = Lifecycle("deleted")
	if err := i.Validate(); err == nil {
		t.Fatal("expected an invalid Lifecycle to fail")
	}
}
