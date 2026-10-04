package vitalsauth

import (
	"context"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// EnsureDefinition checks principalID holds PermissionDefinitionManage
// globally — a Definition carries no scope field at all (ownership of
// a DefinitionKey is the reserved-namespace convention, per
// 14-vitals-model.md), so there's no (ScopeType, ScopeID) pair to
// scope this check to — then calls through to
// vitals/facade.EnsureDefinition unchanged.
func EnsureDefinition(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, vitalsWriter vitalsFacade.Writer, principalID uuid.UUID, d structure.Definition) (structure.Definition, error) {
	if err := evaluation.RequirePermission(ctx, gatehouseStore, principalID, PermissionDefinitionManage); err != nil {
		return structure.Definition{}, err
	}
	return vitalsFacade.EnsureDefinition(ctx, vitalsReader, vitalsWriter, d)
}
