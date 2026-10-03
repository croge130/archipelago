package certcred

import (
	"context"
	"errors"
	"fmt"
	"time"

	certstoreStructure "github.com/croge130/archipelago/certstore/structure"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// GatehouseStore is the one gatehouse-core lookup this package needs
// — the credential binding, never the full evaluator (that's
// peerauth's job, composed separately by a caller that wants both).
type GatehouseStore interface {
	GetCredentialByMTLSFingerprint(ctx context.Context, fingerprint string) (gatehouseStructure.Credential, gatehouseStructure.MTLSCertCredDetail, bool, error)
}

// CertStore is the one certstore lookup this package needs.
type CertStore interface {
	GetCertByFingerprint(ctx context.Context, fingerprint string) (certstoreStructure.Cert, bool, error)
}

// ErrUnknownCredential means fingerprint has no active binding in
// Gatehouse-core at all — the same case peerauth.ErrUnknownPeer
// covers on its own.
var ErrUnknownCredential = errors.New("certcred: fingerprint has no active credential binding")

// ErrCertNotFound means Gatehouse-core has an active binding for
// fingerprint, but Cert-store has no matching certificate record —
// data drift between the two stores, never expected in normal
// operation (a binding is only ever created from a certificate
// Cert-store itself issued).
var ErrCertNotFound = errors.New("certcred: no matching certificate record in cert-store")

// ErrCertNotUsable means Cert-store's own record exists but reports
// the certificate revoked or expired — the DB check 05-pki-and-signing.md
// calls the primary revocation signal, independent of whatever the
// TLS handshake itself already validated.
var ErrCertNotUsable = errors.New("certcred: certificate is revoked or expired")

// ResolvePrincipal resolves a verified mTLS fingerprint to a
// Gatehouse-core principal, requiring both an active credential
// binding and a currently-usable certificate record — the stronger
// check named in this package's own doc comment.
func ResolvePrincipal(ctx context.Context, gatehouseStore GatehouseStore, certStore CertStore, fingerprint string) (uuid.UUID, error) {
	cred, _, found, err := gatehouseStore.GetCredentialByMTLSFingerprint(ctx, fingerprint)
	if err != nil {
		return uuid.Nil, fmt.Errorf("certcred: resolve principal: %w", err)
	}
	if !found || cred.Status != gatehouseStructure.CredentialStatusActive {
		return uuid.Nil, ErrUnknownCredential
	}

	cert, found, err := certStore.GetCertByFingerprint(ctx, fingerprint)
	if err != nil {
		return uuid.Nil, fmt.Errorf("certcred: resolve principal: %w", err)
	}
	if !found {
		return uuid.Nil, ErrCertNotFound
	}
	if !cert.IsUsable(time.Now()) {
		return uuid.Nil, ErrCertNotUsable
	}

	return cred.PrincipalID, nil
}
