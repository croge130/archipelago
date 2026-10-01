package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/google/uuid"
)

// EnsurePolicyContext is idempotent by Key: calling it again with an
// already-existing key just returns the existing context unchanged —
// Description is cosmetic, not something a second Ensure call
// conflicts over the way EnsureAlias's Target does.
func EnsurePolicyContext(ctx context.Context, r Reader, w Writer, key, description string) (structure.PolicyContext, error) {
	existing, found, err := r.GetPolicyContextByKey(ctx, key)
	if err != nil {
		return structure.PolicyContext{}, fmt.Errorf("facade: ensure policy context: %w", err)
	}
	if found {
		return existing, nil
	}

	now := time.Now().Truncate(time.Microsecond)
	c := structure.PolicyContext{
		PolicyContextID: uuid.New(),
		Key:             key,
		Description:     description,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := w.CreatePolicyContext(ctx, c); err != nil {
		return structure.PolicyContext{}, fmt.Errorf("facade: ensure policy context: %w", err)
	}
	return c, nil
}

// AddPolicyContextMember is idempotent — adding a Ref that's already
// a member is a no-op, matching Writer.AddPolicyContextMember's own
// ON CONFLICT DO NOTHING.
func AddPolicyContextMember(ctx context.Context, w Writer, policyContextID uuid.UUID, ref structure.Ref) error {
	now := time.Now().Truncate(time.Microsecond)
	if err := w.AddPolicyContextMember(ctx, structure.PolicyContextMember{
		PolicyContextID: policyContextID, Ref: ref, CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("facade: add policy context member: %w", err)
	}
	return nil
}

// RemovePolicyContextMember is idempotent — removing a Ref that was
// never a member is a no-op success, matching the SQL DELETE's own
// zero-rows-affected behavior.
func RemovePolicyContextMember(ctx context.Context, w Writer, policyContextID uuid.UUID, ref structure.Ref) error {
	if err := w.RemovePolicyContextMember(ctx, policyContextID, ref); err != nil {
		return fmt.Errorf("facade: remove policy context member: %w", err)
	}
	return nil
}
