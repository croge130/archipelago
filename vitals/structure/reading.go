package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/google/uuid"
)

// QualityWarning flags a semantically odd (but not invalid) reading —
// warn, don't reject, per the original Vitals supplement's §6.4.
type QualityWarning struct {
	Code    string
	Message string
}

// Reading is the current observation for one instance — one row per
// instance, upserted on write. See 14-vitals-model.md for why this
// trims Lighthouse's own shape: SourceInstanceID replaces SourceAppID/
// SourceAppInstanceID (a real reference to registry's own Instance,
// not a second free-string identity vocabulary), TraceContext replaces
// CorrelationID/CausationID (Archipelago's own standard OTel
// vocabulary, not Lighthouse's ad hoc terms), and LogEntryID/
// NarrativeID/RequestedByPrincipalID/AuthoritySource are dropped
// entirely (no Logbook equivalent exists; no concrete write path needs
// the actor/requester distinction yet).
type Reading struct {
	InstanceID       uuid.UUID
	State            State
	Impact           Impact
	ImpactScore      *int
	Value            json.RawMessage
	Summary          string
	ReasonCode       string
	DetailsJSON      json.RawMessage
	ObservedAt       *time.Time
	UpdatedAt        time.Time
	ExpiresAt        *time.Time
	SourceInstanceID *uuid.UUID
	ActorPrincipalID *uuid.UUID
	TraceContext     *logging.SpanContext
	QualityWarnings  []QualityWarning
	Revision         int64
}

func (r Reading) Validate() error {
	if r.InstanceID == uuid.Nil {
		return fmt.Errorf("structure: reading: InstanceID is required")
	}
	if !r.State.Valid() {
		return fmt.Errorf("structure: reading: invalid State %q", r.State)
	}
	if r.Impact != "" && !r.Impact.Valid() {
		return fmt.Errorf("structure: reading: invalid Impact %q", r.Impact)
	}
	if r.ImpactScore != nil && (*r.ImpactScore < 0 || *r.ImpactScore > 100) {
		return fmt.Errorf("structure: reading: ImpactScore must be between 0 and 100")
	}
	if err := ValidateSummary(r.Summary); err != nil {
		return fmt.Errorf("structure: reading: %w", err)
	}
	if err := ValidateReasonCode(r.ReasonCode); err != nil {
		return fmt.Errorf("structure: reading: %w", err)
	}
	return nil
}

// HistoryEntry is a captured notable transition — see evaluation's
// IsNotableTransition for what counts as notable.
type HistoryEntry struct {
	Reading
	HistoryID uuid.UUID
}
