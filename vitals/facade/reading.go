package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/vitals/structure"
)

// ErrInstanceNotFound is returned by WriteReading when reading's
// InstanceID names no registered instance.
var ErrInstanceNotFound = errors.New("facade: vitals: instance not found")

// WriteReading looks up reading.InstanceID's own Instance and
// Definition to derive the expected-states set
// (structure.ExpectedStatesOrDefault), then delegates the actual
// decide-and-write sequence — quality warnings, the next Revision,
// the current-row upsert, and the conditional history insert — to
// Writer.WriteReadingAtomic, which runs all of it inside one
// transaction. See that method's own doc comment for why: a separate
// read-then-decide-then-write here, the shape every other Ensure*-
// style facade in this codebase accepts for a first-creation-only
// race, would instead race on *every* write to the same instance,
// silently overwriting readings and duplicating history rows with no
// error signal — multi-instance reporting into shared Vitals state is
// exactly the pattern this base exists to support, so that race isn't
// a theoretical edge case here the way it is elsewhere.
//
// UpdatedAt is always set to now here, overriding whatever the caller
// passed; ObservedAt stays the caller's own "when I actually observed
// this" field, which can legitimately differ.
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
	reading.UpdatedAt = time.Now().Truncate(time.Microsecond)

	written, err := writer.WriteReadingAtomic(ctx, reading, expectedStates)
	if err != nil {
		return structure.Reading{}, fmt.Errorf("facade: write reading: %w", err)
	}
	return written, nil
}
