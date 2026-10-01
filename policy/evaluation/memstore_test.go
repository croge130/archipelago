package evaluation

import (
	"context"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/google/uuid"
)

// memStore is an in-memory fake Store, the same role memStore/
// memstore_test.go fakes play for every other base's evaluation
// tests in this design — pure logic, no DB needed to exercise it.
type memStore struct {
	definitions map[string]structure.PolicyDefinition
	instances   []structure.PolicyInstance
	members     []structure.PolicyContextMember
}

func newMemStore() *memStore {
	return &memStore{definitions: map[string]structure.PolicyDefinition{}}
}

func (m *memStore) GetPolicyDefinition(_ context.Context, policyKey string) (structure.PolicyDefinition, bool, error) {
	d, ok := m.definitions[policyKey]
	return d, ok, nil
}

func (m *memStore) GlobalInstance(_ context.Context, policyDefinitionID uuid.UUID) (structure.PolicyInstance, bool, error) {
	for _, inst := range m.instances {
		if inst.PolicyDefinitionID == policyDefinitionID && inst.TargetKind == structure.TargetKindGlobal {
			return inst, true, nil
		}
	}
	return structure.PolicyInstance{}, false, nil
}

func (m *memStore) RefInstances(_ context.Context, policyDefinitionID uuid.UUID, refs []structure.Ref) ([]structure.PolicyInstance, error) {
	var out []structure.PolicyInstance
	for _, inst := range m.instances {
		if inst.PolicyDefinitionID != policyDefinitionID || inst.TargetKind != structure.TargetKindRef {
			continue
		}
		for _, r := range refs {
			if *inst.Ref == r {
				out = append(out, inst)
				break
			}
		}
	}
	return out, nil
}

func (m *memStore) PolicyContextIDsForRefs(_ context.Context, refs []structure.Ref) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, mem := range m.members {
		for _, r := range refs {
			if mem.Ref == r && !seen[mem.PolicyContextID] {
				seen[mem.PolicyContextID] = true
				out = append(out, mem.PolicyContextID)
			}
		}
	}
	return out, nil
}

func (m *memStore) PolicyContextInstances(_ context.Context, policyDefinitionID uuid.UUID, policyContextIDs []uuid.UUID) ([]structure.PolicyInstance, error) {
	var out []structure.PolicyInstance
	for _, inst := range m.instances {
		if inst.PolicyDefinitionID != policyDefinitionID || inst.TargetKind != structure.TargetKindPolicyContext {
			continue
		}
		for _, id := range policyContextIDs {
			if *inst.PolicyContextID == id {
				out = append(out, inst)
				break
			}
		}
	}
	return out, nil
}
