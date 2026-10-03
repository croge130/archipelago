package aliasauth

import (
	"context"
	"fmt"

	aliasevaluation "github.com/croge130/archipelago/alias/evaluation"
	aliasfacade "github.com/croge130/archipelago/alias/facade"
	aliasstructure "github.com/croge130/archipelago/alias/structure"
	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// Permission keys this package owns. See doc.go for why these three and
// why table-scoped rather than entry-scoped.
const (
	PermissionCreate  = "alias.create"
	PermissionResolve = "alias.resolve"
	PermissionRelease = "alias.release"
)

// ContextTypeTable is the Gatehouse-core ContextType this package scopes
// grants to — a Context's ID under this type is an alias table name.
const ContextTypeTable = "alias.table"

// RegisterPermissions registers this package's three built-in
// permission keys as standard-authority, wildcard-includable
// definitions — wildcard-includable so a single "alias.*" grant can
// cover create, resolve, and release at once, the same sane default
// every other wildcard-includable key in this design uses.
// facade.RegisterPermission is idempotent by key, so this is safe to
// call on every app startup, not just once ever.
func RegisterPermissions(ctx context.Context, reader gatehouseFacade.Reader, writer gatehouseFacade.Writer) error {
	defs := []gatehouseStructure.PermissionDefinition{
		{PermissionKey: PermissionCreate, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
		{PermissionKey: PermissionResolve, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
		{PermissionKey: PermissionRelease, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard, WildcardIncludable: true},
	}
	opts := gatehouseFacade.RegisterPermissionOptions{AllowReservedNamespace: true}
	for _, def := range defs {
		if err := gatehouseFacade.RegisterPermission(ctx, reader, writer, def, opts); err != nil {
			return fmt.Errorf("aliasauth: register permissions: %w", err)
		}
	}
	return nil
}

// CreateAlias checks principalID holds PermissionCreate for table (global
// or table-scoped), then calls through to alias/facade.EnsureAlias
// unchanged — same idempotent-by-(table,name) shape, same ErrConflict.
func CreateAlias(ctx context.Context, gatehouseStore evaluation.Store, aliasReader aliasfacade.Reader, aliasWriter aliasfacade.Writer, principalID uuid.UUID, table, name, target string) (aliasstructure.Alias, error) {
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionCreate, ContextTypeTable, table); err != nil {
		return aliasstructure.Alias{}, err
	}
	return aliasfacade.EnsureAlias(ctx, aliasReader, aliasWriter, table, name, target)
}

// ResolveAlias checks principalID holds PermissionResolve for table
// before calling through to alias/evaluation.Resolve — denied up front,
// so a principal with no access to table learns nothing about whether
// name exists under it.
func ResolveAlias(ctx context.Context, gatehouseStore evaluation.Store, aliasStore aliasevaluation.Store, principalID uuid.UUID, table, name string) (target string, found bool, err error) {
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionResolve, ContextTypeTable, table); err != nil {
		return "", false, err
	}
	return aliasevaluation.Resolve(ctx, aliasStore, table, name)
}

// ReleaseAlias checks principalID holds PermissionRelease for table,
// then calls through to alias/facade.ReleaseAlias unchanged — same
// idempotent shape, same ErrNotFound.
func ReleaseAlias(ctx context.Context, gatehouseStore evaluation.Store, aliasReader aliasfacade.Reader, aliasWriter aliasfacade.Writer, principalID uuid.UUID, table, name string) error {
	if err := evaluation.RequireContextPermission(ctx, gatehouseStore, principalID, PermissionRelease, ContextTypeTable, table); err != nil {
		return err
	}
	return aliasfacade.ReleaseAlias(ctx, aliasReader, aliasWriter, table, name)
}
