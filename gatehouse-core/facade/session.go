package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// CreateSession fills in the fields a draft Session doesn't set for
// itself (ID, CreatedAt, LastSeen) and persists it. Unlike
// EnsurePrincipal, this is never idempotent-by-key — every call mints
// a genuinely new session, the same way logging in twice gets two
// sessions, not one shared between them.
func CreateSession(ctx context.Context, writer Writer, draft structure.Session) (structure.Session, error) {
	now := time.Now().Truncate(time.Microsecond) // see EnsurePrincipal's comment on why
	draft.SessionID = uuid.New()
	draft.CreatedAt = now
	draft.LastSeen = now
	if err := draft.Validate(); err != nil {
		return structure.Session{}, fmt.Errorf("facade: create session: %w", err)
	}
	if err := writer.CreateSession(ctx, draft); err != nil {
		return structure.Session{}, fmt.Errorf("facade: create session: %w", err)
	}
	return draft, nil
}

// RevokeSession marks a session revoked, effective immediately —
// per the model doc's standing invariant, nothing cached honors a
// revoked session regardless of what it still claims.
func RevokeSession(ctx context.Context, writer Writer, sessionID uuid.UUID) error {
	if err := writer.RevokeSession(ctx, sessionID, time.Now().Truncate(time.Microsecond)); err != nil {
		return fmt.Errorf("facade: revoke session: %w", err)
	}
	return nil
}
