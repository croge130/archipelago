// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. They exercise peerauth
// through gatehouse-core's real Postgres-backed facade.Reader —
// proving peerauth.Store is actually satisfied by it — paired with
// transit/inmem sessions, whose WithPeerIdentity test accessor exists
// for exactly this: exercising an mTLS-aware caller without a real
// certificate or TLS handshake.
package peerauth

import (
	"context"
	"errors"
	"os"
	"testing"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/inmem"
)

func setupTest(t *testing.T) (*dbstore.PostgresReader, *dbstore.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping peerauth integration test")
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

func sessionWithFingerprint(fingerprint string) transit.Session {
	a, _ := inmem.NewPipe()
	return a.WithPeerIdentity(transit.PeerIdentity{Present: true, Fingerprint: fingerprint})
}

func TestResolvePrincipalViaBoundCredential(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := facade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if _, err := facade.EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}

	session := sessionWithFingerprint("sha256:abcd1234")
	got, err := ResolvePrincipal(ctx, reader, session)
	if err != nil {
		t.Fatalf("ResolvePrincipal: %v", err)
	}
	if got != p.PrincipalID {
		t.Fatalf("ResolvePrincipal = %s, want %s", got, p.PrincipalID)
	}
}

func TestResolvePrincipalNoPeerIdentity(t *testing.T) {
	reader, _ := setupTest(t)
	a, _ := inmem.NewPipe() // no WithPeerIdentity — Present stays false
	if _, err := ResolvePrincipal(context.Background(), reader, a); !errors.Is(err, ErrNoPeerIdentity) {
		t.Fatalf("expected ErrNoPeerIdentity, got: %v", err)
	}
}

func TestResolvePrincipalUnknownFingerprint(t *testing.T) {
	reader, _ := setupTest(t)
	session := sessionWithFingerprint("sha256:never-registered")
	if _, err := ResolvePrincipal(context.Background(), reader, session); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("expected ErrUnknownPeer, got: %v", err)
	}
}

func TestRequireGrantedPermission(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := facade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if _, err := facade.EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}
	if err := facade.RegisterPermission(ctx, writer, structure.PermissionDefinition{
		PermissionKey: "myapp.storage.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, facade.RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	if _, err := facade.GrantPermission(ctx, writer, structure.GrantSubjectTypePrincipal, p.PrincipalID, "myapp.storage.read"); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}

	session := sessionWithFingerprint("sha256:abcd1234")
	if err := Require(ctx, reader, session, "myapp.storage.read"); err != nil {
		t.Fatalf("Require: %v", err)
	}
}

func TestRequireUngrantedPermissionDenied(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := facade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if _, err := facade.EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}
	if err := facade.RegisterPermission(ctx, writer, structure.PermissionDefinition{
		PermissionKey: "myapp.storage.read", RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, facade.RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	// Deliberately no grant.

	session := sessionWithFingerprint("sha256:abcd1234")
	if err := Require(ctx, reader, session, "myapp.storage.read"); err == nil {
		t.Fatal("expected Require to fail for a principal with no matching grant")
	}
}

func TestRequireFailsBeforeEvaluatingWithNoPeerIdentity(t *testing.T) {
	reader, _ := setupTest(t)
	a, _ := inmem.NewPipe()
	if err := Require(context.Background(), reader, a, "myapp.storage.read"); !errors.Is(err, ErrNoPeerIdentity) {
		t.Fatalf("expected ErrNoPeerIdentity, got: %v", err)
	}
}
