// Tests in this file need a real PostgreSQL instance. They skip,
// rather than fail, when ARCHIPELAGO_TEST_DATABASE_URL isn't set — the
// same convention db's own integration tests use.
package dbstore

import (
	"context"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

func setupTestStore(t *testing.T) (*PostgresReader, *PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping dbstore integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
	}

	// Clean slate per test — every table this package owns, truncated
	// together so FK order doesn't matter.
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
		t.Fatalf("truncate: %v", err)
	}

	return NewPostgresReader(pool.Pgx()), NewPostgresWriter(pool.Pgx())
}

func testPrincipal() structure.Principal {
	now := time.Now()
	return structure.Principal{
		PrincipalID: uuid.New(),
		Key:         "user." + uuid.New().String(),
		Type:        structure.PrincipalTypeUser,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestDBStoreCreatePrincipalAndReadBack(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	p := testPrincipal()
	if err := writer.CreatePrincipal(ctx, p); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	// No direct GetPrincipal on Store/Writer yet (not needed by
	// Evaluate), so confirm indirectly: a grant referencing this
	// principal as subject reads back correctly.
	grant := structure.Grant{
		GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypePrincipal, SubjectID: p.PrincipalID,
		TargetType: structure.GrantTargetTypePermission, PermissionKey: strPtrDB("myapp.readinglist.read"),
		Scope: structure.GrantScopeGlobal, Effect: structure.GrantEffectAllow,
		Status: structure.GrantStatusActive, Origin: structure.GrantOriginManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := writer.CreateGrant(ctx, grant); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	grants, err := reader.ActiveGrantsForSubject(ctx, structure.GrantSubjectTypePrincipal, p.PrincipalID)
	if err != nil {
		t.Fatalf("ActiveGrantsForSubject: %v", err)
	}
	if len(grants) != 1 || *grants[0].PermissionKey != "myapp.readinglist.read" {
		t.Fatalf("expected 1 grant for myapp.readinglist.read, got %+v", grants)
	}
}

func strPtrDB(s string) *string { return &s }

func TestDBStoreCreateMTLSCredentialAndGetByFingerprint(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	now := time.Now()
	cred := structure.Credential{
		CredentialID: uuid.New(),
		PrincipalID:  principal.PrincipalID,
		Kind:         structure.CredentialKindMTLSCertificate,
		Status:       structure.CredentialStatusActive,
		CreatedAt:    now,
	}
	detail := structure.MTLSCertCredDetail{CredentialID: cred.CredentialID, CertFingerprint: "sha256:abcd1234"}
	if err := writer.CreateMTLSCredential(ctx, cred, detail); err != nil {
		t.Fatalf("CreateMTLSCredential: %v", err)
	}

	gotCred, gotDetail, found, err := reader.GetCredentialByMTLSFingerprint(ctx, "sha256:abcd1234")
	if err != nil {
		t.Fatalf("GetCredentialByMTLSFingerprint: %v", err)
	}
	if !found || gotCred.PrincipalID != principal.PrincipalID || gotCred.Kind != structure.CredentialKindMTLSCertificate {
		t.Fatalf("GetCredentialByMTLSFingerprint = %+v, found=%v", gotCred, found)
	}
	if gotDetail.CertFingerprint != "sha256:abcd1234" {
		t.Fatalf("gotDetail.CertFingerprint = %q, want sha256:abcd1234", gotDetail.CertFingerprint)
	}
}

func TestDBStoreGetCredentialByMTLSFingerprintNotFound(t *testing.T) {
	reader, _ := setupTestStore(t)
	_, _, found, err := reader.GetCredentialByMTLSFingerprint(context.Background(), "sha256:never-registered")
	if err != nil {
		t.Fatalf("GetCredentialByMTLSFingerprint: %v", err)
	}
	if found {
		t.Fatal("expected not found for a never-registered fingerprint")
	}
}

func TestDBStoreCreateMTLSCredentialRejectsWrongKind(t *testing.T) {
	_, writer := setupTestStore(t)
	ctx := context.Background()
	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	cred := structure.Credential{
		CredentialID: uuid.New(), PrincipalID: principal.PrincipalID,
		Kind: structure.CredentialKindSessionToken, Status: structure.CredentialStatusActive, CreatedAt: time.Now(),
	}
	detail := structure.MTLSCertCredDetail{CredentialID: cred.CredentialID, CertFingerprint: "sha256:abcd1234"}
	if err := writer.CreateMTLSCredential(ctx, cred, detail); err == nil {
		t.Fatal("expected an error creating an mtls credential with Kind=session_token")
	}
}

func TestDBStoreEvaluateExactAllowAgainstRealPostgres(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if err := writer.RegisterPermissionDefinition(ctx, structure.PermissionDefinition{
		PermissionKey: "myapp.readinglist.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}); err != nil {
		t.Fatalf("RegisterPermissionDefinition: %v", err)
	}
	if err := writer.CreateGrant(ctx, structure.Grant{
		GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypePrincipal, SubjectID: principal.PrincipalID,
		TargetType: structure.GrantTargetTypePermission, PermissionKey: strPtrDB("myapp.readinglist.read"),
		Scope: structure.GrantScopeGlobal, Effect: structure.GrantEffectAllow,
		Status: structure.GrantStatusActive, Origin: structure.GrantOriginManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	decision, err := evaluation.Evaluate(ctx, reader, evaluation.Request{
		PrincipalID: principal.PrincipalID, PermissionKey: "myapp.readinglist.read",
		Scope: evaluation.ScopeGlobal, AuthorityLevel: evaluation.AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allow against real Postgres, got deny with reason %q", decision.Reason)
	}
}

func TestDBStoreEvaluateDenyAlwaysWinsAgainstRealPostgres(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if err := writer.RegisterPermissionDefinition(ctx, structure.PermissionDefinition{
		PermissionKey: "myapp.readinglist.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}); err != nil {
		t.Fatalf("RegisterPermissionDefinition: %v", err)
	}
	for _, effect := range []structure.GrantEffect{structure.GrantEffectAllow, structure.GrantEffectDeny} {
		if err := writer.CreateGrant(ctx, structure.Grant{
			GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypePrincipal, SubjectID: principal.PrincipalID,
			TargetType: structure.GrantTargetTypePermission, PermissionKey: strPtrDB("myapp.readinglist.read"),
			Scope: structure.GrantScopeGlobal, Effect: effect,
			Status: structure.GrantStatusActive, Origin: structure.GrantOriginManual,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("CreateGrant(%s): %v", effect, err)
		}
	}

	decision, err := evaluation.Evaluate(ctx, reader, evaluation.Request{
		PrincipalID: principal.PrincipalID, PermissionKey: "myapp.readinglist.read",
		Scope: evaluation.ScopeGlobal, AuthorityLevel: evaluation.AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected deny to win over allow against real Postgres data")
	}
}

func TestDBStoreEvaluateRoleExpansionAgainstRealPostgres(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if err := writer.RegisterPermissionDefinition(ctx, structure.PermissionDefinition{
		PermissionKey: "myapp.readinglist.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}); err != nil {
		t.Fatalf("RegisterPermissionDefinition: %v", err)
	}

	role := structure.Role{RoleID: uuid.New(), Key: "support-agent", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := writer.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := writer.AddRolePermission(ctx, structure.RolePermission{
		RoleID: role.RoleID, PermissionKey: strPtrDB("myapp.readinglist.read"), Effect: structure.GrantEffectAllow,
	}); err != nil {
		t.Fatalf("AddRolePermission: %v", err)
	}
	if err := writer.CreateGrant(ctx, structure.Grant{
		GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypePrincipal, SubjectID: principal.PrincipalID,
		TargetType: structure.GrantTargetTypeRole, RoleID: &role.RoleID,
		Scope: structure.GrantScopeGlobal, Effect: structure.GrantEffectAllow,
		Status: structure.GrantStatusActive, Origin: structure.GrantOriginManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateGrant (role target): %v", err)
	}

	decision, err := evaluation.Evaluate(ctx, reader, evaluation.Request{
		PrincipalID: principal.PrincipalID, PermissionKey: "myapp.readinglist.read",
		Scope: evaluation.ScopeGlobal, AuthorityLevel: evaluation.AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected the role's permission to apply against real Postgres, got deny with reason %q", decision.Reason)
	}
}

func TestDBStoreEvaluateGroupSourcedGrantAgainstRealPostgres(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if err := writer.RegisterPermissionDefinition(ctx, structure.PermissionDefinition{
		PermissionKey: "myapp.readinglist.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}); err != nil {
		t.Fatalf("RegisterPermissionDefinition: %v", err)
	}

	group := structure.Group{GroupID: uuid.New(), Key: "readers", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := writer.CreateGroup(ctx, group); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := writer.AddGroupMember(ctx, structure.GroupMembership{
		GroupID: group.GroupID, PrincipalID: principal.PrincipalID, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	if err := writer.CreateGrant(ctx, structure.Grant{
		GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypeGroup, SubjectID: group.GroupID,
		TargetType: structure.GrantTargetTypePermission, PermissionKey: strPtrDB("myapp.readinglist.read"),
		Scope: structure.GrantScopeGlobal, Effect: structure.GrantEffectAllow,
		Status: structure.GrantStatusActive, Origin: structure.GrantOriginManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateGrant (group subject): %v", err)
	}

	decision, err := evaluation.Evaluate(ctx, reader, evaluation.Request{
		PrincipalID: principal.PrincipalID, PermissionKey: "myapp.readinglist.read",
		Scope: evaluation.ScopeGlobal, AuthorityLevel: evaluation.AuthorityLevelStandard,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected the group-sourced grant to apply against real Postgres, got deny with reason %q", decision.Reason)
	}
}

func TestDBStoreGenerationBumpsOnGrantCreate(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	before, err := reader.GetGeneration(ctx)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if err := writer.CreateGrant(ctx, structure.Grant{
		GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypePrincipal, SubjectID: principal.PrincipalID,
		TargetType: structure.GrantTargetTypePermission, PermissionKey: strPtrDB("myapp.readinglist.read"),
		Scope: structure.GrantScopeGlobal, Effect: structure.GrantEffectAllow,
		Status: structure.GrantStatusActive, Origin: structure.GrantOriginManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	after, err := reader.GetGeneration(ctx)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}
	if after.PrincipalGrantGeneration != before.PrincipalGrantGeneration+1 {
		t.Errorf("principal_grant_generation = %d, want %d", after.PrincipalGrantGeneration, before.PrincipalGrantGeneration+1)
	}
	if after.PermissionSchemaGeneration != before.PermissionSchemaGeneration {
		t.Errorf("permission_schema_generation changed on a grant create, want unchanged: before=%d after=%d",
			before.PermissionSchemaGeneration, after.PermissionSchemaGeneration)
	}
}

func TestDBStoreGenerationBumpsOnPermissionRegister(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	before, err := reader.GetGeneration(ctx)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}
	if err := writer.RegisterPermissionDefinition(ctx, structure.PermissionDefinition{
		PermissionKey: "myapp.readinglist.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}); err != nil {
		t.Fatalf("RegisterPermissionDefinition: %v", err)
	}
	after, err := reader.GetGeneration(ctx)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}
	if after.PermissionSchemaGeneration != before.PermissionSchemaGeneration+1 {
		t.Errorf("permission_schema_generation = %d, want %d", after.PermissionSchemaGeneration, before.PermissionSchemaGeneration+1)
	}
}

func TestDBStoreCreateSessionAndGetBack(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	now := time.Now()
	s := structure.Session{
		SessionID: uuid.New(), PrincipalID: principal.PrincipalID,
		Kind: structure.SessionKindService, AuthorityLevel: structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate", CreatedAt: now, LastSeen: now,
	}
	if err := writer.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, found, err := reader.GetSession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !found || got.PrincipalID != principal.PrincipalID || got.Kind != structure.SessionKindService {
		t.Fatalf("GetSession = %+v, found=%v", got, found)
	}
	if got.RevokedAt != nil {
		t.Fatalf("expected a freshly created session to not be revoked, got %+v", got.RevokedAt)
	}
}

func TestDBStoreGetSessionNotFound(t *testing.T) {
	reader, _ := setupTestStore(t)
	_, found, err := reader.GetSession(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if found {
		t.Fatal("expected not found for a never-created session")
	}
}

func TestDBStoreRevokeSession(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	now := time.Now()
	s := structure.Session{
		SessionID: uuid.New(), PrincipalID: principal.PrincipalID,
		Kind: structure.SessionKindService, AuthorityLevel: structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate", CreatedAt: now, LastSeen: now,
	}
	if err := writer.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := writer.RevokeSession(ctx, s.SessionID, time.Now()); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	got, found, err := reader.GetSession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !found || got.RevokedAt == nil {
		t.Fatalf("expected the session to be revoked, got %+v", got)
	}
}

func TestDBStoreRevokeSessionIsIdempotent(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	now := time.Now()
	s := structure.Session{
		SessionID: uuid.New(), PrincipalID: principal.PrincipalID,
		Kind: structure.SessionKindService, AuthorityLevel: structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate", CreatedAt: now, LastSeen: now,
	}
	if err := writer.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	first := time.Now()
	if err := writer.RevokeSession(ctx, s.SessionID, first); err != nil {
		t.Fatalf("RevokeSession (first): %v", err)
	}
	// A second revoke must not overwrite the original revocation time
	// — the WHERE revoked_at IS NULL guard makes this a no-op.
	if err := writer.RevokeSession(ctx, s.SessionID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("RevokeSession (second): %v", err)
	}
	got, found, err := reader.GetSession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !found || got.RevokedAt == nil {
		t.Fatalf("expected the session to still be revoked, got %+v", got)
	}
	if !got.RevokedAt.Before(first.Add(time.Second)) {
		t.Fatalf("expected the original revocation time to stick, got %v", got.RevokedAt)
	}
}

func testInstance(t *testing.T, ctx context.Context, writer *PostgresWriter, group string) structure.Instance {
	t.Helper()
	principal := testPrincipal()
	if err := writer.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	now := time.Now().Truncate(time.Microsecond) // matches timestamptz's own precision, see facade's EnsurePrincipal comment
	i := structure.Instance{
		InstanceID: uuid.New(), PrincipalID: principal.PrincipalID, Group: group,
		RegisteredAt: now, LastHeartbeatAt: now,
	}
	if err := writer.UpsertInstance(ctx, i); err != nil {
		t.Fatalf("UpsertInstance: %v", err)
	}
	return i
}

func TestDBStoreUpsertInstanceAndGetBack(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	i := testInstance(t, ctx, writer, "gamebridge.workers")

	got, found, err := reader.GetInstance(ctx, i.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if !found || got.PrincipalID != i.PrincipalID || got.Group != "gamebridge.workers" {
		t.Fatalf("GetInstance = %+v, found=%v", got, found)
	}
}

func TestDBStoreGetInstanceNotFound(t *testing.T) {
	reader, _ := setupTestStore(t)
	_, found, err := reader.GetInstance(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if found {
		t.Fatal("expected not found for a never-registered instance")
	}
}

func TestDBStoreUpsertInstanceUpdatesHeartbeat(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	i := testInstance(t, ctx, writer, "gamebridge.workers")
	i.LastHeartbeatAt = i.LastHeartbeatAt.Add(time.Minute)
	if err := writer.UpsertInstance(ctx, i); err != nil {
		t.Fatalf("UpsertInstance (heartbeat): %v", err)
	}

	got, found, err := reader.GetInstance(ctx, i.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if !found || !got.LastHeartbeatAt.Equal(i.LastHeartbeatAt) {
		t.Fatalf("expected the heartbeat to be updated, got %+v", got)
	}
}

func TestDBStoreDeleteInstance(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	i := testInstance(t, ctx, writer, "gamebridge.workers")
	if err := writer.DeleteInstance(ctx, i.InstanceID); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	_, found, err := reader.GetInstance(ctx, i.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if found {
		t.Fatal("expected the instance to be gone after DeleteInstance")
	}
}

func TestDBStoreListInstancesByGroupRespectsStalenessCutoff(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	fresh := testInstance(t, ctx, writer, "gamebridge.workers")

	stale := testInstance(t, ctx, writer, "gamebridge.workers")
	stale.LastHeartbeatAt = time.Now().Add(-time.Hour)
	if err := writer.UpsertInstance(ctx, stale); err != nil {
		t.Fatalf("UpsertInstance (stale): %v", err)
	}

	testInstance(t, ctx, writer, "storage-manager.workers")

	got, err := reader.ListInstancesByGroup(ctx, "gamebridge.workers", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListInstancesByGroup: %v", err)
	}
	if len(got) != 1 || got[0].InstanceID != fresh.InstanceID {
		t.Fatalf("ListInstancesByGroup = %+v, want exactly [%s]", got, fresh.InstanceID)
	}
}

func TestDBStoreAcquireLeaseThenConflictThenRelease(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	holder := testInstance(t, ctx, writer, "gamebridge.workers")
	rival := testInstance(t, ctx, writer, "gamebridge.workers")

	now := time.Now()
	lease, ok, err := writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", holder.InstanceID, now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("AcquireOrRenewLease (first): %v", err)
	}
	if !ok || lease.HolderInstanceID != holder.InstanceID {
		t.Fatalf("expected the first acquire to succeed, got ok=%v lease=%+v", ok, lease)
	}

	// A rival instance trying to acquire the same, unexpired lease must
	// be rejected, not silently steal it.
	_, ok, err = writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", rival.InstanceID, time.Now(), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("AcquireOrRenewLease (rival): %v", err)
	}
	if ok {
		t.Fatal("expected the rival's acquire to fail while the lease is still held and unexpired")
	}

	// The original holder renewing its own lease must succeed.
	renewed, ok, err := writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", holder.InstanceID, time.Now(), time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("AcquireOrRenewLease (renew): %v", err)
	}
	if !ok || renewed.HolderInstanceID != holder.InstanceID {
		t.Fatalf("expected the holder's own renewal to succeed, got ok=%v lease=%+v", ok, renewed)
	}

	if err := writer.ReleaseLease(ctx, "gamebridge.workers", "reconciler", holder.InstanceID); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	_, found, err := reader.GetLease(ctx, "gamebridge.workers", "reconciler")
	if err != nil {
		t.Fatalf("GetLease: %v", err)
	}
	if found {
		t.Fatal("expected the lease to be gone after ReleaseLease")
	}

	// Now the rival can acquire the freshly released lease.
	claimed, ok, err := writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", rival.InstanceID, time.Now(), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("AcquireOrRenewLease (rival after release): %v", err)
	}
	if !ok || claimed.HolderInstanceID != rival.InstanceID {
		t.Fatalf("expected the rival to acquire the released lease, got ok=%v lease=%+v", ok, claimed)
	}
}

func TestDBStoreAcquireLeaseExpiredIsReclaimable(t *testing.T) {
	_, writer := setupTestStore(t)
	ctx := context.Background()

	holder := testInstance(t, ctx, writer, "gamebridge.workers")
	rival := testInstance(t, ctx, writer, "gamebridge.workers")

	past := time.Now().Add(-time.Hour)
	if _, ok, err := writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", holder.InstanceID, past, past.Add(time.Second)); err != nil || !ok {
		t.Fatalf("AcquireOrRenewLease (expired-on-arrival): ok=%v err=%v", ok, err)
	}

	claimed, ok, err := writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", rival.InstanceID, time.Now(), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("AcquireOrRenewLease (reclaim): %v", err)
	}
	if !ok || claimed.HolderInstanceID != rival.InstanceID {
		t.Fatalf("expected an expired lease to be reclaimable by a different instance, got ok=%v lease=%+v", ok, claimed)
	}
}

func TestDBStoreReleaseLeaseByNonHolderIsNoop(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()

	holder := testInstance(t, ctx, writer, "gamebridge.workers")
	impostor := testInstance(t, ctx, writer, "gamebridge.workers")

	now := time.Now()
	if _, ok, err := writer.AcquireOrRenewLease(ctx, "gamebridge.workers", "reconciler", holder.InstanceID, now, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("AcquireOrRenewLease: ok=%v err=%v", ok, err)
	}

	if err := writer.ReleaseLease(ctx, "gamebridge.workers", "reconciler", impostor.InstanceID); err != nil {
		t.Fatalf("ReleaseLease (non-holder): %v", err)
	}

	got, found, err := reader.GetLease(ctx, "gamebridge.workers", "reconciler")
	if err != nil {
		t.Fatalf("GetLease: %v", err)
	}
	if !found || got.HolderInstanceID != holder.InstanceID {
		t.Fatalf("expected the real holder's lease to survive a non-holder's release, got found=%v lease=%+v", found, got)
	}
}
