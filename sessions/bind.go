package sessions

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/transit"
	"github.com/google/uuid"
)

// Store is everything this package needs — just the two gatehouse-core
// mutations a connection-bound session's lifecycle drives. Declared
// here, in the consuming package, the same rule every other
// evaluation/integration boundary in this design follows.
type Store interface {
	CreateSession(ctx context.Context, s structure.Session) error
	RevokeSession(ctx context.Context, id uuid.UUID, revokedAt time.Time) error
}

// BindConnection creates a Gatehouse-core Session from draft (filling
// in SessionID/CreatedAt/LastSeen, same as facade.CreateSession) and
// ties its lifetime to conn: once conn.Done() fires, the session is
// revoked automatically, in the background, using a fresh context —
// the caller's ctx may already be gone by the time the connection
// actually drops, and revocation on disconnect must not be held
// hostage to a request-scoped context outliving the connection it
// describes.
//
// Returns an error without creating anything if conn is already
// closed — there's no point minting a session only to revoke it in
// the next instant.
func BindConnection(ctx context.Context, store Store, conn transit.Session, draft structure.Session) (structure.Session, error) {
	select {
	case <-conn.Done():
		return structure.Session{}, fmt.Errorf("sessions: bind connection: connection is already closed")
	default:
	}

	now := time.Now().Truncate(time.Microsecond)
	draft.SessionID = uuid.New()
	draft.CreatedAt = now
	draft.LastSeen = now
	if err := draft.Validate(); err != nil {
		return structure.Session{}, fmt.Errorf("sessions: bind connection: %w", err)
	}
	if err := store.CreateSession(ctx, draft); err != nil {
		return structure.Session{}, fmt.Errorf("sessions: bind connection: %w", err)
	}

	go func() {
		<-conn.Done()
		_ = store.RevokeSession(context.Background(), draft.SessionID, time.Now().Truncate(time.Microsecond))
	}()

	return draft, nil
}
