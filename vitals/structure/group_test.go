package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validGroup() Group {
	now := time.Now()
	return Group{
		GroupID:   uuid.New(),
		ScopeType: "gatehouse.context",
		ScopeID:   "myapp",
		GroupKey:  "default",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestGroupValidateOK(t *testing.T) {
	if err := validGroup().Validate(); err != nil {
		t.Fatalf("expected a valid group to validate, got: %v", err)
	}
}

func TestGroupValidateRejectsBadGroupKey(t *testing.T) {
	g := validGroup()
	g.GroupKey = "Not Valid!"
	if err := g.Validate(); err == nil {
		t.Fatal("expected an error for an invalid GroupKey")
	}
}

func validInstanceMember(groupID uuid.UUID) GroupMember {
	instanceID := uuid.New()
	return GroupMember{
		GroupID:         groupID,
		MemberKey:       "importer",
		MemberKind:      MemberKindVitalInstance,
		VitalInstanceID: &instanceID,
	}
}

func TestGroupMemberValidateOK(t *testing.T) {
	m := validInstanceMember(uuid.New())
	if err := m.Validate(); err != nil {
		t.Fatalf("expected a valid group member to validate, got: %v", err)
	}
}

func TestGroupMemberValidateRejectsNeitherReference(t *testing.T) {
	m := validInstanceMember(uuid.New())
	m.VitalInstanceID = nil
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error when neither VitalInstanceID nor ChildGroupID is set")
	}
}

func TestGroupMemberValidateRejectsBothReferences(t *testing.T) {
	m := validInstanceMember(uuid.New())
	childGroupID := uuid.New()
	m.ChildGroupID = &childGroupID
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error when both VitalInstanceID and ChildGroupID are set")
	}
}

func TestGroupMemberValidateRejectsKindMismatch(t *testing.T) {
	m := validInstanceMember(uuid.New())
	m.MemberKind = MemberKindVitalGroup
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error when MemberKind doesn't match the set reference")
	}
}

func TestGroupMemberValidateRejectsSelfInclusion(t *testing.T) {
	groupID := uuid.New()
	m := GroupMember{
		GroupID:      groupID,
		MemberKey:    "child",
		MemberKind:   MemberKindVitalGroup,
		ChildGroupID: &groupID,
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error for a group including itself")
	}
}
