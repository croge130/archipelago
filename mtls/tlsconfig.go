package mtls

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
)

// ChainToTLSCertificate pairs a certstore-signed chain (leaf first, as
// returned by certstore/facade's SubmitEnrollment/ConfirmEnrollment)
// with the private key the caller already holds — certstore signs a
// submitted CSR, it never sees or generates the key — producing a
// tls.Certificate ready to serve or dial with.
func ChainToTLSCertificate(chain []*x509.Certificate, key crypto.Signer) (tls.Certificate, error) {
	if len(chain) == 0 {
		return tls.Certificate{}, fmt.Errorf("mtls: chain is empty")
	}
	der := make([][]byte, len(chain))
	for i, cert := range chain {
		der[i] = cert.Raw
	}
	return tls.Certificate{Certificate: der, PrivateKey: key, Leaf: chain[0]}, nil
}

// ServerTLSConfig builds a tls.Config for an http.Server fronting a
// transit/websocket.Backend: presents serverCert as this server's own
// identity, and trusts rootPEM (the certstore CA's own root bundle)
// to verify client certificates. requireClientCert chooses between
// RequireAndVerifyClientCert (mTLS is mandatory) and
// VerifyClientCertIfGiven (mTLS is optional, so password/token auth
// can still carry browser or other non-cert-capable clients — see
// 11-transit-model.md's own note that mTLS is a native-client/
// service-peer feature, not something every peer can present).
func ServerTLSConfig(rootPEM []byte, serverCert tls.Certificate, requireClientCert bool) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rootPEM) {
		return nil, fmt.Errorf("mtls: no valid certificates found in rootPEM")
	}
	clientAuth := tls.VerifyClientCertIfGiven
	if requireClientCert {
		clientAuth = tls.RequireAndVerifyClientCert
	}
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   clientAuth,
	}, nil
}

// ClientTLSConfig builds a tls.Config for dialing a server trusted via
// rootPEM. clientCert is nil for a connection that presents no client
// certificate at all (fine against a server using
// VerifyClientCertIfGiven; rejected by one requiring a cert).
func ClientTLSConfig(rootPEM []byte, clientCert *tls.Certificate) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rootPEM) {
		return nil, fmt.Errorf("mtls: no valid certificates found in rootPEM")
	}
	cfg := &tls.Config{RootCAs: pool}
	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}
	return cfg, nil
}

// HTTPClient wraps tlsConfig in the *http.Client shape
// transit/websocket.Dial's own client parameter expects.
func HTTPClient(tlsConfig *tls.Config) *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}
