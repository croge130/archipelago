package sso

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	certstoreEvaluation "github.com/croge130/archipelago/certstore/evaluation"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/sso/structure"
	"github.com/google/uuid"
)

// PrincipalStore is the one Gatehouse-core lookup this package needs on
// either side: confirming a principal that's about to be vouched for
// (Issue) or resolved (Verify) actually exists.
type PrincipalStore interface {
	GetPrincipal(ctx context.Context, principalID uuid.UUID) (gatehouseStructure.Principal, bool, error)
}

// ErrUnknownPrincipal is returned by Issue when subjectPrincipalID
// doesn't name a real principal, and by Verify when a ticket's own
// SubjectPrincipalID no longer does — signing or honoring a ticket for
// an identity that doesn't exist would make its "identity-only" promise
// meaningless the moment anyone checked it.
var ErrUnknownPrincipal = errors.New("sso: subject principal does not exist")

// Issue mints a Ticket for subjectPrincipalID, valid for ttl, signed
// with signer — a certstore Signer holding the dedicated ticket-signing
// key, never the mTLS CA's own signer (see 12-sso-tickets-model.md).
// ttl is the caller's own call about how short "short-lived" needs to
// be for a given handoff; this function enforces nothing about it
// beyond requiring it to be positive.
func Issue(ctx context.Context, principals PrincipalStore, signer certstoreEvaluation.Signer, subjectPrincipalID uuid.UUID, audience string, ttl time.Duration) (structure.Ticket, error) {
	if audience == "" {
		return structure.Ticket{}, fmt.Errorf("sso: issue: audience is required")
	}
	if ttl <= 0 {
		return structure.Ticket{}, fmt.Errorf("sso: issue: ttl must be positive")
	}

	if _, found, err := principals.GetPrincipal(ctx, subjectPrincipalID); err != nil {
		return structure.Ticket{}, fmt.Errorf("sso: issue: %w", err)
	} else if !found {
		return structure.Ticket{}, ErrUnknownPrincipal
	}

	now := time.Now().Truncate(time.Microsecond)
	t := structure.Ticket{
		TicketID:           uuid.New(),
		SubjectPrincipalID: subjectPrincipalID,
		Audience:           audience,
		IssuedAt:           now,
		ExpiresAt:          now.Add(ttl),
	}

	digest := sha256.Sum256(t.SigningBytes())
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return structure.Ticket{}, fmt.Errorf("sso: issue: sign: %w", err)
	}
	t.Signature = sig

	if err := t.Validate(); err != nil {
		return structure.Ticket{}, fmt.Errorf("sso: issue: %w", err)
	}
	return t, nil
}
