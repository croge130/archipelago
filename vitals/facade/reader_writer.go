package facade

import (
	"context"

	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// Reader is everything the facade needs to read.
type Reader interface {
	GetDefinition(ctx context.Context, id uuid.UUID) (structure.Definition, bool, error)
	GetDefinitionByKeyVersion(ctx context.Context, key string, version int) (structure.Definition, bool, error)
	GetInstance(ctx context.Context, id uuid.UUID) (structure.Instance, bool, error)
	GetInstanceByScopeKey(ctx context.Context, scopeType, scopeID, instanceKey string) (structure.Instance, bool, error)
	GetReading(ctx context.Context, instanceID uuid.UUID) (structure.Reading, bool, error)
	ListHistory(ctx context.Context, instanceID uuid.UUID, limit int) ([]structure.HistoryEntry, error)
	GetGroup(ctx context.Context, id uuid.UUID) (structure.Group, bool, error)
	GetGroupByScopeKey(ctx context.Context, scopeType, scopeID, groupKey string) (structure.Group, bool, error)
	ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]structure.GroupMember, error)
}

// Writer is every mutation the facade drives. Declared here, its
// consumer, not in storage/dbstore — same rule every other module's
// own facade.Writer follows.
type Writer interface {
	CreateDefinition(ctx context.Context, d structure.Definition) error
	CreateInstance(ctx context.Context, i structure.Instance) error
	UpsertReading(ctx context.Context, r structure.Reading) error
	InsertHistory(ctx context.Context, h structure.HistoryEntry) error
	CreateGroup(ctx context.Context, g structure.Group) error
	UpsertGroupMember(ctx context.Context, m structure.GroupMember) error
	DeleteGroupMember(ctx context.Context, groupID uuid.UUID, memberKey string) error
}
