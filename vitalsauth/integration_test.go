// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. Both Gatehouse-core's and
// Vitals' schemas are provisioned against the same database — their
// tables live in disjoint namespaces (gatehouse_* vs. vitals_*), so
// one real Postgres instance serves both stores, proving the
// integration against real data in both, not fakes.
package vitalsauth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	vitalsDB "github.com/croge130/archipelago/vitals/storage/dbstore"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func setupTest(t *testing.T) (*gatehouseDB.PostgresReader, *gatehouseDB.PostgresWriter, *vitalsDB.PostgresReader, *vitalsDB.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping vitalsauth integration test")
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
	vitalsMigrations, err := vitalsDB.Migrations()
	if err != nil {
		t.Fatalf("vitals Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), vitalsMigrations); err != nil {
		t.Fatalf("vitals ProvisionSchemas: %v", err)
	}

	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_leases, gatehouse_instances,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate gatehouse: %v", err)
	}
	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE
		vitals_group_members, vitals_groups, vitals_reading_history, vitals_current_readings,
		vitals_instances, vitals_definitions
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate vitals: %v", err)
	}

	return gatehouseDB.NewPostgresReader(pool.Pgx()), gatehouseDB.NewPostgresWriter(pool.Pgx()),
		vitalsDB.NewPostgresReader(pool.Pgx()), vitalsDB.NewPostgresWriter(pool.Pgx())
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

// grantForScope grants permissionKey scoped to one (scopeType, scopeID)
// pair — the exact Context a vitalsauth check builds from an
// Instance's or Group's own scope fields.
func grantForScope(t *testing.T, ctx context.Context, ghWriter *gatehouseDB.PostgresWriter, principalID uuid.UUID, permissionKey, scopeType, scopeID string) {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	key := permissionKey
	ct, ci := scopeType, scopeID
	g := gatehouseStructure.Grant{
		GrantID:       uuid.New(),
		SubjectType:   gatehouseStructure.GrantSubjectTypePrincipal,
		SubjectID:     principalID,
		TargetType:    gatehouseStructure.GrantTargetTypePermission,
		PermissionKey: &key,
		Scope:         gatehouseStructure.GrantScopeContext,
		ContextType:   &ct,
		ContextID:     &ci,
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

func grantGlobal(t *testing.T, ctx context.Context, ghWriter *gatehouseDB.PostgresWriter, principalID uuid.UUID, permissionKey string) {
	t.Helper()
	if _, err := gatehouseFacade.GrantPermission(ctx, ghWriter, gatehouseStructure.GrantSubjectTypePrincipal, principalID, permissionKey); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
}

// seedDefinitionAndInstance bypasses vitalsauth entirely to create a
// definition+instance directly through the base, the same "seed data,
// then test authorization on top of it" pattern aliasauth's own tests
// use.
func seedDefinitionAndInstance(t *testing.T, ctx context.Context, vitalsReader *vitalsDB.PostgresReader, vitalsWriter *vitalsDB.PostgresWriter, scopeType, scopeID, instanceKey string) structure.Instance {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	def := structure.Definition{
		// A fresh key per call — tests that seed more than one
		// instance in the same database must not collide on
		// (DefinitionKey, DefinitionVersion)'s own uniqueness.
		DefinitionID: uuid.New(), DefinitionKey: "myapp.importer.status." + uuid.New().String(), DefinitionVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := vitalsWriter.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	inst := structure.Instance{
		InstanceID: uuid.New(), ScopeType: scopeType, ScopeID: scopeID, InstanceKey: instanceKey,
		DefinitionID: def.DefinitionID, CreatedAt: now, UpdatedAt: now,
	}
	if err := vitalsWriter.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	return inst
}

func TestWriteReadingDeniedWithoutGrant(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	inst := seedDefinitionAndInstance(t, ctx, vitalsReader, vitalsWriter, "gatehouse.context", "myapp", "core.services.importer.status")
	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")

	if _, err := WriteReading(ctx, ghReader, vitalsReader, vitalsWriter, principalID, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK}); err == nil {
		t.Fatal("expected denial, got nil error")
	}
	if _, found, err := vitalsReader.GetReading(ctx, inst.InstanceID); err != nil {
		t.Fatalf("GetReading: %v", err)
	} else if found {
		t.Fatal("reading should not have been written when the permission check was denied")
	}
}

func TestWriteReadingAllowedWithScopedGrant(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	inst := seedDefinitionAndInstance(t, ctx, vitalsReader, vitalsWriter, "gatehouse.context", "myapp", "core.services.importer.status")
	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.allowed")
	grantForScope(t, ctx, ghWriter, principalID, PermissionWrite, "gatehouse.context", "myapp")

	got, err := WriteReading(ctx, ghReader, vitalsReader, vitalsWriter, principalID, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK})
	if err != nil {
		t.Fatalf("WriteReading: %v", err)
	}
	if got.State != structure.StateOK {
		t.Fatalf("State = %q, want ok", got.State)
	}
}

func TestWriteReadingScopedGrantDoesNotCoverOtherScopes(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	instA := seedDefinitionAndInstance(t, ctx, vitalsReader, vitalsWriter, "gatehouse.context", "myapp", "a")
	instB := seedDefinitionAndInstance(t, ctx, vitalsReader, vitalsWriter, "gatehouse.context", "other-app", "b")
	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.scoped")
	grantForScope(t, ctx, ghWriter, principalID, PermissionWrite, "gatehouse.context", "myapp")

	if _, err := WriteReading(ctx, ghReader, vitalsReader, vitalsWriter, principalID, structure.Reading{InstanceID: instA.InstanceID, State: structure.StateOK}); err != nil {
		t.Fatalf("WriteReading on granted scope: %v", err)
	}
	if _, err := WriteReading(ctx, ghReader, vitalsReader, vitalsWriter, principalID, structure.Reading{InstanceID: instB.InstanceID, State: structure.StateOK}); err == nil {
		t.Fatal("expected denial for a scope the grant doesn't cover, got nil error")
	}
}

func TestWriteReadingUnknownInstance(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.whoever")
	if _, err := WriteReading(ctx, ghReader, vitalsReader, vitalsWriter, principalID, structure.Reading{InstanceID: uuid.New(), State: structure.StateOK}); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("expected ErrInstanceNotFound, got: %v", err)
	}
}

func TestGetReadingAndListHistoryRequirePermission(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	inst := seedDefinitionAndInstance(t, ctx, vitalsReader, vitalsWriter, "gatehouse.context", "myapp", "core.services.importer.status")
	writerPrincipal := setupPrincipal(t, ctx, ghReader, ghWriter, "service.writer")
	grantForScope(t, ctx, ghWriter, writerPrincipal, PermissionWrite, "gatehouse.context", "myapp")
	if _, err := WriteReading(ctx, ghReader, vitalsReader, vitalsWriter, writerPrincipal, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK}); err != nil {
		t.Fatalf("WriteReading: %v", err)
	}

	denied := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")
	if _, _, err := GetReading(ctx, ghReader, vitalsReader, denied, inst.InstanceID); err == nil {
		t.Fatal("expected GetReading denial, got nil error")
	}
	if _, err := ListHistory(ctx, ghReader, vitalsReader, denied, inst.InstanceID, 10); err == nil {
		t.Fatal("expected ListHistory denial, got nil error")
	}

	allowed := setupPrincipal(t, ctx, ghReader, ghWriter, "service.reader")
	grantForScope(t, ctx, ghWriter, allowed, PermissionRead, "gatehouse.context", "myapp")
	reading, found, err := GetReading(ctx, ghReader, vitalsReader, allowed, inst.InstanceID)
	if err != nil {
		t.Fatalf("GetReading: %v", err)
	}
	if !found || reading.State != structure.StateOK {
		t.Fatalf("GetReading = %+v, found=%v", reading, found)
	}
	history, err := ListHistory(ctx, ghReader, vitalsReader, allowed, inst.InstanceID, 10)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected exactly one history row, got %d", len(history))
	}
}

func TestEnsureInstanceRequiresWritePermissionOnDeclaredScope(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	now := time.Now().Truncate(time.Microsecond)
	def := structure.Definition{DefinitionID: uuid.New(), DefinitionKey: "myapp.importer.status", DefinitionVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := vitalsWriter.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}

	denied := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")
	if _, err := EnsureInstance(ctx, ghReader, vitalsReader, vitalsWriter, denied, structure.Instance{
		ScopeType: "gatehouse.context", ScopeID: "myapp", InstanceKey: "core.services.importer.status", DefinitionID: def.DefinitionID,
	}); err == nil {
		t.Fatal("expected denial, got nil error")
	}

	allowed := setupPrincipal(t, ctx, ghReader, ghWriter, "service.allowed")
	grantForScope(t, ctx, ghWriter, allowed, PermissionWrite, "gatehouse.context", "myapp")
	inst, err := EnsureInstance(ctx, ghReader, vitalsReader, vitalsWriter, allowed, structure.Instance{
		ScopeType: "gatehouse.context", ScopeID: "myapp", InstanceKey: "core.services.importer.status", DefinitionID: def.DefinitionID,
	})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	if inst.InstanceID == uuid.Nil {
		t.Fatal("expected EnsureInstance to assign an InstanceID")
	}
}

func TestEnsureDefinitionRequiresGlobalManagePermission(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	denied := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")
	if _, err := EnsureDefinition(ctx, ghReader, vitalsReader, vitalsWriter, denied, structure.Definition{
		DefinitionKey: "myapp.importer.status", DefinitionVersion: 1,
	}); err == nil {
		t.Fatal("expected denial, got nil error")
	}

	allowed := setupPrincipal(t, ctx, ghReader, ghWriter, "service.admin")
	grantGlobal(t, ctx, ghWriter, allowed, PermissionDefinitionManage)
	def, err := EnsureDefinition(ctx, ghReader, vitalsReader, vitalsWriter, allowed, structure.Definition{
		DefinitionKey: "myapp.importer.status", DefinitionVersion: 1,
	})
	if err != nil {
		t.Fatalf("EnsureDefinition: %v", err)
	}
	if def.DefinitionID == uuid.Nil {
		t.Fatal("expected EnsureDefinition to assign a DefinitionID")
	}
}

func TestGroupLifecycleRequiresPermission(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	inst := seedDefinitionAndInstance(t, ctx, vitalsReader, vitalsWriter, "gatehouse.context", "myapp", "core.services.importer.status")

	denied := setupPrincipal(t, ctx, ghReader, ghWriter, "service.denied")
	if _, err := EnsureGroup(ctx, ghReader, vitalsReader, vitalsWriter, denied, structure.Group{
		ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "default",
	}); err == nil {
		t.Fatal("expected EnsureGroup denial, got nil error")
	}

	manager := setupPrincipal(t, ctx, ghReader, ghWriter, "service.manager")
	grantForScope(t, ctx, ghWriter, manager, PermissionGroupManage, "gatehouse.context", "myapp")
	group, err := EnsureGroup(ctx, ghReader, vitalsReader, vitalsWriter, manager, structure.Group{
		ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "default",
	})
	if err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	if err := SetGroupMember(ctx, ghReader, vitalsReader, vitalsWriter, denied, structure.GroupMember{
		GroupID: group.GroupID, MemberKey: "importer", MemberKind: structure.MemberKindVitalInstance, VitalInstanceID: &inst.InstanceID,
	}); err == nil {
		t.Fatal("expected SetGroupMember denial, got nil error")
	}
	if err := SetGroupMember(ctx, ghReader, vitalsReader, vitalsWriter, manager, structure.GroupMember{
		GroupID: group.GroupID, MemberKey: "importer", MemberKind: structure.MemberKindVitalInstance, VitalInstanceID: &inst.InstanceID,
	}); err != nil {
		t.Fatalf("SetGroupMember: %v", err)
	}

	if _, err := ListGroupMembers(ctx, ghReader, vitalsReader, denied, group.GroupID); err == nil {
		t.Fatal("expected ListGroupMembers denial, got nil error")
	}
	reader := setupPrincipal(t, ctx, ghReader, ghWriter, "service.reader")
	grantForScope(t, ctx, ghWriter, reader, PermissionGroupRead, "gatehouse.context", "myapp")
	members, err := ListGroupMembers(ctx, ghReader, vitalsReader, reader, group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 1 || members[0].MemberKey != "importer" {
		t.Fatalf("expected exactly one member \"importer\", got %+v", members)
	}

	if err := RemoveGroupMember(ctx, ghReader, vitalsReader, vitalsWriter, denied, group.GroupID, "importer"); err == nil {
		t.Fatal("expected RemoveGroupMember denial, got nil error")
	}
	if err := RemoveGroupMember(ctx, ghReader, vitalsReader, vitalsWriter, manager, group.GroupID, "importer"); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
}

func TestGroupOperationsUnknownGroup(t *testing.T) {
	ghReader, ghWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	principalID := setupPrincipal(t, ctx, ghReader, ghWriter, "service.whoever")
	if _, err := ListGroupMembers(ctx, ghReader, vitalsReader, principalID, uuid.New()); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got: %v", err)
	}
	if err := SetGroupMember(ctx, ghReader, vitalsReader, vitalsWriter, principalID, structure.GroupMember{
		GroupID: uuid.New(), MemberKey: "x", MemberKind: structure.MemberKindVitalInstance, VitalInstanceID: &principalID,
	}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got: %v", err)
	}
}
