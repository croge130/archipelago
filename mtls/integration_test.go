package mtls

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
	"math/big"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	certstoreEval "github.com/croge130/archipelago/certstore/evaluation"
	"github.com/croge130/archipelago/transit/websocket"
	"github.com/croge130/archipelago/wire"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// realCA builds a genuine root + intermediate CA (the same shape
// certstore's own evaluation tests use) and wraps it as an
// evaluation.CA, returning the root's PEM bundle alongside it so
// tests can build tls.Config pools from it.
func realCA(t *testing.T) (*certstoreEval.CA, []byte) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mtls test root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
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

	intKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "mtls test intermediate"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	intDER, err := x509.CreateCertificate(rand.Reader, intTmpl, root, &intKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := x509.ParseCertificate(intDER)
	if err != nil {
		t.Fatal(err)
	}

	rootPEM := pemEncodeCert(t, root.Raw)
	ca, err := certstoreEval.NewCA(rootPEM, certstoreEval.NewSoftwareSigner(intermediate, intKey))
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca, rootPEM
}

func pemEncodeCert(t *testing.T, der []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// issue generates a fresh key + CSR for cn, signs it through ca, and
// returns the ready-to-use tls.Certificate — standing in for an
// enrollee that generated its own key, submitted the CSR to
// certstore's enrollment ceremony, and got the chain back exactly
// once at mint (certstore/facade.SubmitEnrollment's own shape,
// exercised directly against evaluation.CA here since this test is
// about the TLS wiring, not re-testing the ceremony itself). ips are
// IP SANs to carry through to the issued cert — needed on a server
// cert since the client side of a TLS handshake verifies the
// server's hostname/IP against its certificate's SANs; a client cert
// needs none, since the server side performs no such check.
func issue(t *testing.T, ca *certstoreEval.CA, cn string, ips ...net.IP) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:     pkix.Name{CommonName: cn},
		IPAddresses: ips,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := withTimeout(t)
	defer cancel()
	chain, err := ca.Sign(ctx, csr, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("ca.Sign: %v", err)
	}

	cert, err := ChainToTLSCertificate(chain, key)
	if err != nil {
		t.Fatalf("ChainToTLSCertificate: %v", err)
	}
	return cert
}

func leafFingerprint(cert tls.Certificate) string {
	sum := sha256.Sum256(cert.Certificate[0])
	return "sha256:" + hex.EncodeToString(sum[:])
}

func startServer(t *testing.T, tlsConfig *tls.Config) (*websocket.Backend, *httptest.Server) {
	t.Helper()
	backend := websocket.NewBackend()
	server := httptest.NewUnstartedServer(backend)
	server.TLS = tlsConfig
	server.StartTLS()
	return backend, server
}

func TestMTLSBothSidesPresentRealCertstoreCerts(t *testing.T) {
	ca, rootPEM := realCA(t)
	serverCert := issue(t, ca, "service:gamebridge-server", net.ParseIP("127.0.0.1"))
	clientCert := issue(t, ca, "service:gamebridge-client")

	serverTLS, err := ServerTLSConfig(rootPEM, serverCert, true)
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	clientTLS, err := ClientTLSConfig(rootPEM, &clientCert)
	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}

	backend, httpServer := startServer(t, serverTLS)
	defer httpServer.Close()
	defer backend.Close()

	wsURL := "wss" + strings.TrimPrefix(httpServer.URL, "https")
	ctx, cancel := withTimeout(t)
	defer cancel()

	clientSession, err := websocket.Dial(ctx, wsURL, HTTPClient(clientTLS))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	serverSession, err := backend.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	identity := serverSession.PeerIdentity()
	if !identity.Present {
		t.Fatal("expected the server to see a present client PeerIdentity")
	}
	if want := leafFingerprint(clientCert); identity.Fingerprint != want {
		t.Fatalf("server's view of client Fingerprint = %q, want %q", identity.Fingerprint, want)
	}
	if identity.Subject != "CN=service:gamebridge-client" {
		t.Fatalf("identity.Subject = %q, want CN=service:gamebridge-client", identity.Subject)
	}

	clientIdentity := clientSession.PeerIdentity()
	if !clientIdentity.Present {
		t.Fatal("expected the client to see a present server PeerIdentity")
	}
	if want := leafFingerprint(serverCert); clientIdentity.Fingerprint != want {
		t.Fatalf("client's view of server Fingerprint = %q, want %q", clientIdentity.Fingerprint, want)
	}

	// Prove the connection isn't just handshake theater — push a real
	// message end to end over it.
	if err := serverSession.Push(ctx, wire.Message{Type: "demo.event"}); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func TestMTLSOptionalClientCertAllowsNoCert(t *testing.T) {
	ca, rootPEM := realCA(t)
	serverCert := issue(t, ca, "service:gamebridge-server", net.ParseIP("127.0.0.1"))

	serverTLS, err := ServerTLSConfig(rootPEM, serverCert, false) // optional
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	clientTLS, err := ClientTLSConfig(rootPEM, nil) // no client cert offered
	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}

	backend, httpServer := startServer(t, serverTLS)
	defer httpServer.Close()
	defer backend.Close()

	wsURL := "wss" + strings.TrimPrefix(httpServer.URL, "https")
	ctx, cancel := withTimeout(t)
	defer cancel()

	if _, err := websocket.Dial(ctx, wsURL, HTTPClient(clientTLS)); err != nil {
		t.Fatalf("expected a certless Dial against an optional-mTLS server to succeed, got: %v", err)
	}
	serverSession, err := backend.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if serverSession.PeerIdentity().Present {
		t.Fatal("expected PeerIdentity().Present to be false when no client certificate was offered")
	}
}

func TestMTLSRequiredClientCertRejectsNoCert(t *testing.T) {
	ca, rootPEM := realCA(t)
	serverCert := issue(t, ca, "service:gamebridge-server", net.ParseIP("127.0.0.1"))

	serverTLS, err := ServerTLSConfig(rootPEM, serverCert, true) // required
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	clientTLS, err := ClientTLSConfig(rootPEM, nil) // no client cert offered

	backend, httpServer := startServer(t, serverTLS)
	defer httpServer.Close()
	defer backend.Close()

	wsURL := "wss" + strings.TrimPrefix(httpServer.URL, "https")
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}
	if _, err := websocket.Dial(ctx, wsURL, HTTPClient(clientTLS)); err == nil {
		t.Fatal("expected a certless Dial against a required-mTLS server to fail")
	}
}

func TestMTLSRejectsUntrustedClientCert(t *testing.T) {
	ca, rootPEM := realCA(t)
	serverCert := issue(t, ca, "service:gamebridge-server", net.ParseIP("127.0.0.1"))

	// A self-signed cert from an entirely different, untrusted CA —
	// never submitted to this certstore's own enrollment ceremony.
	otherCA, _ := realCA(t)
	untrustedCert := issue(t, otherCA, "service:imposter")

	serverTLS, err := ServerTLSConfig(rootPEM, serverCert, true)
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	// Build a client config that offers the untrusted cert but still
	// trusts the real root for verifying the server.
	clientTLS, err := ClientTLSConfig(rootPEM, &untrustedCert)
	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}

	backend, httpServer := startServer(t, serverTLS)
	defer httpServer.Close()
	defer backend.Close()

	wsURL := "wss" + strings.TrimPrefix(httpServer.URL, "https")
	ctx, cancel := withTimeout(t)
	defer cancel()

	if _, err := websocket.Dial(ctx, wsURL, HTTPClient(clientTLS)); err == nil {
		t.Fatal("expected the server to reject a client certificate signed by an untrusted CA")
	}
}
