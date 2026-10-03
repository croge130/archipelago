package sso

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/sso/structure"
)

// clockSkewAllowance tolerates IssuedAt being a little ahead of the
// verifier's own clock — issuer and verifier are different processes,
// never expected to share a clock exactly.
const clockSkewAllowance = 5 * time.Second

var (
	// ErrInvalidSignature means the ticket's signature doesn't verify
	// against signerCert's public key — either it was never signed by
	// the trusted key, or a field was tampered with after signing.
	ErrInvalidSignature = errors.New("sso: ticket signature is invalid")

	// ErrExpired covers both directions: a ticket presented after its
	// own ExpiresAt, and one presented implausibly before its own
	// IssuedAt (beyond the allowed clock skew) — the latter is as much
	// "don't trust this" as the former.
	ErrExpired = errors.New("sso: ticket is expired or not yet valid")

	// ErrAudienceMismatch means the ticket is genuine but was minted
	// for a different destination — exact match only, no wildcard or
	// prefix semantics, per 12-sso-tickets-model.md.
	ErrAudienceMismatch = errors.New("sso: ticket audience does not match")

	// ErrNotECDSAKey means signerCert doesn't hold the ECDSA P-256
	// public key this package's signature scheme requires — see
	// 12-sso-tickets-model.md's Signing section for why this package
	// commits to one algorithm rather than negotiating one generically.
	ErrNotECDSAKey = errors.New("sso: signer certificate does not hold an ECDSA public key")
)

// Verify checks ticket's signature against signerCert, its validity
// window against now, and its Audience against expectedAudience, in
// that order, then resolves its SubjectPrincipalID through principals.
// It stops at identity: callers run their own Gatehouse-core permission
// check against the returned Principal, the same as any other resolved
// identity in this design.
func Verify(ctx context.Context, principals PrincipalStore, signerCert *x509.Certificate, ticket structure.Ticket, expectedAudience string, now time.Time) (gatehouseStructure.Principal, error) {
	if err := ticket.Validate(); err != nil {
		return gatehouseStructure.Principal{}, fmt.Errorf("sso: verify: %w", err)
	}

	pub, ok := signerCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return gatehouseStructure.Principal{}, ErrNotECDSAKey
	}
	digest := sha256.Sum256(ticket.SigningBytes())
	if !ecdsa.VerifyASN1(pub, digest[:], ticket.Signature) {
		return gatehouseStructure.Principal{}, ErrInvalidSignature
	}

	if now.Before(ticket.IssuedAt.Add(-clockSkewAllowance)) || now.After(ticket.ExpiresAt) {
		return gatehouseStructure.Principal{}, ErrExpired
	}

	if ticket.Audience != expectedAudience {
		return gatehouseStructure.Principal{}, ErrAudienceMismatch
	}

	principal, found, err := principals.GetPrincipal(ctx, ticket.SubjectPrincipalID)
	if err != nil {
		return gatehouseStructure.Principal{}, fmt.Errorf("sso: verify: %w", err)
	}
	if !found {
		return gatehouseStructure.Principal{}, ErrUnknownPrincipal
	}
	return principal, nil
}
