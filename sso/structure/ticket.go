package structure

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Ticket is a short-lived, signed, audience-bound assertion that
// SubjectPrincipalID was recently authenticated by the one signing key
// this authority domain trusts — identity-only, never a permission
// claim. See docs/architecture/12-sso-tickets-model.md for the full
// field-by-field rationale, including why there's no Issuer field
// (exactly one trusted key per domain) and no payload/metadata field
// (a ticket that carried one would quietly become a second
// authorization mechanism).
type Ticket struct {
	TicketID           uuid.UUID
	SubjectPrincipalID uuid.UUID
	Audience           string
	IssuedAt           time.Time
	ExpiresAt          time.Time
	Signature          []byte
}

func (t Ticket) Validate() error {
	if t.TicketID == uuid.Nil {
		return fmt.Errorf("structure: ticket: TicketID is required")
	}
	if t.SubjectPrincipalID == uuid.Nil {
		return fmt.Errorf("structure: ticket: SubjectPrincipalID is required")
	}
	if t.Audience == "" {
		return fmt.Errorf("structure: ticket: Audience is required")
	}
	if t.IssuedAt.IsZero() {
		return fmt.Errorf("structure: ticket: IssuedAt is required")
	}
	if t.ExpiresAt.IsZero() {
		return fmt.Errorf("structure: ticket: ExpiresAt is required")
	}
	if !t.ExpiresAt.After(t.IssuedAt) {
		return fmt.Errorf("structure: ticket: ExpiresAt must be after IssuedAt")
	}
	if len(t.Signature) == 0 {
		return fmt.Errorf("structure: ticket: Signature is required")
	}
	return nil
}

// SigningBytes is the canonical, deterministic encoding of every field
// except Signature itself — what gets hashed and signed at issuance,
// and recomputed identically at verification. Fixed field order, UUIDs
// in their canonical string form, timestamps as RFC3339Nano in UTC —
// see 12-sso-tickets-model.md's Signing section for why this is a
// plain deterministic encoding rather than a general-purpose
// canonicalization scheme: there are exactly five fields, fixed in
// number and order for as long as the type exists.
func (t Ticket) SigningBytes() []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%s",
		t.TicketID.String(),
		t.SubjectPrincipalID.String(),
		t.Audience,
		t.IssuedAt.UTC().Format(time.RFC3339Nano),
		t.ExpiresAt.UTC().Format(time.RFC3339Nano),
	))
}
