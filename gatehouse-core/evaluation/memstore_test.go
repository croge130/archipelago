package evaluation

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// memStore is an in-memory Store for testing Evaluate's logic without
// a DB — exactly the point of defining Store as an interface Evaluation
// owns.
type memStore struct {
	permissions       map[string]structure.PermissionDefinition
	grantsBySubject   map[string][]structure.Grant
	groupsByPrincipal map[uuid.UUID][]uuid.UUID
	rolePermissions   map[uuid.UUID][]structure.RolePermission
}

func newMemStore() *memStore {
	return &memStore{
		permissions:       make(map[string]structure.PermissionDefinition),
		grantsBySubject:   make(map[string][]structure.Grant),
		groupsByPrincipal: make(map[uuid.UUID][]uuid.UUID),
		rolePermissions:   make(map[uuid.UUID][]structure.RolePermission),
	}
}

func subjectKey(t structure.GrantSubjectType, id uuid.UUID) string {
	return fmt.Sprintf("%s:%s", t, id)
}

func (m *memStore) addPermission(def structure.PermissionDefinition) {
	m.permissions[def.PermissionKey] = def
}

func (m *memStore) addGrant(g structure.Grant) {
	key := subjectKey(g.SubjectType, g.SubjectID)
	m.grantsBySubject[key] = append(m.grantsBySubject[key], g)
}

func (m *memStore) addGroupMember(groupID, principalID uuid.UUID) {
	m.groupsByPrincipal[principalID] = append(m.groupsByPrincipal[principalID], groupID)
}

func (m *memStore) addRolePermission(p structure.RolePermission) {
	m.rolePermissions[p.RoleID] = append(m.rolePermissions[p.RoleID], p)
}

func (m *memStore) GetPermissionDefinition(ctx context.Context, key string) (structure.PermissionDefinition, bool, error) {
	def, ok := m.permissions[key]
	return def, ok, nil
}

func (m *memStore) ActiveGrantsForSubject(ctx context.Context, subjectType structure.GrantSubjectType, subjectID uuid.UUID) ([]structure.Grant, error) {
	var active []structure.Grant
	for _, g := range m.grantsBySubject[subjectKey(subjectType, subjectID)] {
		if g.Status == structure.GrantStatusActive {
			active = append(active, g)
		}
	}
	return active, nil
}

func (m *memStore) GroupIDsForPrincipal(ctx context.Context, principalID uuid.UUID) ([]uuid.UUID, error) {
	return m.groupsByPrincipal[principalID], nil
}

func (m *memStore) RolePermissions(ctx context.Context, roleID uuid.UUID) ([]structure.RolePermission, error) {
	return m.rolePermissions[roleID], nil
}
