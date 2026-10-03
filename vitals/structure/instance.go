package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Instance is one concrete observed thing in a scope. ScopeType/ScopeID
// are deliberately plain strings with no enum and no validation
// against a closed set — per 14-vitals-model.md's "no Scope type"
// section, this pair is reused directly as a Gatehouse-core Context by
// vitalsauth and as a Policy Ref by vitalsdefaults; Vitals itself never
// interprets it, the same restraint Alias's own Table field already
// uses.
//
// Uniqueness is (ScopeType, ScopeID, InstanceKey) — enforced by
// storage, not here.
type Instance struct {
	InstanceID      uuid.UUID
	ScopeType       string
	ScopeID         string
	InstanceKey     string
	DefinitionID    uuid.UUID
	SubjectType     string // free-form: "service", "queue", "job_run", ...
	SubjectKey      string
	DisplayName     string
	Description     string
	CategoryPath    []string // organizational only, not identity
	ExpectedStates  []State
	Importance      Importance
	ReferenceRanges json.RawMessage
	DisplayHints    json.RawMessage
	ResourceURI     string
	ExternalURI     string
	DetailRoute     string
	AppMetadata     json.RawMessage
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ArchivedAt      *time.Time
}

func (i Instance) Validate() error {
	if i.InstanceID == uuid.Nil {
		return fmt.Errorf("structure: instance: InstanceID is required")
	}
	if i.ScopeType == "" {
		return fmt.Errorf("structure: instance: ScopeType is required")
	}
	if i.ScopeID == "" {
		return fmt.Errorf("structure: instance: ScopeID is required")
	}
	if err := ValidateInstanceKey(i.InstanceKey); err != nil {
		return fmt.Errorf("structure: instance: %w", err)
	}
	if i.DefinitionID == uuid.Nil {
		return fmt.Errorf("structure: instance: DefinitionID is required")
	}
	if err := validateStates("expected_states", i.ExpectedStates); err != nil {
		return fmt.Errorf("structure: instance: %w", err)
	}
	if i.Importance != "" && !i.Importance.Valid() {
		return fmt.Errorf("structure: instance: invalid Importance %q", i.Importance)
	}
	if err := ValidateCategoryPath(i.CategoryPath); err != nil {
		return fmt.Errorf("structure: instance: %w", err)
	}
	return nil
}

// ExpectedStatesOrDefault resolves "expected = reading.state in
// instance.expected_states," falling back to def's default and then
// to [ok] if both are absent — per 14-vitals-model.md / the original
// Vitals supplement's §6.2.
func ExpectedStatesOrDefault(instance Instance, def Definition) []State {
	if len(instance.ExpectedStates) > 0 {
		return instance.ExpectedStates
	}
	if len(def.DefaultExpectedStates) > 0 {
		return def.DefaultExpectedStates
	}
	return []State{StateOK}
}
