package peerauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/transit"
	"github.com/google/uuid"
)

// Store is everything this package needs to read — Gatehouse-core's
// own evaluation.Store (so Require can call straight through to it)
// plus the one extra lookup peer resolution itself needs. Declared
// here, in the consuming package, the same rule every other
// evaluation-shaped boundary in this design follows.
type Store interface {
	evaluation.Store
	GetCredentialByMTLSFingerprint(ctx context.Context, fingerprint string) (structure.Credential, structure.MTLSCertCredDetail, bool, error)
}

// ErrNoPeerIdentity is returned when session carries no verified mTLS
// peer identity at all — Session.PeerIdentity().Present is false.
var ErrNoPeerIdentity = errors.New("peerauth: session has no verified peer identity")

// ErrUnknownPeer is returned when a verified identity doesn't resolve
// to any active credential — a cert certstore/mTLS verified, but
// nobody ever bound to a principal via EnsureMTLSCredential, or the
// binding was revoked.
var ErrUnknownPeer = errors.New("peerauth: peer identity does not resolve to a known, active credential")

// ResolvePrincipal maps session's verified mTLS peer identity to the
// Gatehouse-core principal it's bound to.
func ResolvePrincipal(ctx context.Context, store Store, session transit.Session) (uuid.UUID, error) {
	identity := session.PeerIdentity()
	if !identity.Present {
		return uuid.Nil, ErrNoPeerIdentity
	}
	cred, _, found, err := store.GetCredentialByMTLSFingerprint(ctx, identity.Fingerprint)
	if err != nil {
		return uuid.Nil, fmt.Errorf("peerauth: resolve principal: %w", err)
	}
	if !found || cred.Status != structure.CredentialStatusActive {
		return uuid.Nil, ErrUnknownPeer
	}
	return cred.PrincipalID, nil
}

// Require resolves session's peer identity to a principal and checks
// it holds permissionKey (global scope, standard authority — the same
// sane-defaults shape evaluation.RequirePermission itself uses). This
// is the one chokepoint peer authorization needs: Transit's verified
// identity and Gatehouse-core's own evaluator, combined, never
// reimplemented.
func Require(ctx context.Context, store Store, session transit.Session, permissionKey string) error {
	principalID, err := ResolvePrincipal(ctx, store, session)
	if err != nil {
		return err
	}
	return evaluation.RequirePermission(ctx, store, principalID, permissionKey)
}
