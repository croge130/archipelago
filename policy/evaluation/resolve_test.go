package evaluation

import (
	"context"
	"testing"
	"time"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

const maxRequestsKey = "myapp.rate_limit.max_requests"

func maxRequestsDef(mode typeconstraints.MergeMode) structure.PolicyDefinition {
	now := time.Now()
	return structure.PolicyDefinition{
		PolicyDefinitionID: uuid.New(),
		PolicyKey:          maxRequestsKey,
		ValueType:          typedvalue.Count("request", ""),
		Merge:              mode,
		Activation:         structure.ActivationImmediate,
		DefaultBinding:     structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func newInstance(defID uuid.UUID, kind structure.TargetKind, value any) structure.PolicyInstance {
	now := time.Now()
	return structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: defID,
		TargetKind:         kind,
		Value:              value,
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestResolveUnknownKeyIsAnError(t *testing.T) {
	store := newMemStore()
	_, _, err := Resolve(context.Background(), store, "nonexistent.key", nil)
	if err == nil {
		t.Fatal("expected an error for an unregistered policy key")
	}
}

func TestResolveNoApplicableValueIsNotAnError(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeMinimum)
	store.definitions[def.PolicyKey] = def

	res, found, err := Resolve(context.Background(), store, def.PolicyKey, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if found {
		t.Fatalf("expected found=false with no global default and no override, got resolution %+v", res)
	}
}

func TestResolveGlobalOnly(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeMinimum)
	store.definitions[def.PolicyKey] = def
	global := newInstance(def.PolicyDefinitionID, structure.TargetKindGlobal, int64(1000))
	store.instances = append(store.instances, global)

	res, found, err := Resolve(context.Background(), store, def.PolicyKey, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(1000) {
		t.Fatalf("Resolve = (%+v, %v), want value 1000", res, found)
	}
	if len(res.ContributingInstanceIDs) != 1 || res.ContributingInstanceIDs[0] != global.PolicyInstanceID {
		t.Fatalf("expected the global instance to be the sole contributor, got %v", res.ContributingInstanceIDs)
	}
}

func TestResolveRefOverrideCombinesWithGlobal(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeMinimum)
	store.definitions[def.PolicyKey] = def
	store.instances = append(store.instances,
		newInstance(def.PolicyDefinitionID, structure.TargetKindGlobal, int64(1000)),
	)
	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	refInst := newInstance(def.PolicyDefinitionID, structure.TargetKindRef, int64(200))
	refInst.Ref = &ref
	store.instances = append(store.instances, refInst)

	res, found, err := Resolve(context.Background(), store, def.PolicyKey, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(200) {
		t.Fatalf("Resolve = (%+v, %v), want minimum(1000, 200) = 200", res, found)
	}
	if len(res.ContributingInstanceIDs) != 2 {
		t.Fatalf("expected both the global and ref instance to contribute, got %v", res.ContributingInstanceIDs)
	}
}

func TestResolveViaPolicyContextMembership(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeMinimum)
	store.definitions[def.PolicyKey] = def

	ctxID := uuid.New()
	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	store.members = append(store.members, structure.PolicyContextMember{PolicyContextID: ctxID, Ref: ref})

	ctxInst := newInstance(def.PolicyDefinitionID, structure.TargetKindPolicyContext, int64(50))
	ctxInst.PolicyContextID = &ctxID
	store.instances = append(store.instances, ctxInst)

	res, found, err := Resolve(context.Background(), store, def.PolicyKey, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(50) {
		t.Fatalf("Resolve = (%+v, %v), want 50 via policy context membership", res, found)
	}
}

func TestResolveIgnoresArchivedInstances(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeMinimum)
	store.definitions[def.PolicyKey] = def
	global := newInstance(def.PolicyDefinitionID, structure.TargetKindGlobal, int64(1000))
	store.instances = append(store.instances, global)

	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	archived := newInstance(def.PolicyDefinitionID, structure.TargetKindRef, int64(1))
	archived.Ref = &ref
	archived.Lifecycle = structure.LifecycleArchived
	store.instances = append(store.instances, archived)

	res, found, err := Resolve(context.Background(), store, def.PolicyKey, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(1000) {
		t.Fatalf("Resolve = (%+v, %v), want the global value since the ref override is archived", res, found)
	}
}

func TestResolveRejectsMultipleOverridesUnderNonCommutativeMode(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeOverride)
	store.definitions[def.PolicyKey] = def

	refA := structure.Ref{Kind: "service", Key: "gamebridge"}
	refB := structure.Ref{Kind: "group", Key: "internet-facing"}
	instA := newInstance(def.PolicyDefinitionID, structure.TargetKindRef, int64(10))
	instA.Ref = &refA
	instB := newInstance(def.PolicyDefinitionID, structure.TargetKindRef, int64(20))
	instB.Ref = &refB
	store.instances = append(store.instances, instA, instB)

	_, _, err := Resolve(context.Background(), store, def.PolicyKey, []structure.Ref{refA, refB})
	if err == nil {
		t.Fatal("expected an error when two overrides simultaneously apply under a non-commutative merge_mode")
	}
}

func TestResolveOverrideModeSingleOverrideWins(t *testing.T) {
	store := newMemStore()
	def := maxRequestsDef(typeconstraints.MergeOverride)
	store.definitions[def.PolicyKey] = def
	store.instances = append(store.instances,
		newInstance(def.PolicyDefinitionID, structure.TargetKindGlobal, int64(1000)),
	)
	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	refInst := newInstance(def.PolicyDefinitionID, structure.TargetKindRef, int64(5000))
	refInst.Ref = &ref
	store.instances = append(store.instances, refInst)

	res, found, err := Resolve(context.Background(), store, def.PolicyKey, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(5000) {
		t.Fatalf("Resolve = (%+v, %v), want the single override (5000) to win outright under override mode", res, found)
	}
}
