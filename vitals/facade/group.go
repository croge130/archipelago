package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// EnsureGroup registers g, idempotent by (ScopeType, ScopeID,
// GroupKey) — same shape as EnsureInstance.
func EnsureGroup(ctx context.Context, reader Reader, writer Writer, g structure.Group) (structure.Group, error) {
	existing, found, err := reader.GetGroupByScopeKey(ctx, g.ScopeType, g.ScopeID, g.GroupKey)
	if err != nil {
		return structure.Group{}, fmt.Errorf("facade: ensure group: %w", err)
	}
	if found {
		return existing, nil
	}

	now := time.Now().Truncate(time.Microsecond)
	g.GroupID = uuid.New()
	g.CreatedAt = now
	g.UpdatedAt = now
	if err := g.Validate(); err != nil {
		return structure.Group{}, fmt.Errorf("facade: ensure group: %w", err)
	}
	if err := writer.CreateGroup(ctx, g); err != nil {
		return structure.Group{}, fmt.Errorf("facade: ensure group: %w", err)
	}
	return g, nil
}

// SetGroupMember sets one member of a group, including the recursive
// cycle check UpsertGroupMember performs when the member is itself a
// group — exposed here so callers go through facade consistently
// rather than reaching into storage/dbstore directly.
func SetGroupMember(ctx context.Context, writer Writer, m structure.GroupMember) error {
	if err := writer.UpsertGroupMember(ctx, m); err != nil {
		return fmt.Errorf("facade: set group member: %w", err)
	}
	return nil
}

// RemoveGroupMember removes one member — unconditionally successful
// whether or not it existed, per UpsertGroupMember's/DeleteGroupMember's
// own idempotent-release shape.
func RemoveGroupMember(ctx context.Context, writer Writer, groupID uuid.UUID, memberKey string) error {
	if err := writer.DeleteGroupMember(ctx, groupID, memberKey); err != nil {
		return fmt.Errorf("facade: remove group member: %w", err)
	}
	return nil
}
