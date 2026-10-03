// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. Both certstore's and
// gatehouse-core's schemas are provisioned against the same database
// — their tables live in disjoint namespaces (certstore_* vs.
// gatehouse_*), so one real Postgres instance serves both stores,
// proving the integration against real data in both, not fakes.
package certcred

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	certstoreEval "github.com/croge130/archipelago/certstore/evaluation"
	certstoreFacade "github.com/croge130/archipelago/certstore/facade"
	certstoreDB "github.com/croge130/archipelago/certstore/storage/dbstore"
	certstoreStructure "github.com/croge130/archipelago/certstore/structure"
	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func setupTest(t *testing.T) (*gatehouseDB.PostgresReader, *gatehouseDB.PostgresWriter, *certstoreDB.PostgresReader, *certstoreDB.PostgresWriter, *certstoreEval.CA) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping certcred integration test")
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
	certstoreMigrations, err := certstoreDB.Migrations()
	if err != nil {
		t.Fatalf("certstore Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), certstoreMigrations); err != nil {
		t.Fatalf("certstore ProvisionSchemas: %v", err)
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
		t.Fatalf("truncate gatehouse: %v", err)
	}
	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE certstore_certs, certstore_enrollments`)
	if err != nil {
		t.Fatalf("truncate certstore: %v", err)
	}

	return gatehouseDB.NewPostgresReader(pool.Pgx()), gatehouseDB.NewPostgresWriter(pool.Pgx()),
		certstoreDB.NewPostgresReader(pool.Pgx()), certstoreDB.NewPostgresWriter(pool.Pgx()),
		realCA(t)
}

func realCA(t *testing.T) *certstoreEval.CA {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "certcred test root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}

	intKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "certcred test intermediate"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true,
	}
	intDER, err := x509.CreateCertificate(rand.Reader, intTmpl, root, &intKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := x509.ParseCertificate(intDER)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}); err != nil {
		t.Fatal(err)
	}
	ca, err := certstoreEval.NewCA(buf.Bytes(), certstoreEval.NewSoftwareSigner(intermediate, intKey))
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca
}

func issueCert(t *testing.T, ctx context.Context, writer certstoreFacade.Writer, ca *certstoreEval.CA, cn string) *certstoreStructure.Cert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, key)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}); err != nil {
		t.Fatal(err)
	}

	policy := certstoreEval.Policy{AutoApprove: map[certstoreStructure.EnrollmentPurpose]bool{
		certstoreStructure.EnrollmentPurposeMTLSPeer: true,
	}}
	_, cert, _, err := certstoreFacade.SubmitEnrollment(ctx, writer, ca, policy, certstoreStructure.EnrollmentPurposeMTLSPeer, cn, buf.Bytes(), "", nil)
	if err != nil {
		t.Fatalf("SubmitEnrollment: %v", err)
	}
	return cert
}

func TestResolvePrincipalBothSidesUsable(t *testing.T) {
	ghReader, ghWriter, csReader, csWriter, ca := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "service.gamebridge", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	cert := issueCert(t, ctx, csWriter, ca, "service:gamebridge")
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, ghReader, ghWriter, p.PrincipalID, cert.Fingerprint); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}

	got, err := ResolvePrincipal(ctx, ghReader, csReader, cert.Fingerprint)
	if err != nil {
		t.Fatalf("ResolvePrincipal: %v", err)
	}
	if got != p.PrincipalID {
		t.Fatalf("ResolvePrincipal = %s, want %s", got, p.PrincipalID)
	}
}

func TestResolvePrincipalNoGatehouseCredential(t *testing.T) {
	ghReader, _, csReader, csWriter, ca := setupTest(t) // no credential created on the gatehouse side
	ctx, cancel := withTimeout(t)
	defer cancel()

	cert := issueCert(t, ctx, csWriter, ca, "service:gamebridge")
	if _, err := ResolvePrincipal(ctx, ghReader, csReader, cert.Fingerprint); err != ErrUnknownCredential {
		t.Fatalf("expected ErrUnknownCredential, got: %v", err)
	}
}

func TestResolvePrincipalNoCertRecordDataDrift(t *testing.T) {
	ghReader, ghWriter, csReader, _, _ := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "service.gamebridge", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	// A binding with no matching certstore Cert at all — the data-drift case.
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, ghReader, ghWriter, p.PrincipalID, "sha256:never-issued"); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}

	if _, err := ResolvePrincipal(ctx, ghReader, csReader, "sha256:never-issued"); err != ErrCertNotFound {
		t.Fatalf("expected ErrCertNotFound, got: %v", err)
	}
}

func TestResolvePrincipalRevokedCertIsNotUsable(t *testing.T) {
	ghReader, ghWriter, csReader, csWriter, ca := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "service.gamebridge", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	cert := issueCert(t, ctx, csWriter, ca, "service:gamebridge")
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, ghReader, ghWriter, p.PrincipalID, cert.Fingerprint); err != nil {
		t.Fatalf("EnsureMTLSCredential: %v", err)
	}

	if _, err := certstoreFacade.RevokeCert(ctx, csReader, csWriter, cert.SerialNumber, "key compromise suspected"); err != nil {
		t.Fatalf("RevokeCert: %v", err)
	}

	if _, err := ResolvePrincipal(ctx, ghReader, csReader, cert.Fingerprint); err != ErrCertNotUsable {
		t.Fatalf("expected ErrCertNotUsable after revocation, got: %v", err)
	}
}
