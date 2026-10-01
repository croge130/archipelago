package evaluation

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
	"testing"
	"time"

	"github.com/google/uuid"
)

func selfSignedCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func signedIntermediate(t *testing.T, root *x509.Certificate, rootKey *ecdsa.PrivateKey, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func pemEncodeCert(t *testing.T, der []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestCASignRoundTrip issues a certificate through the real embedded
// authority and verifies the chain against the root — this is the
// exact round trip verified in scratch research before this code was
// written, now exercised as a real test against the module's own CA
// wrapper and its validityWindow CertificateModifier fix.
func TestCASignRoundTrip(t *testing.T) {
	root, rootKey := selfSignedCA(t, "certstore test root")
	intermediate, intermediateKey := signedIntermediate(t, root, rootKey, "certstore test intermediate")

	ca, err := NewCA(pemEncodeCert(t, root.Raw), NewSoftwareSigner(intermediate, intermediateKey))
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}

	subjKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "service:gamebridge"},
		DNSNames: []string{"gamebridge.internal"},
	}, subjKey)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatal(err)
	}

	notBefore := time.Now().Add(-time.Minute)
	notAfter := time.Now().Add(time.Hour)
	chain, err := ca.Sign(context.Background(), csr, notBefore, notAfter)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("expected a 2-cert chain (leaf, intermediate), got %d", len(chain))
	}
	leaf := chain[0]

	pool := x509.NewCertPool()
	pool.AddCert(root)
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         pool,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Fatalf("issued chain does not verify against root: %v", err)
	}

	// X.509 time fields (UTCTime/GeneralizedTime) only carry second
	// precision, so compare truncated to the second rather than exact.
	if !leaf.NotAfter.Truncate(time.Second).Equal(notAfter.Truncate(time.Second)) {
		t.Errorf("leaf NotAfter = %v, want %v (validityWindow modifier didn't apply)", leaf.NotAfter, notAfter)
	}

	issuedAt := time.Now()
	cert := NewCert(leaf, uuid.New(), "mtls_peer", "service:gamebridge", issuedAt)
	if err := cert.Validate(); err != nil {
		t.Fatalf("NewCert produced an invalid Cert: %v", err)
	}
	if cert.Fingerprint == "" {
		t.Error("expected a non-empty Fingerprint")
	}
}
