package websocket

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"

	"github.com/croge130/archipelago/transit"
)

// peerIdentityFromTLS extracts the verified mTLS peer identity from a
// completed TLS handshake state. Present=false (the zero value) if no
// client certificate was offered — Transit surfaces this and nothing
// more; see transit.PeerIdentity's own doc comment for why it stops
// there.
func peerIdentityFromTLS(state *tls.ConnectionState) transit.PeerIdentity {
	if state == nil || len(state.PeerCertificates) == 0 {
		return transit.PeerIdentity{}
	}
	cert := state.PeerCertificates[0]
	sum := sha256.Sum256(cert.Raw)
	return transit.PeerIdentity{
		Present:     true,
		Fingerprint: "sha256:" + hex.EncodeToString(sum[:]),
		Subject:     cert.Subject.String(),
		SANs:        cert.DNSNames,
	}
}
