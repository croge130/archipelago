package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

// Definition is a reusable meaning/type contract, immutable once
// published by (DefinitionKey, DefinitionVersion, SchemaHash): same
// key+version+hash is an idempotent no-op, same key+version+different
// hash is rejected, same key+new version is a new definition — the
// facade's EnsureDefinition enforces this, not Validate (the same
// "structure checks shape, not cross-row invariants" split used
// throughout this design).
//
// No OwnerKind/OwnerAppID: ownership is the reserved-namespace
// convention gatehouse-core/facade already has for permission keys,
// reused here for definition keys — "vitals" is reserved for this
// package's own built-ins (see builtins.go); any other namespace is a
// caller's own, the same way an app registers "myapp.readinglist.read"
// without a separate ownership field. See 14-vitals-model.md.
type Definition struct {
	DefinitionID          uuid.UUID
	DefinitionKey         string
	DefinitionVersion     int
	SchemaHash            string
	Title                 string
	Description           string
	ValueMetadata         *typedvalue.Definition // nil if this vital carries no typed value
	AppValueMetadata      json.RawMessage        // opaque, never authorized on
	AllowedStates         []State                // nil = no restriction beyond the closed State enum
	DefaultExpectedStates []State
	DefaultImportance     Importance
	DefaultTTL            *time.Duration
	DefaultDisplayHints   json.RawMessage
	AppMetadata           json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ArchivedAt            *time.Time
}

func (d Definition) Validate() error {
	if d.DefinitionID == uuid.Nil {
		return fmt.Errorf("structure: definition: DefinitionID is required")
	}
	if err := ValidateDefinitionKey(d.DefinitionKey); err != nil {
		return fmt.Errorf("structure: definition: %w", err)
	}
	if d.DefinitionVersion <= 0 {
		return fmt.Errorf("structure: definition: DefinitionVersion must be positive")
	}
	if d.ValueMetadata != nil {
		if err := d.ValueMetadata.Validate(); err != nil {
			return fmt.Errorf("structure: definition: value metadata: %w", err)
		}
	}
	if err := validateStates("allowed_states", d.AllowedStates); err != nil {
		return fmt.Errorf("structure: definition: %w", err)
	}
	if err := validateStates("default_expected_states", d.DefaultExpectedStates); err != nil {
		return fmt.Errorf("structure: definition: %w", err)
	}
	if d.DefaultImportance != "" && !d.DefaultImportance.Valid() {
		return fmt.Errorf("structure: definition: invalid DefaultImportance %q", d.DefaultImportance)
	}
	return nil
}
