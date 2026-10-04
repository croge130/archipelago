package vitalsauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// ErrGroupNotFound is returned by SetGroupMember/RemoveGroupMember/
// ListGroupMembers when groupID names no registered group — needed
// here independently of vitals/facade, since this package has to look
// the group up itself to learn its scope before it can check
// permission against it.
var ErrGroupNotFound = errors.New("vitalsauth: group not found")

// EnsureGroup checks principalID holds PermissionGroupManage scoped to
// g's own (ScopeType, ScopeID), then calls through to
// vitals/facade.EnsureGroup unchanged.
func EnsureGroup(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, vitalsWriter vitalsFacade.Writer, principalID uuid.UUID, g structure.Group) (structure.Group, error) {
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionGroupManage, g.ScopeType, g.ScopeID); err != nil {
		return structure.Group{}, err
	}
	return vitalsFacade.EnsureGroup(ctx, vitalsReader, vitalsWriter, g)
}

// SetGroupMember checks principalID holds PermissionGroupManage scoped
// to groupID's own (ScopeType, ScopeID) — looked up first, since a
// member-set call only carries the GroupID, not the group's scope —
// then calls through to vitals/facade.SetGroupMember unchanged.
func SetGroupMember(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, vitalsWriter vitalsFacade.Writer, principalID uuid.UUID, m structure.GroupMember) error {
	group, found, err := vitalsReader.GetGroup(ctx, m.GroupID)
	if err != nil {
		return fmt.Errorf("vitalsauth: set group member: %w", err)
	}
	if !found {
		return ErrGroupNotFound
	}
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionGroupManage, group.ScopeType, group.ScopeID); err != nil {
		return err
	}
	return vitalsFacade.SetGroupMember(ctx, vitalsWriter, m)
}

// RemoveGroupMember is SetGroupMember's removal counterpart — same
// lookup, same permission, same scope.
func RemoveGroupMember(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, vitalsWriter vitalsFacade.Writer, principalID uuid.UUID, groupID uuid.UUID, memberKey string) error {
	group, found, err := vitalsReader.GetGroup(ctx, groupID)
	if err != nil {
		return fmt.Errorf("vitalsauth: remove group member: %w", err)
	}
	if !found {
		return ErrGroupNotFound
	}
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionGroupManage, group.ScopeType, group.ScopeID); err != nil {
		return err
	}
	return vitalsFacade.RemoveGroupMember(ctx, vitalsWriter, groupID, memberKey)
}

// ListGroupMembers checks principalID holds PermissionGroupRead scoped
// to groupID's own (ScopeType, ScopeID) before listing its members.
//
// Known v0 narrowing, named rather than hidden: holding read access to
// the group does not re-check each individual member's own instance-
// or child-group-level read permission — a caller that can read a
// group sees every member it lists, full stop. Per-member filtering
// (so a group read never reveals an instance its own scope would deny)
// is real future work, not built here; nothing in this codebase yet
// needs the distinction between "can see the group's shape" and "can
// see every individual member's own data" to actually differ.
func ListGroupMembers(ctx context.Context, gatehouseStore evaluation.Store, vitalsReader vitalsFacade.Reader, principalID uuid.UUID, groupID uuid.UUID) ([]structure.GroupMember, error) {
	group, found, err := vitalsReader.GetGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("vitalsauth: list group members: %w", err)
	}
	if !found {
		return nil, ErrGroupNotFound
	}
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionGroupRead, group.ScopeType, group.ScopeID); err != nil {
		return nil, err
	}
	return vitalsReader.ListGroupMembers(ctx, groupID)
}
