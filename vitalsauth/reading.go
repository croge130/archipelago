package vitalsauth

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// ErrInstanceNotFound mirrors vitalsFacade.ErrInstanceNotFound — this
// package's own lookup (to learn the instance's scope before checking
// permission) can hit the same "no such instance" case independently
// of the base facade call that follows.
var ErrInstanceNotFound = vitalsFacade.ErrInstanceNotFound

// WriteReading checks principalID holds PermissionWrite scoped to
// reading.InstanceID's own (ScopeType, ScopeID), then calls through to
// vitals/facade.WriteReading unchanged. Looking up the instance here,
// before vitalsFacade.WriteReading does its own identical lookup
// internally, is the same acceptable duplication aliasauth's own
// CreateAlias/ResolveAlias/ReleaseAlias already accept: a caller must
// learn what it's checking permission against before it can check
// permission against it.
func WriteReading(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, vitalsWriter vitalsFacade.Writer, principalID uuid.UUID, reading structure.Reading) (structure.Reading, error) {
	instance, found, err := vitalsReader.GetInstance(ctx, reading.InstanceID)
	if err != nil {
		return structure.Reading{}, fmt.Errorf("vitalsauth: write reading: %w", err)
	}
	if !found {
		return structure.Reading{}, ErrInstanceNotFound
	}
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionWrite, instance.ScopeType, instance.ScopeID); err != nil {
		return structure.Reading{}, err
	}
	return vitalsFacade.WriteReading(ctx, vitalsReader, vitalsWriter, reading)
}

// GetReading checks principalID holds PermissionRead scoped to
// instanceID's own (ScopeType, ScopeID) before returning its current
// reading — denied up front, so a principal with no read access to an
// instance's scope learns nothing about whether a reading exists for
// it at all.
func GetReading(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, principalID uuid.UUID, instanceID uuid.UUID) (structure.Reading, bool, error) {
	instance, found, err := vitalsReader.GetInstance(ctx, instanceID)
	if err != nil {
		return structure.Reading{}, false, fmt.Errorf("vitalsauth: get reading: %w", err)
	}
	if !found {
		return structure.Reading{}, false, ErrInstanceNotFound
	}
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionRead, instance.ScopeType, instance.ScopeID); err != nil {
		return structure.Reading{}, false, err
	}
	return vitalsReader.GetReading(ctx, instanceID)
}

// ListHistory is GetReading's history-list counterpart — same
// permission check, same scope, same up-front denial.
func ListHistory(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, principalID uuid.UUID, instanceID uuid.UUID, limit int) ([]structure.HistoryEntry, error) {
	instance, found, err := vitalsReader.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("vitalsauth: list history: %w", err)
	}
	if !found {
		return nil, ErrInstanceNotFound
	}
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionRead, instance.ScopeType, instance.ScopeID); err != nil {
		return nil, err
	}
	return vitalsReader.ListHistory(ctx, instanceID, limit)
}
