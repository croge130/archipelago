package structure

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PolicyContext is a named, independently-editable set of Refs — the
// "apply one override to a group of services" case, generalized.
// Targeting the set once and changing its membership later without
// touching the override itself is the same "pointer, not an embedded
// value" reasoning the status-aggregation policy pointer already uses
// (03-multi-instance-and-suites.md).
type PolicyContext struct {
	PolicyContextID uuid.UUID
	Key             string
	Description     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c PolicyContext) Validate() error {
	if c.PolicyContextID == uuid.Nil {
		return fmt.Errorf("structure: policy context: PolicyContextID is required")
	}
	if c.Key == "" {
		return fmt.Errorf("structure: policy context: Key is required")
	}
	return nil
}

// PolicyContextMember is one Ref belonging to a PolicyContext.
// Membership is Refs only — never another PolicyContext. A
// PolicyContext that could contain other PolicyContexts would
// recreate a containment tree under a new name, the exact mistake
// this model exists to not repeat; stated here as firmly as "no
// realm anywhere in this model" was for Gatehouse-core.
type PolicyContextMember struct {
	PolicyContextID uuid.UUID
	Ref             Ref
	CreatedAt       time.Time
}

func (m PolicyContextMember) Validate() error {
	if m.PolicyContextID == uuid.Nil {
		return fmt.Errorf("structure: policy context member: PolicyContextID is required")
	}
	if err := m.Ref.Validate(); err != nil {
		return fmt.Errorf("structure: policy context member: %w", err)
	}
	return nil
}
