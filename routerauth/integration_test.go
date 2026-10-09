// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. Two routers are connected over the
// in-memory pipe and the accepting side's session carries a verified-peer
// identity via WithPeerIdentity, so the whole path under test — handshake,
// authorizer, peerauth, Gatehouse-core's evaluator — is the real one.
package routerauth

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
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/croge130/archipelago/wire"
)

func setupTest(t *testing.T) (*dbstore.PostgresReader, *dbstore.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping routerauth integration test")
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
	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_leases, gatehouse_instances, gatehouse_endpoint_definitions,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx())
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

type env struct {
	reader *dbstore.PostgresReader
	writer *dbstore.PostgresWriter
	router *router.Router
	reg    *Registrar
}

func newEnv(t *testing.T) *env {
	t.Helper()
	reader, writer := setupTest(t)
	r := router.New(router.Options{Logger: quiet()})
	reg, err := New(r, reader, writer)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &env{reader: reader, writer: writer, router: r, reg: reg}
}

// principal creates a service account bound to fingerprint, optionally
// holding permissions (registered here if not yet registered).
func (e *env) principal(t *testing.T, key, fingerprint string, perms ...string) {
	t.Helper()
	ctx := context.Background()
	p, err := facade.EnsurePrincipal(ctx, e.reader, e.writer, key, structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if _, err := facade.EnsureMTLSCredential(ctx, e.reader, e.writer, p.PrincipalID, fingerprint); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}
	for _, perm := range perms {
		e.permission(t, perm)
		if _, err := facade.GrantPermission(ctx, e.writer, structure.GrantSubjectTypePrincipal, p.PrincipalID, perm); err != nil {
			t.Fatalf("GrantPermission: %v", err)
		}
	}
}

func (e *env) permission(t *testing.T, key string) {
	t.Helper()
	if err := facade.RegisterPermission(context.Background(), e.reader, e.writer, structure.PermissionDefinition{
		PermissionKey: key, RequiredAuthorityLevel: structure.AuthorityLevelStandard,
	}, facade.RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
}

// connect returns the dialing peer. The accepting session reports
// fingerprint as its verified peer identity ("" means no identity).
func (e *env) connect(t *testing.T, fingerprint string) *router.Peer {
	t.Helper()
	dc, ac := inmem.NewPipe()
	if fingerprint != "" {
		ac.WithPeerIdentity(transit.PeerIdentity{Present: true, Fingerprint: fingerprint})
	}
	a := e.router.Accept(ac)
	d, err := router.New(router.Options{Logger: quiet()}).Connect(ctxT(t), dc, "test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { d.Close(); a.Close() })
	return d
}

func echo(_ context.Context, req router.Request) (json.RawMessage, error) {
	return req.Message.Payload, nil
}

func wantCode(t *testing.T, err error, code wire.ErrorCode) {
	t.Helper()
	var re *router.RemoteError
	if !errors.As(err, &re) || re.Code != code {
		t.Fatalf("error = %v, want remote code %q", err, code)
	}
}

func TestHandleEnforcesThePermissionItAdvertises(t *testing.T) {
	e := newEnv(t)
	e.permission(t, "myapp.storage.read")
	e.principal(t, "service.reader", "fp:reader", "myapp.storage.read")
	e.principal(t, "service.nobody", "fp:nobody")

	if err := e.reg.Handle(context.Background(), Endpoint{
		Key: "myapp.storage.read", Description: "Read storage", Permission: "myapp.storage.read", Handler: echo,
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// The definition and the check name the same permission.
	defs, err := facade.ListEndpoints(context.Background(), e.reader)
	if err != nil || len(defs) != 1 || defs[0].EndpointKey != "myapp.storage.read" || defs[0].RequiredPermissionKey != "myapp.storage.read" {
		t.Fatalf("registered endpoints = %+v, err=%v", defs, err)
	}

	if got, err := e.connect(t, "fp:reader").Call(ctxT(t), "myapp.storage.read", json.RawMessage(`1`)); err != nil || string(got) != "1" {
		t.Fatalf("granted caller: %s err=%v", got, err)
	}
	_, err = e.connect(t, "fp:nobody").Call(ctxT(t), "myapp.storage.read", nil)
	wantCode(t, err, wire.ErrUnauthorized)
	_, err = e.connect(t, "fp:never-registered").Call(ctxT(t), "myapp.storage.read", nil)
	wantCode(t, err, wire.ErrUnauthorized)
	_, err = e.connect(t, "").Call(ctxT(t), "myapp.storage.read", nil)
	wantCode(t, err, wire.ErrUnauthorized)
}

func TestRevokedAuthorityIsHonouredOnTheNextCall(t *testing.T) {
	// Authorization is evaluated per message, not cached per session.
	e := newEnv(t)
	e.principal(t, "service.reader", "fp:reader")
	e.permission(t, "myapp.storage.read")
	if err := e.reg.Handle(context.Background(), Endpoint{Key: "myapp.storage.read", Permission: "myapp.storage.read", Handler: echo}); err != nil {
		t.Fatal(err)
	}
	peer := e.connect(t, "fp:reader")
	_, err := peer.Call(ctxT(t), "myapp.storage.read", nil)
	wantCode(t, err, wire.ErrUnauthorized)

	p, _, _ := e.reader.GetPrincipalByKey(context.Background(), "service.reader")
	if _, err := facade.GrantPermission(context.Background(), e.writer, structure.GrantSubjectTypePrincipal, p.PrincipalID, "myapp.storage.read"); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Call(ctxT(t), "myapp.storage.read", nil); err != nil {
		t.Fatalf("after the grant, on the same session: %v", err)
	}
}

func TestAPublicEndpointNeedsNoPermission(t *testing.T) {
	e := newEnv(t)
	if err := e.reg.Handle(context.Background(), Endpoint{Key: "myapp.health.check", Handler: echo}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.connect(t, "fp:never-registered").Call(ctxT(t), "myapp.health.check", nil); err != nil {
		t.Fatalf("public endpoint for an unknown peer: %v", err)
	}
}

func TestAFailedRegistrationLeavesNothingBehind(t *testing.T) {
	ctx := context.Background()
	called := func(t *testing.T, e *env, key string) error {
		_, err := e.connect(t, "").Call(ctxT(t), key, nil)
		return err
	}

	t.Run("permission not registered", func(t *testing.T) {
		e := newEnv(t)
		err := e.reg.Handle(ctx, Endpoint{Key: "myapp.x", Permission: "myapp.never.registered", Handler: echo})
		if !errors.Is(err, facade.ErrPermissionNotRegistered) {
			t.Fatalf("err = %v, want ErrPermissionNotRegistered", err)
		}
		wantCode(t, called(t, e, "myapp.x"), wire.ErrUnknownRoute)
		if defs, _ := facade.ListEndpoints(ctx, e.reader); len(defs) != 0 {
			t.Fatalf("endpoints = %+v, want none", defs)
		}
	})
	t.Run("reserved namespace", func(t *testing.T) {
		e := newEnv(t)
		if err := e.reg.Handle(ctx, Endpoint{Key: "gatehouse.x", Handler: echo}); err == nil {
			t.Fatal("a reserved namespace was accepted")
		}
		wantCode(t, called(t, e, "gatehouse.x"), wire.ErrUnknownRoute)
	})
	t.Run("route invalid", func(t *testing.T) {
		e := newEnv(t)
		if err := e.reg.Handle(ctx, Endpoint{Key: "myapp.nohandler"}); err == nil {
			t.Fatal("a route with no handler was accepted")
		}
		if defs, _ := facade.ListEndpoints(ctx, e.reader); len(defs) != 0 {
			t.Fatalf("a rejected route still advertised itself: %+v", defs)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		e := newEnv(t)
		ep := Endpoint{Key: "myapp.dup", Description: "first", Handler: echo}
		if err := e.reg.Handle(ctx, ep); err != nil {
			t.Fatal(err)
		}
		ep.Description = "second"
		if err := e.reg.Handle(ctx, ep); err == nil {
			t.Fatal("a conflicting second registration was accepted")
		}
		defs, _ := facade.ListEndpoints(ctx, e.reader)
		if len(defs) != 1 || defs[0].Description != "first" {
			t.Fatalf("endpoints = %+v, want the first definition untouched", defs)
		}
	})
}

func TestEndpointsListShowsEachCallerWhatItMayCall(t *testing.T) {
	e := newEnv(t)
	e.permission(t, "myapp.admin")
	e.principal(t, "service.admin", "fp:admin", "myapp.admin")
	e.principal(t, "service.plain", "fp:plain")
	ctx := context.Background()
	if err := e.reg.Handle(ctx, Endpoint{Key: "myapp.admin.do", Description: "admin", Permission: "myapp.admin", Handler: echo}); err != nil {
		t.Fatal(err)
	}
	if err := e.reg.Handle(ctx, Endpoint{Key: "myapp.health", Handler: echo, Metadata: json.RawMessage(`{"hint":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := e.reg.HandleEndpointsList(ctx, true); err != nil {
		t.Fatalf("HandleEndpointsList: %v", err)
	}

	keys := func(infos []EndpointInfo) map[string]EndpointInfo {
		m := map[string]EndpointInfo{}
		for _, i := range infos {
			m[i.Key] = i
		}
		return m
	}
	admin, err := ListEndpoints(ctxT(t), e.connect(t, "fp:admin"))
	if err != nil {
		t.Fatalf("admin ListEndpoints: %v", err)
	}
	if m := keys(admin); len(m) != 3 || m["myapp.admin.do"].RequiredPermission != "myapp.admin" || string(m["myapp.health"].Metadata) != `{"hint":"x"}` {
		t.Fatalf("admin sees %+v", admin)
	}
	for _, fp := range []string{"fp:plain", "fp:never-registered", ""} {
		got, err := ListEndpoints(ctxT(t), e.connect(t, fp))
		if err != nil {
			t.Fatalf("%q ListEndpoints: %v", fp, err)
		}
		m := keys(got)
		if _, leaked := m["myapp.admin.do"]; leaked || len(m) != 2 {
			t.Fatalf("%q sees %+v, want only the public endpoints", fp, got)
		}
	}
}

func TestEndpointsListWithoutFilteringShowsEverything(t *testing.T) {
	e := newEnv(t)
	e.permission(t, "myapp.admin")
	ctx := context.Background()
	if err := e.reg.Handle(ctx, Endpoint{Key: "myapp.admin.do", Permission: "myapp.admin", Handler: echo}); err != nil {
		t.Fatal(err)
	}
	if err := e.reg.HandleEndpointsList(ctx, false); err != nil {
		t.Fatal(err)
	}
	got, err := ListEndpoints(ctxT(t), e.connect(t, "fp:never-registered"))
	if err != nil || len(got) != 2 {
		t.Fatalf("unfiltered list = %+v err=%v", got, err)
	}
	// Seeing it is not being allowed to call it.
	_, err = e.connect(t, "fp:never-registered").Call(ctxT(t), "myapp.admin.do", nil)
	wantCode(t, err, wire.ErrUnauthorized)
}

func TestAChannelEndpointIsAuthorizedLikeARoute(t *testing.T) {
	e := newEnv(t)
	e.permission(t, "myapp.upload")
	e.principal(t, "service.up", "fp:up", "myapp.upload")
	e.principal(t, "service.no", "fp:no")
	served := make(chan string, 2)
	if err := e.reg.HandleChannel(context.Background(), ChannelEndpoint{
		Type: "myapp.upload", Description: "upload", Permission: "myapp.upload",
		Handler: func(_ context.Context, req router.ChannelRequest) error {
			served <- string(req.Channel.Params())
			return nil
		},
	}); err != nil {
		t.Fatalf("HandleChannel: %v", err)
	}
	defs, _ := facade.ListEndpoints(context.Background(), e.reader)
	if len(defs) != 1 || defs[0].RequiredPermissionKey != "myapp.upload" {
		t.Fatalf("endpoints = %+v", defs)
	}

	open := func(fp string) transit.Channel {
		ch, err := e.connect(t, fp).OpenChannel(ctxT(t), transit.ChannelOpts{Initiator: wire.InitiatorClient, Type: "myapp.upload", Params: json.RawMessage(`{"name":"a"}`)})
		if err != nil {
			t.Fatalf("OpenChannel: %v", err)
		}
		return ch
	}
	select {
	case p := <-served:
		t.Fatalf("an unauthorised channel reached the handler: %s", p)
	default:
	}
	denied := open("fp:no")
	if _, err := denied.Recv(ctxT(t)); !errors.Is(err, transit.ErrChannelClosed) {
		t.Fatalf("unauthorised channel Recv = %v, want ErrChannelClosed", err)
	}
	select {
	case p := <-served:
		t.Fatalf("an unauthorised channel reached the handler: %s", p)
	case <-time.After(100 * time.Millisecond):
	}

	_ = open("fp:up")
	select {
	case p := <-served:
		if p != `{"name":"a"}` {
			t.Fatalf("params = %s", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the authorised channel never reached the handler")
	}
}

func TestNewRequiresItsDependencies(t *testing.T) {
	if _, err := New(nil, nil, nil); err == nil {
		t.Fatal("New accepted nil dependencies")
	}
}
