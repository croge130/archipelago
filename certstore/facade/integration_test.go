// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. They exercise the facade
// through dbstore's real PostgresReader/PostgresWriter and a real
// embedded CA, proving facade.Reader/facade.Writer are actually
// satisfied by the concrete implementation and that a confirmed
// enrollment really does turn into a verifiable certificate.
package facade

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/croge130/archipelago/certstore/evaluation"
	"github.com/croge130/archipelago/certstore/storage/dbstore"
	"github.com/croge130/archipelago/certstore/structure"
	archidb "github.com/croge130/archipelago/db"
)

func pemEncodeCert(t *testing.T, der []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pemEncodeCSR(t *testing.T, der []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func setupFacadeTest(t *testing.T) (Reader, Writer, *evaluation.CA) {
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
	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE certstore_certs, certstore_enrollments`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx()), testCA(t)
}

func testCA(t *testing.T) *evaluation.CA {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "certstore facade test root"},
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}

	intermediateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intermediateTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "certstore facade test intermediate"},
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	intermediateDER, err := x509.CreateCertificate(rand.Reader, intermediateTmpl, root, &intermediateKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := x509.ParseCertificate(intermediateDER)
	if err != nil {
		t.Fatal(err)
	}

	ca, err := evaluation.NewCA(pemEncodeCert(t, root.Raw), evaluation.NewSoftwareSigner(intermediate, intermediateKey))
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca
}

func testCSR(t *testing.T, commonName string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pemEncodeCSR(t, der)
}

func TestSubmitEnrollmentAutoApproves(t *testing.T) {
	reader, writer, ca := setupFacadeTest(t)
	ctx := context.Background()
	policy := evaluation.Policy{AutoApprove: map[structure.EnrollmentPurpose]bool{
		structure.EnrollmentPurposeMTLSPeer: true,
	}}

	e, cert, err := SubmitEnrollment(ctx, writer, ca, policy, structure.EnrollmentPurposeMTLSPeer, "service:gamebridge", testCSR(t, "service:gamebridge"), "", nil)
	if err != nil {
		t.Fatalf("SubmitEnrollment: %v", err)
	}
	if e.Status != structure.EnrollmentStatusConfirmed {
		t.Fatalf("expected an auto-approved enrollment to be confirmed, got %v", e.Status)
	}
	if cert == nil {
		t.Fatal("expected a signed cert for an auto-approved enrollment")
	}

	got, found, err := reader.GetCert(ctx, cert.SerialNumber)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if !found || got.Status != structure.CertStatusActive {
		t.Fatalf("GetCert = %+v, found=%v", got, found)
	}
}

func TestSubmitEnrollmentRequiresApproval(t *testing.T) {
	_, writer, ca := setupFacadeTest(t)
	ctx := context.Background()
	policy := evaluation.Policy{} // nothing auto-approved

	e, cert, err := SubmitEnrollment(ctx, writer, ca, policy, structure.EnrollmentPurposeAdminKey, "operator:christian", testCSR(t, "operator:christian"), "", nil)
	if err != nil {
		t.Fatalf("SubmitEnrollment: %v", err)
	}
	if e.Status != structure.EnrollmentStatusPending {
		t.Fatalf("expected a not-auto-approved enrollment to stay pending, got %v", e.Status)
	}
	if cert != nil {
		t.Fatal("expected no cert signed before an operator confirms")
	}
}

func TestConfirmEnrollmentSignsCert(t *testing.T) {
	reader, writer, ca := setupFacadeTest(t)
	ctx := context.Background()
	policy := evaluation.Policy{}

	pending, _, err := SubmitEnrollment(ctx, writer, ca, policy, structure.EnrollmentPurposeAdminKey, "operator:christian", testCSR(t, "operator:christian"), "", nil)
	if err != nil {
		t.Fatalf("SubmitEnrollment: %v", err)
	}

	confirmed, cert, err := ConfirmEnrollment(ctx, reader, writer, ca, policy, pending.EnrollmentID, "operator:christian")
	if err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}
	if confirmed.Status != structure.EnrollmentStatusConfirmed || confirmed.ConfirmedBy != "operator:christian" {
		t.Fatalf("unexpected confirmed enrollment: %+v", confirmed)
	}
	if cert.Status != structure.CertStatusActive {
		t.Fatalf("unexpected cert: %+v", cert)
	}
}

func TestRejectEnrollment(t *testing.T) {
	reader, writer, ca := setupFacadeTest(t)
	ctx := context.Background()
	policy := evaluation.Policy{}

	pending, _, err := SubmitEnrollment(ctx, writer, ca, policy, structure.EnrollmentPurposeAdminKey, "operator:christian", testCSR(t, "operator:christian"), "", nil)
	if err != nil {
		t.Fatalf("SubmitEnrollment: %v", err)
	}

	rejected, err := RejectEnrollment(ctx, reader, writer, pending.EnrollmentID)
	if err != nil {
		t.Fatalf("RejectEnrollment: %v", err)
	}
	if rejected.Status != structure.EnrollmentStatusRejected {
		t.Fatalf("expected rejected status, got %v", rejected.Status)
	}

	if _, _, err := ConfirmEnrollment(ctx, reader, writer, ca, policy, pending.EnrollmentID, "operator:christian"); err == nil {
		t.Fatal("expected confirming an already-rejected enrollment to fail")
	}
}

func TestRevokeCert(t *testing.T) {
	reader, writer, ca := setupFacadeTest(t)
	ctx := context.Background()
	policy := evaluation.Policy{AutoApprove: map[structure.EnrollmentPurpose]bool{
		structure.EnrollmentPurposeMTLSPeer: true,
	}}

	_, cert, err := SubmitEnrollment(ctx, writer, ca, policy, structure.EnrollmentPurposeMTLSPeer, "service:gamebridge", testCSR(t, "service:gamebridge"), "", nil)
	if err != nil {
		t.Fatalf("SubmitEnrollment: %v", err)
	}

	revoked, err := RevokeCert(ctx, reader, writer, cert.SerialNumber, "key compromise suspected")
	if err != nil {
		t.Fatalf("RevokeCert: %v", err)
	}
	if revoked.Status != structure.CertStatusRevoked || revoked.RevocationReason == "" {
		t.Fatalf("unexpected revoked cert: %+v", revoked)
	}
}

func TestRevokeCertNotFound(t *testing.T) {
	reader, writer, _ := setupFacadeTest(t)
	_, err := RevokeCert(context.Background(), reader, writer, "does-not-exist", "test")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
