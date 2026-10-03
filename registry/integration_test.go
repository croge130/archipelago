// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. Peer identity is exercised
// over a real (not mocked) transit/inmem Session, same as peerauth's
// own tests — proving RegisterFromSession's identity resolution
// against the actual peerauth.ResolvePrincipal chokepoint, not a fake.
package registry

import (
	"context"
	"os"
	"testing"

	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/inmem"
)

func setupTest(t *testing.T) (*gatehouseDB.PostgresReader, *gatehouseDB.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping registry integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := gatehouseDB.Migrations()
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
		gatehouse_leases, gatehouse_instances,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return gatehouseDB.NewPostgresReader(pool.Pgx()), gatehouseDB.NewPostgresWriter(pool.Pgx())
}

func sessionWithFingerprint(fingerprint string) transit.Session {
	a, _ := inmem.NewPipe()
	return a.WithPeerIdentity(transit.PeerIdentity{Present: true, Fingerprint: fingerprint})
}

func TestRegisterFromSessionResolvesRealPeerIdentity(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}

	session := sessionWithFingerprint("sha256:abcd1234")
	inst, err := RegisterFromSession(ctx, reader, writer, session, "gamebridge.workers", nil)
	if err != nil {
		t.Fatalf("RegisterFromSession: %v", err)
	}
	if inst.PrincipalID != p.PrincipalID {
		t.Fatalf("Instance.PrincipalID = %s, want %s", inst.PrincipalID, p.PrincipalID)
	}
	if inst.Group != "gamebridge.workers" {
		t.Fatalf("Instance.Group = %q, want gamebridge.workers", inst.Group)
	}

	got, found, err := reader.GetInstance(ctx, inst.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if !found || got.PrincipalID != p.PrincipalID {
		t.Fatalf("GetInstance = %+v, found=%v", got, found)
	}
}

func TestRegisterFromSessionDeniedForUnknownPeer(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	session := sessionWithFingerprint("sha256:never-registered")
	if _, err := RegisterFromSession(ctx, reader, writer, session, "gamebridge.workers", nil); err != peerauth.ErrUnknownPeer {
		t.Fatalf("expected peerauth.ErrUnknownPeer, got: %v", err)
	}
}

func TestRegisterFromSessionDeniedForNoPeerIdentity(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	a, _ := inmem.NewPipe()
	if _, err := RegisterFromSession(ctx, reader, writer, a, "gamebridge.workers", nil); err != peerauth.ErrNoPeerIdentity {
		t.Fatalf("expected peerauth.ErrNoPeerIdentity, got: %v", err)
	}
}
