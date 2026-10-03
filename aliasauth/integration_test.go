// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. Both Alias's and Gatehouse-core's
// schemas are provisioned against the same database — their tables live
// in disjoint namespaces (alias_* vs. gatehouse_*), so one real Postgres
// instance serves both stores, proving the integration against real
// data in both, not fakes.
package aliasauth

import (
	"context"
	"os"
	"testing"
	"time"

	aliasDB "github.com/croge130/archipelago/alias/storage/dbstore"
	aliasstructure "github.com/croge130/archipelago/alias/structure"
	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// aliasAliasFor builds an active Alias row for seeding tests that need
// one to already exist, bypassing aliasauth's own Create path entirely.
func aliasAliasFor(table, name, target string) aliasstructure.Alias {
	now := time.Now().Truncate(time.Microsecond)
	return aliasstructure.Alias{
		Table:     table,
		Name:      name,
		Target:    target,
		Lifecycle: aliasstructure.LifecycleActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func setupTest(t *testing.T) (*gatehouseDB.PostgresReader, *gatehouseDB.PostgresWriter, *aliasDB.PostgresReader, *aliasDB.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping aliasauth integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	gatehouseMigrations, err := gatehouseDB.Migrations()
	if err != nil {
		t.Fatalf("gatehouse Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), gatehouseMigrations); err != nil {
		t.Fatalf("gatehouse ProvisionSchemas: %v", err)
	}
	aliasMigrations, err := aliasDB.Migrations()
	if err != nil {
		t.Fatalf("alias Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), aliasMigrations); err != nil {
		t.Fatalf("alias ProvisionSchemas: %v", err)
	}

	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate gatehouse: %v", err)
	}
	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE alias_aliases`)
	if err != nil {
		t.Fatalf("truncate alias: %v", err)
	}

	return gatehouseDB.NewPostgresReader(pool.Pgx()), gatehouseDB.NewPostgresWriter(pool.Pgx()),
		aliasDB.NewPostgresReader(pool.Pgx()), aliasDB.NewPostgresWriter(pool.Pgx())
}

// setupPrincipal registers this package's permission definitions and
// returns a fresh principal with none of them granted yet.
func setupPrincipal(t *testing.T, ctx context.Context, ghReader *gatehouseDB.PostgresReader, ghWriter *gatehouseDB.PostgresWriter, key string) uuid.UUID {
	t.Helper()
	if err := RegisterPermissions(ctx, ghReader, ghWriter); err != nil {
		t.Fatalf("RegisterPermissions: %v", err)
	}
	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, key, gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	return p.PrincipalID
}

// grantGlobal grants permissionKey unconditionally.
func grantGlobal(t *testing.T, ctx context.Context, ghWriter *gatehouseDB.PostgresWriter, principalID uuid.UUID, permissionKey string) {
	t.Helper()
	if _, err := gatehouseFacade.GrantPermission(ctx, ghWriter, gatehouseStructure.GrantSubjectTypePrincipal, principalID, permissionKey); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
}

// grantForTable grants permissionKey scoped to one alias table only.
func grantForTable(t *testing.T, ctx context.Context, ghWriter *gatehouseDB.PostgresWriter, principalID uuid.UUID, permissionKey, table string) {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	key := permissionKey
	contextType := ContextTypeTable
	contextID := table
	g := gatehouseStructure.Grant{
		GrantID:       uuid.New(),
		SubjectType:   gatehouseStructure.GrantSubjectTypePrincipal,
		SubjectID:     principalID,
		TargetType:    gatehouseStructure.GrantTargetTypePermission,
		PermissionKey: &key,
		Scope:         gatehouseStructure.GrantScopeContext,
		ContextType:   &contextType,
		ContextID:     &contextID,
		Effect:        gatehouseStructure.GrantEffectAllow,
		Status:        gatehouseStructure.GrantStatusActive,
		Origin:        gatehouseStructure.GrantOriginManual,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ghWriter.CreateGrant(ctx, g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
}

func TestCreateAliasDeniedWithoutGrant(t *testing.T) {
	ghReader, ghWriter, aliasReader, aliasWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.nobody")

	if _, err := CreateAlias(ctx, ghReader, aliasReader, aliasWriter, principalID, "docs.address_aliases", "alpha", "target-1"); err == nil {
		t.Fatal("expected denial, got nil error")
	}

	if _, found, err := aliasReader.GetAlias(ctx, "docs.address_aliases", "alpha"); err != nil {
		t.Fatalf("GetAlias: %v", err)
	} else if found {
		t.Fatal("alias should not have been created when the permission check was denied")
	}
}

func TestCreateAliasAllowedWithGlobalGrant(t *testing.T) {
	ghReader, ghWriter, aliasReader, aliasWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.admin")
	grantGlobal(t, ctx, ghWriter, principalID, PermissionCreate)

	a, err := CreateAlias(ctx, ghReader, aliasReader, aliasWriter, principalID, "docs.address_aliases", "alpha", "target-1")
	if err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	if a.Target != "target-1" {
		t.Fatalf("Target = %q, want target-1", a.Target)
	}
}

func TestCreateAliasTableScopedGrantDoesNotCoverOtherTables(t *testing.T) {
	ghReader, ghWriter, aliasReader, aliasWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.docs-owner")
	grantForTable(t, ctx, ghWriter, principalID, PermissionCreate, "docs.address_aliases")

	if _, err := CreateAlias(ctx, ghReader, aliasReader, aliasWriter, principalID, "docs.address_aliases", "alpha", "target-1"); err != nil {
		t.Fatalf("CreateAlias on granted table: %v", err)
	}

	if _, err := CreateAlias(ctx, ghReader, aliasReader, aliasWriter, principalID, "other.table", "beta", "target-2"); err == nil {
		t.Fatal("expected denial for a table the grant doesn't cover, got nil error")
	}
	if _, found, err := aliasReader.GetAlias(ctx, "other.table", "beta"); err != nil {
		t.Fatalf("GetAlias: %v", err)
	} else if found {
		t.Fatal("alias should not have been created on an un-granted table")
	}
}

func TestResolveAliasRequiresPermission(t *testing.T) {
	ghReader, ghWriter, aliasReader, aliasWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	// Seed the alias directly through the base, bypassing authorization
	// entirely — aliasauth only wraps Create/Release, it doesn't become
	// the only way to write one.
	if err := aliasWriter.UpsertAlias(ctx, aliasAliasFor("docs.address_aliases", "alpha", "target-1")); err != nil {
		t.Fatalf("seed UpsertAlias: %v", err)
	}

	denied := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")
	if _, _, err := ResolveAlias(ctx, ghReader, aliasReader, denied, "docs.address_aliases", "alpha"); err == nil {
		t.Fatal("expected denial, got nil error")
	}

	allowed := setupPrincipal(t, ctx, ghReader, ghWriter, "service.allowed")
	grantGlobal(t, ctx, ghWriter, allowed, PermissionResolve)
	target, found, err := ResolveAlias(ctx, ghReader, aliasReader, allowed, "docs.address_aliases", "alpha")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if !found || target != "target-1" {
		t.Fatalf("ResolveAlias = (%q, %v), want (target-1, true)", target, found)
	}
}

func TestReleaseAliasRequiresPermission(t *testing.T) {
	ghReader, ghWriter, aliasReader, aliasWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := aliasWriter.UpsertAlias(ctx, aliasAliasFor("docs.address_aliases", "alpha", "target-1")); err != nil {
		t.Fatalf("seed UpsertAlias: %v", err)
	}

	denied := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")
	if err := ReleaseAlias(ctx, ghReader, aliasReader, aliasWriter, denied, "docs.address_aliases", "alpha"); err == nil {
		t.Fatal("expected denial, got nil error")
	}
	if a, _, err := aliasReader.GetAlias(ctx, "docs.address_aliases", "alpha"); err != nil {
		t.Fatalf("GetAlias: %v", err)
	} else if a.Lifecycle != "active" {
		t.Fatal("alias should still be active after a denied release")
	}

	allowed := setupPrincipal(t, ctx, ghReader, ghWriter, "service.allowed")
	grantForTable(t, ctx, ghWriter, allowed, PermissionRelease, "docs.address_aliases")
	if err := ReleaseAlias(ctx, ghReader, aliasReader, aliasWriter, allowed, "docs.address_aliases", "alpha"); err != nil {
		t.Fatalf("ReleaseAlias: %v", err)
	}
	if a, _, err := aliasReader.GetAlias(ctx, "docs.address_aliases", "alpha"); err != nil {
		t.Fatalf("GetAlias: %v", err)
	} else if a.Lifecycle != "released" {
		t.Fatalf("Lifecycle = %q, want released", a.Lifecycle)
	}
}
