// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. They exercise this package
// through gatehouse-core's real Postgres-backed facade.Reader/Writer
// — proving sessions.Store is actually satisfied by it — paired with
// transit/inmem's real (not mocked) connection lifecycle, so Done()
// actually fires the way a dropped network connection would.
package sessions

import (
	"context"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/google/uuid"
)

func setupTest(t *testing.T) (*dbstore.PostgresReader, *dbstore.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping sessions integration test")
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

func waitForRevocation(t *testing.T, reader *dbstore.PostgresReader, sessionID uuid.UUID, deadline time.Duration) structure.Session {
	t.Helper()
	// The revoke happens in a background goroutine racing this
	// assertion, so poll briefly rather than asserting immediately —
	// the one place in this test suite that genuinely needs it, since
	// everywhere else in this design, writes complete before the call
	// that triggered them returns.
	ctx := context.Background()
	deadlineAt := time.Now().Add(deadline)
	for {
		got, found, err := reader.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if found && got.RevokedAt != nil {
			return got
		}
		if time.Now().After(deadlineAt) {
			t.Fatalf("session was not revoked within %v (found=%v, got=%+v)", deadline, found, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBindConnectionCreatesSession(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := facade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	conn, _ := inmem.NewPipe()
	s, err := BindConnection(ctx, writer, conn, structure.Session{
		PrincipalID:          p.PrincipalID,
		Kind:                 structure.SessionKindService,
		AuthorityLevel:       structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate",
	})
	if err != nil {
		t.Fatalf("BindConnection: %v", err)
	}

	got, found, err := reader.GetSession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !found || got.RevokedAt != nil {
		t.Fatalf("expected a freshly bound session to be found and not revoked, got %+v found=%v", got, found)
	}
}

func TestBindConnectionRevokesOnDisconnect(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := facade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	conn, _ := inmem.NewPipe()
	s, err := BindConnection(ctx, writer, conn, structure.Session{
		PrincipalID:          p.PrincipalID,
		Kind:                 structure.SessionKindService,
		AuthorityLevel:       structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate",
	})
	if err != nil {
		t.Fatalf("BindConnection: %v", err)
	}

	conn.Close() // simulate the connection dropping

	got := waitForRevocation(t, reader, s.SessionID, 2*time.Second)
	if got.PrincipalID != p.PrincipalID {
		t.Fatalf("got.PrincipalID = %s, want %s", got.PrincipalID, p.PrincipalID)
	}
}

func TestBindConnectionRejectsAlreadyClosedConnection(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	p, err := facade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	conn, _ := inmem.NewPipe()
	conn.Close() // already closed before BindConnection is even called

	if _, err := BindConnection(ctx, writer, conn, structure.Session{
		PrincipalID:          p.PrincipalID,
		Kind:                 structure.SessionKindService,
		AuthorityLevel:       structure.AuthorityLevelStandard,
		AuthenticationMethod: "mtls_certificate",
	}); err == nil {
		t.Fatal("expected BindConnection to reject an already-closed connection")
	}
}
