package facade

import (
	"context"

	"github.com/croge130/archipelago/policy/evaluation"
	"github.com/croge130/archipelago/policy/structure"
	"github.com/google/uuid"
)

// Reader is everything the facade needs to read — evaluation.Store
// plus the extra lookups facade's own write-side logic needs that a
// pure resolver never does.
type Reader interface {
	evaluation.Store

	GetPolicyInstance(ctx context.Context, id uuid.UUID) (structure.PolicyInstance, bool, error)
	GetPolicyContext(ctx context.Context, id uuid.UUID) (structure.PolicyContext, bool, error)
	GetPolicyContextByKey(ctx context.Context, key string) (structure.PolicyContext, bool, error)
	ListPolicyContextMembers(ctx context.Context, policyContextID uuid.UUID) ([]structure.Ref, error)

	// ActiveNonGlobalInstances backs the conservative, definition-wide
	// exclusivity rule SetPolicyInstance enforces for non-commutative
	// merge modes — see that function's own doc comment.
	ActiveNonGlobalInstances(ctx context.Context, policyDefinitionID uuid.UUID) ([]structure.PolicyInstance, error)
}

// Writer is what the facade needs to persist — raw creates/updates
// with no idempotency or exclusivity logic of its own; that logic is
// this package's, which decides the desired row state before calling
// these.
type Writer interface {
	CreatePolicyDefinition(ctx context.Context, d structure.PolicyDefinition) error
	CreatePolicyContext(ctx context.Context, c structure.PolicyContext) error
	AddPolicyContextMember(ctx context.Context, m structure.PolicyContextMember) error
	RemovePolicyContextMember(ctx context.Context, policyContextID uuid.UUID, ref structure.Ref) error
	CreatePolicyInstance(ctx context.Context, i structure.PolicyInstance) error
	UpdatePolicyInstance(ctx context.Context, i structure.PolicyInstance) error
}
