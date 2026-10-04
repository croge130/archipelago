package vitalsauth

import (
	"context"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// EnsureInstance checks principalID holds PermissionWrite scoped to
// i's own (ScopeType, ScopeID) — the scope the caller is declaring for
// the instance it's about to create, checked before creation rather
// than after — then calls through to vitals/facade.EnsureInstance
// unchanged. Registering an instance is treated as a write to its own
// scope, not a separate permission: there's no meaningful distinction
// here between "may write readings for this scope" and "may register
// what reports them."
func EnsureInstance(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, vitalsWriter vitalsFacade.Writer, principalID uuid.UUID, i structure.Instance) (structure.Instance, error) {
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionWrite, i.ScopeType, i.ScopeID); err != nil {
		return structure.Instance{}, err
	}
	return vitalsFacade.EnsureInstance(ctx, vitalsReader, vitalsWriter, i)
}
