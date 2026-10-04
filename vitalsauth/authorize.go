package vitalsauth

import (
	"context"
	"fmt"

	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
)

// Permission keys this package owns, under the "vitals" reserved
// namespace. See doc.go for why there's no vitalsauth-owned context
// type: Instance/Group's own (ScopeType, ScopeID) pair is the context
// every scoped check below uses directly.
const (
	PermissionRead             = "vitals.read"
	PermissionWrite            = "vitals.write"
	PermissionDefinitionManage = "vitals.definition.manage"
	PermissionGroupRead        = "vitals.group.read"
	PermissionGroupManage      = "vitals.group.manage"
)

// RegisterPermissions registers this package's five built-in
// permission keys as standard-authority, wildcard-includable
// definitions — wildcard-includable so a single "vitals.*" grant can
// cover all five at once, the same sane default every other
// wildcard-includable key in this design uses.
// gatehouseFacade.RegisterPermission is idempotent by key, so this is
// safe to call on every app startup, not just once ever.
func RegisterPermissions(ctx context.Context, reader gatehouseFacade.Reader, writer gatehouseFacade.Writer) error {
	defs := []gatehouseStructure.PermissionDefinition{
		{PermissionKey: PermissionRead, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
		{PermissionKey: PermissionWrite, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
		{PermissionKey: PermissionDefinitionManage, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
		{PermissionKey: PermissionGroupRead, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
		{PermissionKey: PermissionGroupManage, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
	}
	opts := gatehouseFacade.RegisterPermissionOptions{AllowReservedNamespace: true}
	for _, def := range defs {
		if err := gatehouseFacade.RegisterPermission(ctx, reader, writer, def, opts); err != nil {
			return fmt.Errorf("vitalsauth: register permissions: %w", err)
		}
	}
	return nil
}
