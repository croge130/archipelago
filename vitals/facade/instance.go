package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// EnsureInstance registers i, idempotent by (ScopeType, ScopeID,
// InstanceKey): a second call for the same scope+key returns the
// existing instance unchanged — this function doesn't attempt to
// reconcile a changed DefinitionID or display fields on an existing
// instance; that's a deliberate v0 narrowing, not an oversight (an app
// that needs to actually change an instance's shape does so through a
// future dedicated update path, not a silent side effect of Ensure).
func EnsureInstance(ctx context.Context, reader Reader, writer Writer, i structure.Instance) (structure.Instance, error) {
	existing, found, err := reader.GetInstanceByScopeKey(ctx, i.ScopeType, i.ScopeID, i.InstanceKey)
	if err != nil {
		return structure.Instance{}, fmt.Errorf("facade: ensure instance: %w", err)
	}
	if found {
		return existing, nil
	}

	now := time.Now().Truncate(time.Microsecond)
	i.InstanceID = uuid.New()
	i.CreatedAt = now
	i.UpdatedAt = now
	if err := i.Validate(); err != nil {
		return structure.Instance{}, fmt.Errorf("facade: ensure instance: %w", err)
	}
	if err := writer.CreateInstance(ctx, i); err != nil {
		return structure.Instance{}, fmt.Errorf("facade: ensure instance: %w", err)
	}
	return i, nil
}
