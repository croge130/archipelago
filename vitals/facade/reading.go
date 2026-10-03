package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/vitals/evaluation"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// ErrInstanceNotFound is returned by WriteReading when reading's
// InstanceID names no registered instance.
var ErrInstanceNotFound = errors.New("facade: vitals: instance not found")

// WriteReading is the one real read-then-write operation this base
// has: it looks up reading.InstanceID's own Instance and Definition to
// derive the expected-states set (structure.ExpectedStatesOrDefault),
// runs evaluation.QualityWarnings against it, computes the next
// Revision from whatever's currently stored, upserts the current
// reading, and — if evaluation.IsNotableTransition says so — inserts a
// history row. UpdatedAt is always set to now here, overriding
// whatever the caller passed; ObservedAt stays the caller's own "when
// I actually observed this" field, which can legitimately differ.
//
// Known limitation, same shape as EnsureAlias/EnsurePrincipal's own:
// this reads the previous reading and the next Revision, then writes,
// without wrapping both in one transaction — two concurrent writes to
// the same instance can race on Revision. Accepted for the same reason
// those facades accept it: the common case (one reporter per
// instance) doesn't hit it, and Vitals readings are liveness-shaped
// data, not a security boundary that needs strict serialization.
func WriteReading(ctx context.Context, reader Reader, writer Writer, reading structure.Reading) (structure.Reading, error) {
	instance, found, err := reader.GetInstance(ctx, reading.InstanceID)
	if err != nil {
		return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
	}
	if !found {
		return structure.Reading{}, ErrInstanceNotFound
	}
	def, found, err := reader.GetDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
	}
	if !found {
		return structure.Reading{}, fmt.Errorf("facade: write reading: instance %s references a missing definition %s", instance.InstanceID, instance.DefinitionID)
	}

	expectedStates := structure.ExpectedStatesOrDefault(instance, def)
	reading.QualityWarnings = evaluation.QualityWarnings(reading, expectedStates)

	previous, hadPrevious, err := reader.GetReading(ctx, reading.InstanceID)
	if err != nil {
		return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
	}
	reading.Revision = 1
	if hadPrevious {
		reading.Revision = previous.Revision + 1
	}
	reading.UpdatedAt = time.Now().Truncate(time.Microsecond)

	if err := reading.Validate(); err != nil {
		return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
	}
	if err := writer.UpsertReading(ctx, reading); err != nil {
		return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
	}

	if evaluation.IsNotableTransition(previous, reading, hadPrevious) {
		if err := writer.InsertHistory(ctx, structure.HistoryEntry{HistoryID: uuid.New(), Reading: reading}); err != nil {
			return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
		}
	}
	return reading, nil
}
