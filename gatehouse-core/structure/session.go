package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SessionKind. asserted covers both today's SSO case and the still-
// deferred cross-app case — named to match AssertedByPrincipalID rather
// than introducing a second word for the same job.
type SessionKind string

const (
	SessionKindUI         SessionKind = "ui"
	SessionKindCLI        SessionKind = "cli"
	SessionKindAgent      SessionKind = "agent"
	SessionKindService    SessionKind = "service"
	SessionKindAutomation SessionKind = "automation"
	SessionKindAsserted   SessionKind = "asserted"
)

func (k SessionKind) Valid() bool {
	switch k {
	case SessionKindUI, SessionKindCLI, SessionKindAgent, SessionKindService,
		SessionKindAutomation, SessionKindAsserted:
		return true
	default:
		return false
	}
}

// AuthenticationMethod is open-ended on purpose — Gatehouse-core owns
// the vocabulary (password, totp, passkey, sso_assertion, ...) but this
// type doesn't enumerate it exhaustively here; validity of a specific
// value is an Evaluation-layer concern tied to which methods are
// actually wired up, not a fixed closed set at the Structure layer.
type AuthenticationMethod string

// Session is who/what is currently authenticated or asserted. Per the
// model doc's confirmed invariant, a Session alone never authenticates
// a request — only explicit Credential material does — which is why
// CredentialID is optional and singular rather than the session
// carrying its own ambient authority.
//
// Deliberately absent: any notion of a live connection. Connection-
// binding is the "Sessions" Layer 2 integration's job (Gatehouse-core +
// Transit, see 01-build-order.md), not a field this base record carries.
type Session struct {
	SessionID             uuid.UUID
	PrincipalID           uuid.UUID
	CredentialID          *uuid.UUID
	Kind                  SessionKind
	AuthorityLevel        AuthorityLevel
	AuthenticationMethod  AuthenticationMethod
	AssertedByPrincipalID *uuid.UUID
	Metadata              json.RawMessage
	CreatedAt             time.Time
	ExpiresAt             *time.Time
	LastSeen              time.Time
	RevokedAt             *time.Time
}

func (s Session) Validate() error {
	if s.SessionID == uuid.Nil {
		return fmt.Errorf("structure: session: SessionID is required")
	}
	if s.PrincipalID == uuid.Nil {
		return fmt.Errorf("structure: session: PrincipalID is required")
	}
	if !s.Kind.Valid() {
		return fmt.Errorf("structure: session: invalid Kind %q", s.Kind)
	}
	if !s.AuthorityLevel.Valid() {
		return fmt.Errorf("structure: session: invalid AuthorityLevel %q", s.AuthorityLevel)
	}
	if s.Kind != SessionKindAsserted && s.AssertedByPrincipalID != nil {
		return fmt.Errorf("structure: session: AssertedByPrincipalID is only valid when Kind is %q", SessionKindAsserted)
	}
	return nil
}

// IsRevoked reports whether the session has been revoked. Per the
// model doc's standing invariant, a revoked session is never honored
// by anything cached, regardless of what any snapshot still claims.
func (s Session) IsRevoked() bool {
	return s.RevokedAt != nil
}

// IsExpired reports whether the session has passed its own expiry,
// independent of revocation.
func (s Session) IsExpired(now time.Time) bool {
	return s.ExpiresAt != nil && now.After(*s.ExpiresAt)
}
