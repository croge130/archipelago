package evaluation

import (
	"context"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/google/uuid"
)

// Store is everything Resolve needs to read. Defined here, in the
// consuming package, rather than in structure or storage — the same
// rule every other base in this design follows.
type Store interface {
	// GetPolicyDefinition looks up a registered policy key. found is
	// false if the key isn't registered at all.
	GetPolicyDefinition(ctx context.Context, policyKey string) (def structure.PolicyDefinition, found bool, err error)

	// GlobalInstance returns the platform-wide default instance for a
	// definition, if one has been set.
	GlobalInstance(ctx context.Context, policyDefinitionID uuid.UUID) (instance structure.PolicyInstance, found bool, err error)

	// RefInstances returns every instance directly targeting any of
	// the given refs, for one definition.
	RefInstances(ctx context.Context, policyDefinitionID uuid.UUID, refs []structure.Ref) ([]structure.PolicyInstance, error)

	// PolicyContextIDsForRefs returns every PolicyContext any of the
	// given refs is a member of, deduplicated.
	PolicyContextIDsForRefs(ctx context.Context, refs []structure.Ref) ([]uuid.UUID, error)

	// PolicyContextInstances returns every instance targeting any of
	// the given policy contexts, for one definition.
	PolicyContextInstances(ctx context.Context, policyDefinitionID uuid.UUID, policyContextIDs []uuid.UUID) ([]structure.PolicyInstance, error)
}
