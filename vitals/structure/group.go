package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Group is a curated collection of Instances (and, by composition,
// other Groups) for display and monitoring. Uniqueness is
// (ScopeType, ScopeID, GroupKey) — enforced by storage.
type Group struct {
	GroupID      uuid.UUID
	ScopeType    string
	ScopeID      string
	GroupKey     string
	Title        string
	Description  string
	SortOrder    int
	DisplayHints json.RawMessage
	AppMetadata  json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ArchivedAt   *time.Time
}

func (g Group) Validate() error {
	if g.GroupID == uuid.Nil {
		return fmt.Errorf("structure: group: GroupID is required")
	}
	if g.ScopeType == "" {
		return fmt.Errorf("structure: group: ScopeType is required")
	}
	if g.ScopeID == "" {
		return fmt.Errorf("structure: group: ScopeID is required")
	}
	if err := ValidateGroupKey(g.GroupKey); err != nil {
		return fmt.Errorf("structure: group: %w", err)
	}
	return nil
}

// GroupMember is one entry in a Group — either a VitalInstanceID or a
// ChildGroupID, matching MemberKind, never both. Composition (a group
// may include child groups) is supported; inheritance and dynamic
// selectors are not — per 14-vitals-model.md / the original Vitals
// supplement's §8.2. Cycle rejection is storage's job (a recursive
// query), since it requires seeing the whole membership graph.
type GroupMember struct {
	GroupID         uuid.UUID
	MemberKey       string
	MemberKind      MemberKind
	VitalInstanceID *uuid.UUID
	ChildGroupID    *uuid.UUID
	Label           string
	SortOrder       int
	Required        bool
	DisplayHints    json.RawMessage
	AppMetadata     json.RawMessage
}

func (m GroupMember) Validate() error {
	if m.GroupID == uuid.Nil {
		return fmt.Errorf("structure: group member: GroupID is required")
	}
	if err := ValidateMemberKey(m.MemberKey); err != nil {
		return fmt.Errorf("structure: group member: %w", err)
	}
	if !m.MemberKind.Valid() {
		return fmt.Errorf("structure: group member: invalid MemberKind %q", m.MemberKind)
	}
	haveInstance := m.VitalInstanceID != nil && *m.VitalInstanceID != uuid.Nil
	haveChildGroup := m.ChildGroupID != nil && *m.ChildGroupID != uuid.Nil
	if haveInstance == haveChildGroup {
		return fmt.Errorf("structure: group member: exactly one of VitalInstanceID or ChildGroupID must be set")
	}
	if m.MemberKind == MemberKindVitalInstance && !haveInstance {
		return fmt.Errorf("structure: group member: MemberKind vital_instance requires VitalInstanceID")
	}
	if m.MemberKind == MemberKindVitalGroup && !haveChildGroup {
		return fmt.Errorf("structure: group member: MemberKind vital_group requires ChildGroupID")
	}
	if haveChildGroup && *m.ChildGroupID == m.GroupID {
		return fmt.Errorf("structure: group member: a group cannot include itself")
	}
	return nil
}
