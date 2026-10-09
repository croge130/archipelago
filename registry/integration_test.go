// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. Peer identity is exercised
// over a real (not mocked) transit/inmem Session, same as peerauth's
// own tests — proving RegisterFromSession's identity resolution
// against the actual peerauth.ResolvePrincipal chokepoint, not a fake.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/croge130/archipelago/wire"
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

// connectRouter serves RegisterHandler on a router whose accepting
// session reports fingerprint as the verified peer identity ("" = none).
func connectRouter(t *testing.T, reader gatehouseFacade.Reader, writer gatehouseFacade.Writer, fingerprint string) *router.Peer {
	t.Helper()
	quiet := router.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	server := router.New(quiet)
	if err := server.Handle(router.Route{Type: RouteRegister, Handler: RegisterHandler(reader, writer)}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	dc, ac := inmem.NewPipe()
	if fingerprint != "" {
		ac.WithPeerIdentity(transit.PeerIdentity{Present: true, Fingerprint: fingerprint})
	}
	a := server.Accept(ac)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, err := router.New(quiet).Connect(ctx, dc, "test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { d.Close(); a.Close() })
	return d
}

func TestRegisterOverARouterBindsTheInstanceToTheVerifiedPeer(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()
	p, err := gatehouseFacade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatal(err)
	}

	peer := connectRouter(t, reader, writer, "sha256:abcd1234")
	inst, err := Register(ctx, peer, "gamebridge.workers", json.RawMessage(`{"zone":"a"}`))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if inst.PrincipalID != p.PrincipalID || inst.Group != "gamebridge.workers" {
		t.Fatalf("Instance = %+v", inst)
	}
	if stored, found, _ := reader.GetInstance(ctx, inst.InstanceID); !found || stored.PrincipalID != p.PrincipalID || !jsonEqual(stored.Metadata, `{"zone":"a"}`) {
		t.Fatalf("stored = %+v found=%v", stored, found)
	}
}

func TestRegisterOverARouterRefusesPeersThatResolveToNoPrincipal(t *testing.T) {
	reader, writer := setupTest(t)
	for name, fp := range map[string]string{"unknown certificate": "sha256:never-registered", "no identity": ""} {
		t.Run(name, func(t *testing.T) {
			_, err := Register(context.Background(), connectRouter(t, reader, writer, fp), "g", nil)
			var re *router.RemoteError
			if !errors.As(err, &re) || re.Code != wire.ErrUnauthorized {
				t.Fatalf("Register = %v, want remote code unauthorized", err)
			}
		})
	}
}

func TestRegisterOverARouterRejectsABadRequestAsInvalid(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()
	p, _ := gatehouseFacade.EnsurePrincipal(ctx, reader, writer, "service.gamebridge", gatehouseStructure.PrincipalTypeServiceAccount)
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, reader, writer, p.PrincipalID, "sha256:abcd1234"); err != nil {
		t.Fatal(err)
	}
	peer := connectRouter(t, reader, writer, "sha256:abcd1234")
	for name, payload := range map[string]string{"no group": `{"group":""}`, "not json": `[1,2`} {
		_, err := peer.Call(ctx, RouteRegister, json.RawMessage(payload))
		var re *router.RemoteError
		if !errors.As(err, &re) || re.Code != wire.ErrInvalid {
			t.Fatalf("%s: Call = %v, want remote code invalid", name, err)
		}
	}
}

// jsonEqual compares JSON semantically: Postgres's jsonb reformats what
// it stores (adds a space after colons), so byte equality would fail.
func jsonEqual(got json.RawMessage, want string) bool {
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
		return false
	}
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	return string(ga) == string(gb)
}
