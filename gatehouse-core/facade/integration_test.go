// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL, same convention as db's and
// dbstore's own integration tests. They exercise the facade through
// dbstore's real PostgresReader/PostgresWriter — proving facade.Reader
// and facade.Writer are actually satisfied by the concrete
// implementation, not just shaped to look like they would be.
package facade

import (
	"context"
	"os"
	"testing"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
)

func setupFacadeTest(t *testing.T) (Reader, Writer) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping facade integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := dbstore.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
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
		t.Fatalf("truncate: %v", err)
	}

	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx())
}

func TestEnsurePrincipalCreatesOnce(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	first, err := EnsurePrincipal(ctx, reader, writer, "user.christian", structure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal (first): %v", err)
	}

	second, err := EnsurePrincipal(ctx, reader, writer, "user.christian", structure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal (second): %v", err)
	}

	if first.PrincipalID != second.PrincipalID {
		t.Fatalf("expected the second EnsurePrincipal call to return the same principal, got %s and %s", first.PrincipalID, second.PrincipalID)
	}
}

func TestRegisterPermissionBlocksReservedNamespaceByDefault(t *testing.T) {
	_, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterPermission(ctx, writer, structure.PermissionDefinition{
		PermissionKey:          "gatehouse.principal.delete",
		RequiredAuthorityLevel: structure.AuthorityLevelElevated,
	}, RegisterPermissionOptions{})
	if err == nil {
		t.Fatal("expected RegisterPermission to block a reserved namespace by default")
	}
}

func TestRegisterPermissionAllowsReservedNamespaceWithOverride(t *testing.T) {
	_, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterPermission(ctx, writer, structure.PermissionDefinition{
		PermissionKey:          "gatehouse.principal.delete",
		RequiredAuthorityLevel: structure.AuthorityLevelElevated,
	}, RegisterPermissionOptions{AllowReservedNamespace: true})
	if err != nil {
		t.Fatalf("expected RegisterPermission to allow a reserved namespace with the override set, got: %v", err)
	}
}

func TestRegisterPermissionAllowsOrdinaryAppNamespace(t *testing.T) {
	_, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterPermission(ctx, writer, structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.read",
		RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, RegisterPermissionOptions{})
	if err != nil {
		t.Fatalf("expected an ordinary app-namespaced permission to register without the override, got: %v", err)
	}
}

func TestGrantPermissionAndEvaluateEndToEnd(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	principal, err := EnsurePrincipal(ctx, reader, writer, "user.christian", structure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if err := RegisterPermission(ctx, writer, structure.PermissionDefinition{
		PermissionKey: "myapp.readinglist.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	if _, err := GrantPermission(ctx, writer, structure.GrantSubjectTypePrincipal, principal.PrincipalID, "myapp.readinglist.read"); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}

	if err := evaluation.RequirePermission(ctx, reader, principal.PrincipalID, "myapp.readinglist.read"); err != nil {
		t.Fatalf("expected the end-to-end facade flow (EnsurePrincipal -> RegisterPermission -> GrantPermission -> RequirePermission) to allow, got: %v", err)
	}
}
