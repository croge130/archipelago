package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// ErrConflict is returned by EnsureDefinition when (DefinitionKey,
// DefinitionVersion) already exists with a different SchemaHash —
// "ensure" means idempotent create-if-absent, never a silent
// redefinition of an already-published contract. Reused for the same
// shape of conflict on EnsureInstance/EnsureGroup below.
var ErrConflict = errors.New("facade: vitals: already exists with a different shape")

// EnsureDefinition registers d, idempotent by (DefinitionKey,
// DefinitionVersion, SchemaHash): an identical re-registration is a
// no-op, a different SchemaHash under the same key+version is
// ErrConflict, and a new version is always a new definition. Any
// caller may register a definition under any key outside the "vitals"
// reserved namespace — this function has no concept of ownership at
// all, per 14-vitals-model.md.
func EnsureDefinition(ctx context.Context, reader Reader, writer Writer, d structure.Definition) (structure.Definition, error) {
	existing, found, err := reader.GetDefinitionByKeyVersion(ctx, d.DefinitionKey, d.DefinitionVersion)
	if err != nil {
		return structure.Definition{}, fmt.Errorf("facade: ensure definition: %w", err)
	}
	if found {
		if existing.SchemaHash != d.SchemaHash {
			return structure.Definition{}, ErrConflict
		}
		return existing, nil
	}

	now := time.Now().Truncate(time.Microsecond)
	d.DefinitionID = uuid.New()
	d.CreatedAt = now
	d.UpdatedAt = now
	if err := d.Validate(); err != nil {
		return structure.Definition{}, fmt.Errorf("facade: ensure definition: %w", err)
	}
	if err := writer.CreateDefinition(ctx, d); err != nil {
		return structure.Definition{}, fmt.Errorf("facade: ensure definition: %w", err)
	}
	return d, nil
}

// SeedBuiltinDefinitions registers every key from
// structure.BuiltinDefinitionKeys() at version 1 with no value
// metadata — the minimal shape needed for the keys to exist and
// resolve; a caller wanting a built-in's typed value shape registers
// its own richer definition under its own key instead. Idempotent via
// EnsureDefinition, safe to call on every app startup.
func SeedBuiltinDefinitions(ctx context.Context, reader Reader, writer Writer) error {
	for _, key := range structure.BuiltinDefinitionKeys() {
		if _, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
			DefinitionKey:     key,
			DefinitionVersion: 1,
		}); err != nil {
			return fmt.Errorf("facade: seed builtin definitions: %q: %w", key, err)
		}
	}
	return nil
}
