// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL, same convention as db's and
// dbstore's own integration tests. They exercise the facade through
// dbstore's real PostgresReader/PostgresWriter — proving facade.Reader
// and facade.Writer are actually satisfied by the concrete
// implementation, not just shaped to look like they would be.
package facade

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
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
		gatehouse_leases, gatehouse_instances, gatehouse_endpoint_definitions,
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

func TestEnsureMTLSCredentialIdempotent(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	p, err := EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	first, err := EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234")
	if err != nil {
		t.Fatalf("EnsureMTLSCredential (first): %v", err)
	}
	second, err := EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234")
	if err != nil {
		t.Fatalf("EnsureMTLSCredential (second): %v", err)
	}
	if first.CredentialID != second.CredentialID {
		t.Fatal("expected the second EnsureMTLSCredential call to return the same credential")
	}
}

func TestEnsureMTLSCredentialConflict(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	a, err := EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal (a): %v", err)
	}
	b, err := EnsurePrincipal(ctx, reader, writer, "service.torrent", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal (b): %v", err)
	}

	if _, err := EnsureMTLSCredential(ctx, reader, writer, a.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatalf("EnsureMTLSCredential (a): %v", err)
	}
	_, err = EnsureMTLSCredential(ctx, reader, writer, b.PrincipalID, "sha256:abcd1234")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict binding the same fingerprint to a different principal, got: %v", err)
	}
}

func TestRegisterPermissionBlocksReservedNamespaceByDefault(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
		PermissionKey:          "gatehouse.principal.delete",
		RequiredAuthorityLevel: structure.AuthorityLevelElevated,
	}, RegisterPermissionOptions{})
	if err == nil {
		t.Fatal("expected RegisterPermission to block a reserved namespace by default")
	}
}

func TestRegisterPermissionAllowsReservedNamespaceWithOverride(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
		PermissionKey:          "gatehouse.principal.delete",
		RequiredAuthorityLevel: structure.AuthorityLevelElevated,
	}, RegisterPermissionOptions{AllowReservedNamespace: true})
	if err != nil {
		t.Fatalf("expected RegisterPermission to allow a reserved namespace with the override set, got: %v", err)
	}
}

func TestRegisterPermissionAllowsOrdinaryAppNamespace(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.read",
		RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, RegisterPermissionOptions{})
	if err != nil {
		t.Fatalf("expected an ordinary app-namespaced permission to register without the override, got: %v", err)
	}
}

func TestRegisterPermissionIsIdempotent(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	def := structure.PermissionDefinition{
		PermissionKey:          "myapp.readinglist.read",
		RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}
	if err := RegisterPermission(ctx, reader, writer, def, RegisterPermissionOptions{}); err != nil {
		t.Fatalf("first RegisterPermission: %v", err)
	}
	// Same definition again: a no-op, not a unique-constraint error —
	// the whole point of making this "ensure"-shaped rather than a raw
	// insert, since it's meant to be safe to call on every app startup.
	if err := RegisterPermission(ctx, reader, writer, def, RegisterPermissionOptions{}); err != nil {
		t.Fatalf("second RegisterPermission with the same definition: %v", err)
	}

	conflicting := def
	conflicting.RequiredAuthorityLevel = structure.AuthorityLevelElevated
	if err := RegisterPermission(ctx, reader, writer, conflicting, RegisterPermissionOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict re-registering %q with a different definition, got: %v", def.PermissionKey, err)
	}
}

func TestGrantPermissionAndEvaluateEndToEnd(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	principal, err := EnsurePrincipal(ctx, reader, writer, "user.christian", structure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if err := RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
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

func TestCreateSessionAndRevoke(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	p, err := EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	s, err := CreateSession(ctx, writer, structure.Session{
		PrincipalID:          p.PrincipalID,
		Kind:                 structure.SessionKindService,
		AuthorityLevel:       structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.SessionID == uuid.Nil {
		t.Fatal("expected CreateSession to assign a SessionID")
	}

	got, found, err := reader.GetSession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !found || got.RevokedAt != nil {
		t.Fatalf("expected a freshly created session to be found and not revoked, got %+v found=%v", got, found)
	}

	if err := RevokeSession(ctx, writer, s.SessionID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	got, found, err = reader.GetSession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !found || got.RevokedAt == nil {
		t.Fatalf("expected the session to be revoked, got %+v found=%v", got, found)
	}
}

func TestRegisterInstanceAndHeartbeat(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	p, err := EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	inst, err := RegisterInstance(ctx, writer, p.PrincipalID, "gamebridge.workers", nil)
	if err != nil {
		t.Fatalf("RegisterInstance: %v", err)
	}
	if inst.InstanceID == uuid.Nil {
		t.Fatal("expected RegisterInstance to assign an InstanceID")
	}

	firstHeartbeat := inst.LastHeartbeatAt
	time.Sleep(time.Millisecond) // guarantee a distinguishable timestamp
	updated, err := Heartbeat(ctx, reader, writer, inst.InstanceID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !updated.LastHeartbeatAt.After(firstHeartbeat) {
		t.Fatalf("expected Heartbeat to advance LastHeartbeatAt, got %v (was %v)", updated.LastHeartbeatAt, firstHeartbeat)
	}

	if err := Deregister(ctx, writer, inst.InstanceID); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if _, err := Heartbeat(ctx, reader, writer, inst.InstanceID); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("expected ErrInstanceNotFound after Deregister, got: %v", err)
	}
}

func TestListPeersRespectsActiveWithin(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	p, err := EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	fresh, err := RegisterInstance(ctx, writer, p.PrincipalID, "gamebridge.workers", nil)
	if err != nil {
		t.Fatalf("RegisterInstance (fresh): %v", err)
	}
	stale, err := RegisterInstance(ctx, writer, p.PrincipalID, "gamebridge.workers", nil)
	if err != nil {
		t.Fatalf("RegisterInstance (stale): %v", err)
	}
	stale.LastHeartbeatAt = time.Now().Add(-time.Hour)
	if err := writer.UpsertInstance(ctx, stale); err != nil {
		t.Fatalf("UpsertInstance (age stale instance): %v", err)
	}

	peers, err := ListPeers(ctx, reader, "gamebridge.workers", time.Minute)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].InstanceID != fresh.InstanceID {
		t.Fatalf("ListPeers = %+v, want exactly [%s]", peers, fresh.InstanceID)
	}
}

func TestAcquireOrRenewLeaseAndRelease(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	p, err := EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	holder, err := RegisterInstance(ctx, writer, p.PrincipalID, "gamebridge.workers", nil)
	if err != nil {
		t.Fatalf("RegisterInstance (holder): %v", err)
	}
	rival, err := RegisterInstance(ctx, writer, p.PrincipalID, "gamebridge.workers", nil)
	if err != nil {
		t.Fatalf("RegisterInstance (rival): %v", err)
	}

	if _, ok, err := AcquireOrRenewLease(ctx, writer, "gamebridge.workers", "reconciler", holder.InstanceID, time.Minute); err != nil || !ok {
		t.Fatalf("AcquireOrRenewLease (holder): ok=%v err=%v", ok, err)
	}
	if _, ok, err := AcquireOrRenewLease(ctx, writer, "gamebridge.workers", "reconciler", rival.InstanceID, time.Minute); err != nil || ok {
		t.Fatalf("expected the rival's acquire to fail, got ok=%v err=%v", ok, err)
	}

	if err := ReleaseLease(ctx, writer, "gamebridge.workers", "reconciler", holder.InstanceID); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	if _, ok, err := AcquireOrRenewLease(ctx, writer, "gamebridge.workers", "reconciler", rival.InstanceID, time.Minute); err != nil || !ok {
		t.Fatalf("expected the rival to acquire the released lease, got ok=%v err=%v", ok, err)
	}
}

func TestRegisterEndpointRejectsUnregisteredPermission(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{
		EndpointKey: "myapp.importer.run", RequiredPermissionKey: "myapp.importer.write",
	}, RegisterEndpointOptions{})
	if !errors.Is(err, ErrPermissionNotRegistered) {
		t.Fatalf("expected ErrPermissionNotRegistered when RequiredPermissionKey was never registered, got: %v", err)
	}
}

func TestRegisterEndpointAllowsNoPermissionRequired(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	if err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{
		EndpointKey: "myapp.health.check",
	}, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("expected a public endpoint with no RequiredPermissionKey to register, got: %v", err)
	}
}

func TestRegisterEndpointIsIdempotentIncludingNilMetadata(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	def := structure.EndpointDefinition{EndpointKey: "myapp.health.check", Description: "liveness"}
	if err := RegisterEndpoint(ctx, reader, writer, def, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("first RegisterEndpoint: %v", err)
	}
	// Same definition again, Metadata still nil on this call too — must
	// stay a no-op even though the stored row's own metadata column
	// round-tripped nil to the literal "{}" on the first insert; a
	// naive equality check comparing those raw bytes would wrongly see
	// this as a conflict. See sameEndpointDefinition's doc comment.
	if err := RegisterEndpoint(ctx, reader, writer, def, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("second RegisterEndpoint with the identical definition (nil Metadata both times): %v", err)
	}

	conflicting := def
	conflicting.Description = "something else entirely"
	if err := RegisterEndpoint(ctx, reader, writer, conflicting, RegisterEndpointOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict re-registering %q with a different definition, got: %v", def.EndpointKey, err)
	}
}

func TestRegisterEndpointBlocksReservedNamespaceByDefault(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{
		EndpointKey: "gatehouse.principal.delete",
	}, RegisterEndpointOptions{})
	if err == nil {
		t.Fatal("expected RegisterEndpoint to block a reserved namespace by default")
	}
}

func TestListEndpointsReturnsEveryRegistration(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	if err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{EndpointKey: "myapp.health.check"}, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("RegisterEndpoint (health.check): %v", err)
	}
	if err := RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
		PermissionKey: "myapp.importer.write", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	if err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{
		EndpointKey: "myapp.importer.run", RequiredPermissionKey: "myapp.importer.write",
	}, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("RegisterEndpoint (importer.run): %v", err)
	}

	got, err := ListEndpoints(ctx, reader)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListEndpoints = %+v, want 2 registered endpoints", got)
	}
}

func TestAdvertiseEndpointsFiltersByGrant(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	principal, err := EnsurePrincipal(ctx, reader, writer, "user.christian", structure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{EndpointKey: "myapp.health.check"}, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("RegisterEndpoint (health.check): %v", err)
	}
	if err := RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
		PermissionKey: "myapp.importer.write", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	if err := RegisterEndpoint(ctx, reader, writer, structure.EndpointDefinition{
		EndpointKey: "myapp.importer.run", RequiredPermissionKey: "myapp.importer.write",
	}, RegisterEndpointOptions{}); err != nil {
		t.Fatalf("RegisterEndpoint (importer.run): %v", err)
	}

	// filterByGrant=false: the show-all-and-deny-at-call-time posture —
	// both endpoints, regardless of what the principal is actually
	// allowed to call.
	all, err := AdvertiseEndpoints(ctx, reader, principal.PrincipalID, false)
	if err != nil {
		t.Fatalf("AdvertiseEndpoints (unfiltered): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("AdvertiseEndpoints(filterByGrant=false) = %+v, want both endpoints", all)
	}

	// filterByGrant=true, no grant yet: only the public endpoint with
	// no RequiredPermissionKey is visible.
	visible, err := AdvertiseEndpoints(ctx, reader, principal.PrincipalID, true)
	if err != nil {
		t.Fatalf("AdvertiseEndpoints (filtered, no grant): %v", err)
	}
	if len(visible) != 1 || visible[0].EndpointKey != "myapp.health.check" {
		t.Fatalf("AdvertiseEndpoints(filterByGrant=true) with no grant = %+v, want only myapp.health.check", visible)
	}

	// Grant the permission the other endpoint requires: now both are
	// visible, through the exact same evaluator RequirePermission
	// itself uses — no separate advertisement-specific authorization
	// path to drift from it.
	if _, err := GrantPermission(ctx, writer, structure.GrantSubjectTypePrincipal, principal.PrincipalID, "myapp.importer.write"); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
	visible, err = AdvertiseEndpoints(ctx, reader, principal.PrincipalID, true)
	if err != nil {
		t.Fatalf("AdvertiseEndpoints (filtered, after grant): %v", err)
	}
	if len(visible) != 2 {
		t.Fatalf("AdvertiseEndpoints(filterByGrant=true) after granting myapp.importer.write = %+v, want both endpoints", visible)
	}
}
