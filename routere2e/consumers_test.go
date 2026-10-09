// The first end-to-end test of the existing consumers (traceagg, registry,
// sso) over a real backend: a real mTLS websocket, the real router, and a
// real Postgres-backed Gatehouse-core, with every route registered through
// routerauth. Skips without ARCHIPELAGO_TEST_DATABASE_URL.
package routere2e

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	certstoreEval "github.com/croge130/archipelago/certstore/evaluation"
	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/mtls"
	"github.com/croge130/archipelago/registry"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/routerauth"
	"github.com/croge130/archipelago/sso"
	"github.com/croge130/archipelago/traceagg"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/websocket"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

func setupGatehouse(t *testing.T) (*dbstore.PostgresReader, *dbstore.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping routere2e consumer tests")
	}
	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	migrations, err := dbstore.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatal(err)
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

// testCA builds a root and intermediate, as mtls's own tests do.
func testCA(t *testing.T) (*certstoreEval.CA, []byte) {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	rootKey, intKey := newKey(), newKey()
	tmpl := func(serial int64, cn string) *x509.Certificate {
		return &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
			IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true,
		}
	}
	rootT := tmpl(1, "e2e root")
	rootDER, err := x509.CreateCertificate(rand.Reader, rootT, rootT, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := x509.ParseCertificate(rootDER)
	intDER, err := x509.CreateCertificate(rand.Reader, tmpl(2, "e2e intermediate"), root, &intKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	inter, _ := x509.ParseCertificate(intDER)
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}); err != nil {
		t.Fatal(err)
	}
	ca, err := certstoreEval.NewCA(buf.Bytes(), certstoreEval.NewSoftwareSigner(inter, intKey))
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca, buf.Bytes()
}

func issueCert(t *testing.T, ca *certstoreEval.CA, cn string, ips ...net.IP) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}, IPAddresses: ips}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, _ := x509.ParseCertificateRequest(der)
	chain, err := ca.Sign(context.Background(), csr, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	cert, err := mtls.ChainToTLSCertificate(chain, key)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func fingerprint(c tls.Certificate) string {
	sum := sha256.Sum256(c.Certificate[0])
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ticketSigner is a self-signed key standing in for the dedicated ticket-signing key.
func ticketSigner(t *testing.T) certstoreEval.Signer {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "e2e ticket signer"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return certstoreEval.NewSoftwareSigner(cert, key)
}

// node is a coordinator: one domain's stores, one router, every core
// route registered through routerauth, listening on real mTLS.
type node struct {
	reader    *dbstore.PostgresReader
	writer    *dbstore.PostgresWriter
	collector *traceagg.Collector
	signer    certstoreEval.Signer
	ca        *certstoreEval.CA
	url       string
	rootPEM   []byte
	backend   *websocket.Backend
	router    *router.Router
}

const (
	permRegister = "e2e.registry.register"
	permPush     = "e2e.traceagg.push"
	permTicket   = "e2e.sso.ticket"
)

func startNode(t *testing.T) *node {
	t.Helper()
	reader, writer := setupGatehouse(t)
	ctx := context.Background()
	ca, rootPEM := testCA(t)
	serverCert := issueCert(t, ca, "e2e-coordinator", net.ParseIP("127.0.0.1"))

	for _, perm := range []string{permRegister, permPush, permTicket} {
		if err := facade.RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
			PermissionKey: perm, RequiredAuthorityLevel: structure.AuthorityLevelStandard,
		}, facade.RegisterPermissionOptions{}); err != nil {
			t.Fatalf("RegisterPermission: %v", err)
		}
	}

	n := &node{reader: reader, writer: writer, collector: traceagg.NewCollector(), signer: ticketSigner(t), ca: ca, rootPEM: rootPEM, router: router.New(quiet())}
	reg, err := routerauth.New(n.router, reader, writer)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := sso.IssueHandler(sso.IssueConfig{Store: reader, Signer: n.signer})
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range []routerauth.Endpoint{
		{Key: registry.RouteRegister, Description: "Register as an instance", Permission: permRegister, Handler: registry.RegisterHandler(reader, writer)},
		{Key: traceagg.MessageTypeEntries, Description: "Report log entries", Permission: permPush, Handler: n.collector.Handler()},
		{Key: sso.RouteIssue, Description: "Issue an SSO ticket", Permission: permTicket, Handler: issue},
	} {
		if err := reg.Handle(ctx, ep); err != nil {
			t.Fatalf("Handle(%s): %v", ep.Key, err)
		}
	}
	if err := reg.HandleEndpointsList(ctx, true); err != nil {
		t.Fatal(err)
	}

	serverTLS, err := mtls.ServerTLSConfig(rootPEM, serverCert, true)
	if err != nil {
		t.Fatal(err)
	}
	n.backend = websocket.NewBackend()
	httpServer := httptest.NewUnstartedServer(n.backend)
	httpServer.TLS = serverTLS
	httpServer.StartTLS()
	t.Cleanup(func() { httpServer.Close(); n.backend.Close() })
	n.url = "wss" + strings.TrimPrefix(httpServer.URL, "https")
	return n
}

// client binds a new service principal to a fresh certificate, grants it
// perms, and connects it over mTLS.
func (n *node) client(t *testing.T, name string, perms ...string) *router.Peer {
	t.Helper()
	ctx := context.Background()
	cert := issueCert(t, n.ca, name)
	p, err := facade.EnsurePrincipal(ctx, n.reader, n.writer, name, structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := facade.EnsureMTLSCredential(ctx, n.reader, n.writer, p.PrincipalID, fingerprint(cert)); err != nil {
		t.Fatal(err)
	}
	for _, perm := range perms {
		if _, err := facade.GrantPermission(ctx, n.writer, structure.GrantSubjectTypePrincipal, p.PrincipalID, perm); err != nil {
			t.Fatal(err)
		}
	}
	clientTLS, err := mtls.ClientTLSConfig(n.rootPEM, &cert)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := websocket.Dial(ctxT(t), n.url, mtls.HTTPClient(clientTLS))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	ss, err := n.backend.Accept(ctxT(t))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	a := n.router.Accept(ss.(transit.Conn))
	d, err := router.New(quiet()).Connect(ctxT(t), cs.(transit.Conn), name)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { d.Close(); a.Close() })
	return d
}

func TestAuthorisedPeerUsesEveryConsumerOverRealMTLS(t *testing.T) {
	n := startNode(t)
	worker := n.client(t, "service.worker", permRegister, permPush, permTicket)
	ctx := ctxT(t)

	// What the peer is told it may do: all three routes plus the list itself.
	infos, err := routerauth.ListEndpoints(ctx, worker)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	keys := map[string]string{}
	for _, i := range infos {
		keys[i.Key] = i.RequiredPermission
	}
	want := map[string]string{
		registry.RouteRegister: permRegister, traceagg.MessageTypeEntries: permPush,
		sso.RouteIssue: permTicket, routerauth.EndpointsListRoute: "",
	}
	if len(keys) != len(want) {
		t.Fatalf("endpoints = %v, want %v", keys, want)
	}
	for k, v := range want {
		if got, ok := keys[k]; !ok || got != v {
			t.Fatalf("endpoint %q: permission %q (present %v), want %q", k, got, ok, v)
		}
	}

	// registry: the instance belongs to the principal the certificate resolves to.
	inst, err := registry.Register(ctx, worker, "e2e.workers", nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if p, _, _ := n.reader.GetPrincipalByKey(ctx, "service.worker"); inst.PrincipalID != p.PrincipalID {
		t.Fatalf("instance principal = %s, want %s", inst.PrincipalID, p.PrincipalID)
	}

	// traceagg: a pushed event arrives in the collector.
	src := uuid.New()
	if err := traceagg.PushEntries(ctx, worker, src, []logging.Entry{{Time: time.Now(), Message: "hello", TraceID: "t-1"}}); err != nil {
		t.Fatalf("PushEntries: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(n.collector.Trace("t-1")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the pushed entry never reached the collector")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := n.collector.Trace("t-1"); got[0].SourceInstanceID != src {
		t.Fatalf("collected %+v", got)
	}

	// sso: a ticket the relying party can verify with the issuer's certificate.
	ticket, err := sso.RequestTicket(ctx, worker, "gamebridge", 0)
	if err != nil {
		t.Fatalf("RequestTicket: %v", err)
	}
	who, err := sso.Verify(ctx, n.reader, n.signer.Certificate(), ticket, "gamebridge", time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if who.PrincipalID != inst.PrincipalID {
		t.Fatalf("ticket subject = %s, want %s", who.PrincipalID, inst.PrincipalID)
	}
}

func TestAPeerWithoutGrantsIsRefusedEverywhereAndToldOnlyWhatIsPublic(t *testing.T) {
	n := startNode(t)
	limited := n.client(t, "service.limited") // authenticated, holds nothing
	ctx := ctxT(t)

	infos, err := routerauth.ListEndpoints(ctx, limited)
	if err != nil || len(infos) != 1 || infos[0].Key != routerauth.EndpointsListRoute {
		t.Fatalf("ListEndpoints = %+v, %v; want only %q", infos, err, routerauth.EndpointsListRoute)
	}

	unauthorized := func(err error) {
		t.Helper()
		var re *router.RemoteError
		if !errors.As(err, &re) || re.Code != wire.ErrUnauthorized {
			t.Fatalf("error = %v, want remote code unauthorized", err)
		}
	}
	_, err = registry.Register(ctx, limited, "e2e.workers", nil)
	unauthorized(err)
	_, err = sso.RequestTicket(ctx, limited, "gamebridge", 0)
	unauthorized(err)

	// An event has no reply to refuse on, so the refusal shows as nothing arriving.
	if err := traceagg.PushEntries(ctx, limited, uuid.New(), []logging.Entry{{Time: time.Now(), Message: "nope", TraceID: "t-2"}}); err != nil {
		t.Fatalf("PushEntries: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := n.collector.Trace("t-2"); len(got) != 0 {
		t.Fatalf("an unauthorised peer's entries were collected: %+v", got)
	}
}
