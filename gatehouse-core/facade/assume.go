package facade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// Assumed sessions: see docs/architecture/09-gatehouse-core-model.md,
// "Assumed sessions." Work can involve four facts — A the actor who
// performs it, B the requester, C the effective principal whose
// permissions apply, and the authority source — and only when C is a
// third principal does this machinery matter at all.

// Permission keys and the context type for the two consent edges. Names
// are provisional (the model doc says so). Neither is wildcard-includable:
// a "gatehouse.*" grant should not silently let its holder act as anyone.
const (
	// PermissionAssumeCause is checked on the requester B: "B may cause
	// work to run as C." It is scoped to C through ContextTypePrincipal.
	PermissionAssumeCause = "gatehouse.assume.cause"

	// PermissionAssumeExecute is checked on the creator A: "A may execute
	// as C." Also scoped to C.
	PermissionAssumeExecute = "gatehouse.assume.execute"

	// ContextTypePrincipal is the context type whose ID is a principal
	// ID — the thing the two edges are scoped to.
	ContextTypePrincipal = "gatehouse.principal"
)

var (
	// ErrAssumeNotPermitted means one of the two consent edges does not
	// hold. It is a denial, never a store failure: store errors are
	// returned as themselves so a caller can tell them apart.
	ErrAssumeNotPermitted = errors.New("facade: assume: not permitted")

	// ErrAssumeCauseDenied and ErrAssumeExecuteDenied say which edge
	// failed. Both satisfy errors.Is(err, ErrAssumeNotPermitted). The
	// distinction matters to a caller: a failed cause edge means the job
	// as submitted is no longer permitted (terminal), while a failed
	// execute edge only means *this* executor may not run it (another
	// might).
	ErrAssumeCauseDenied   = fmt.Errorf("%w: requester may not cause work as that principal", ErrAssumeNotPermitted)
	ErrAssumeExecuteDenied = fmt.Errorf("%w: creator may not execute as that principal", ErrAssumeNotPermitted)

	ErrSessionNotFound   = errors.New("facade: session not found")
	ErrNotAssumedSession = errors.New("facade: session is not an assumed session")
	ErrSessionRevoked    = errors.New("facade: session is revoked")
	ErrSessionExpired    = errors.New("facade: session is expired")
	ErrPrincipalNotFound = errors.New("facade: principal not found")
)

// RegisterAssumePermissions registers the two edge permissions.
// Idempotent, so safe to call on every startup like every other
// RegisterPermissions in this design.
func RegisterAssumePermissions(ctx context.Context, reader Reader, writer Writer) error {
	opts := RegisterPermissionOptions{AllowReservedNamespace: true}
	for _, key := range []string{PermissionAssumeCause, PermissionAssumeExecute} {
		def := structure.PermissionDefinition{
			PermissionKey:          key,
			RequiredAuthorityLevel: structure.AuthorityLevelStandard,
			WildcardIncludable:     false,
		}
		if err := RegisterPermission(ctx, reader, writer, def, opts); err != nil {
			return fmt.Errorf("facade: register assume permissions: %w", err)
		}
	}
	return nil
}

// AssumeRequest names the three principals and the lifetime of an
// assumed session.
type AssumeRequest struct {
	CreatorPrincipalID   uuid.UUID // A: performs the work and holds the session
	RequesterPrincipalID uuid.UUID // B: asked for it
	AsPrincipalID        uuid.UUID // C: whose permissions apply
	TTL                  time.Duration
	Metadata             json.RawMessage // opaque; e.g. a binding to the job and attempt this session serves
}

// AssumeSession creates an assumed session after checking both consent
// edges. The edges are implicit when they would be circular: B needs no
// grant to cause work as B, and A needs none to execute as A.
//
// The session carries only standard authority, no credential, and a
// mandatory expiry (structure.Session enforces all three), so it can
// neither outlive its purpose nor reach elevated or recovery_access
// permissions.
func AssumeSession(ctx context.Context, reader Reader, writer Writer, req AssumeRequest) (structure.Session, error) {
	if req.TTL <= 0 {
		return structure.Session{}, fmt.Errorf("facade: assume session: TTL must be positive")
	}
	for _, id := range []uuid.UUID{req.CreatorPrincipalID, req.RequesterPrincipalID, req.AsPrincipalID} {
		if err := requirePrincipalExists(ctx, reader, id); err != nil {
			return structure.Session{}, fmt.Errorf("facade: assume session: %w", err)
		}
	}
	if err := requireAssumeEdges(ctx, reader, req.CreatorPrincipalID, req.RequesterPrincipalID, req.AsPrincipalID); err != nil {
		return structure.Session{}, fmt.Errorf("facade: assume session: %w", err)
	}

	now := time.Now().Truncate(time.Microsecond)
	expires := now.Add(req.TTL)
	creator, requester := req.CreatorPrincipalID, req.RequesterPrincipalID
	session := structure.Session{
		SessionID:              uuid.New(),
		PrincipalID:            req.AsPrincipalID,
		Kind:                   structure.SessionKindAssumed,
		AuthorityLevel:         structure.AuthorityLevelStandard,
		AuthenticationMethod:   "assumed",
		AssertedByPrincipalID:  &creator,
		RequestedByPrincipalID: &requester,
		Metadata:               req.Metadata,
		CreatedAt:              now,
		ExpiresAt:              &expires,
		LastSeen:               now,
	}
	if err := session.Validate(); err != nil {
		return structure.Session{}, fmt.Errorf("facade: assume session: %w", err)
	}
	if err := writer.CreateSession(ctx, session); err != nil {
		return structure.Session{}, fmt.Errorf("facade: assume session: %w", err)
	}
	return session, nil
}

// ValidateAssumedSession is the check an execution layer makes before
// honoring an assumed session: it exists, is of the right kind, is
// neither revoked nor expired, and — because grants get revoked — both
// consent edges still hold right now. It returns the session so the
// caller has A, B and C in hand.
//
// Like every Session in this package, expiry is judged against the
// calling node's clock; the same single-clock caveat the lease code has.
func ValidateAssumedSession(ctx context.Context, reader Reader, sessionID uuid.UUID) (structure.Session, error) {
	s, found, err := reader.GetSession(ctx, sessionID)
	if err != nil {
		return structure.Session{}, fmt.Errorf("facade: validate assumed session: %w", err)
	}
	if !found {
		return structure.Session{}, ErrSessionNotFound
	}
	if s.Kind != structure.SessionKindAssumed {
		return structure.Session{}, ErrNotAssumedSession
	}
	if s.IsRevoked() {
		return structure.Session{}, ErrSessionRevoked
	}
	if s.IsExpired(time.Now()) {
		return structure.Session{}, ErrSessionExpired
	}
	// Validate guarantees both are set for this kind, but a row written by
	// something else should not be able to cause a nil dereference here.
	if s.AssertedByPrincipalID == nil || s.RequestedByPrincipalID == nil {
		return structure.Session{}, fmt.Errorf("facade: validate assumed session: stored session is missing its creator or requester")
	}
	if err := requireAssumeEdges(ctx, reader, *s.AssertedByPrincipalID, *s.RequestedByPrincipalID, s.PrincipalID); err != nil {
		return structure.Session{}, fmt.Errorf("facade: validate assumed session: %w", err)
	}
	return s, nil
}

// RequireAssumedPermission checks permissionKey globally against the
// assumed session's effective principal C — after ValidateAssumedSession,
// so a revoked, expired or no-longer-permitted session denies everything.
//
// It does not enforce a scope. The thing that wants the authority (a
// job's task kind, say) declares which permissions it may exercise and
// checks membership itself; core evaluation does not change for this.
func RequireAssumedPermission(ctx context.Context, reader Reader, sessionID uuid.UUID, permissionKey string) error {
	s, err := ValidateAssumedSession(ctx, reader, sessionID)
	if err != nil {
		return err
	}
	return evaluation.RequirePermission(ctx, reader, s.PrincipalID, permissionKey)
}

// RequireAssumedContextPermission is RequireAssumedPermission scoped to
// one context.
func RequireAssumedContextPermission(ctx context.Context, reader Reader, sessionID uuid.UUID, permissionKey, contextType, contextID string) error {
	s, err := ValidateAssumedSession(ctx, reader, sessionID)
	if err != nil {
		return err
	}
	return evaluation.RequireContextPermission(ctx, reader, s.PrincipalID, permissionKey, contextType, contextID)
}

func requirePrincipalExists(ctx context.Context, reader Reader, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: nil principal ID", ErrPrincipalNotFound)
	}
	_, found, err := reader.GetPrincipal(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrPrincipalNotFound, id)
	}
	return nil
}

// requireAssumeEdges checks the two consent edges, skipping each when it
// would be circular. A denial wraps ErrAssumeNotPermitted; a store error
// is returned as itself.
func requireAssumeEdges(ctx context.Context, reader Reader, creator, requester, as uuid.UUID) error {
	if err := RequireCanCauseAs(ctx, reader, requester, as); err != nil {
		return err
	}
	return RequireCanExecuteAs(ctx, reader, creator, as)
}

// RequireCanCauseAs checks the requester's edge alone: may requester
// cause work to run as as? Implicit when they are the same principal.
// It exists so an integration can check the edge at submission, before
// any session is created. A denial wraps ErrAssumeCauseDenied; a store
// failure is returned as itself.
func RequireCanCauseAs(ctx context.Context, reader Reader, requester, as uuid.UUID) error {
	if requester == as {
		return nil
	}
	if err := requireEdge(ctx, reader, requester, PermissionAssumeCause, as); err != nil {
		if errors.Is(err, ErrAssumeNotPermitted) {
			return fmt.Errorf("%w (%v)", ErrAssumeCauseDenied, err)
		}
		return err
	}
	return nil
}

// RequireCanExecuteAs checks the creator's edge alone: may creator
// execute as as? Implicit when they are the same principal. A denial
// wraps ErrAssumeExecuteDenied; a store failure is returned as itself.
func RequireCanExecuteAs(ctx context.Context, reader Reader, creator, as uuid.UUID) error {
	if creator == as {
		return nil
	}
	if err := requireEdge(ctx, reader, creator, PermissionAssumeExecute, as); err != nil {
		if errors.Is(err, ErrAssumeNotPermitted) {
			return fmt.Errorf("%w (%v)", ErrAssumeExecuteDenied, err)
		}
		return err
	}
	return nil
}

func requireEdge(ctx context.Context, reader Reader, holder uuid.UUID, permissionKey string, as uuid.UUID) error {
	decision, err := evaluation.Evaluate(ctx, reader, evaluation.Request{
		PrincipalID:    holder,
		PermissionKey:  permissionKey,
		Scope:          evaluation.ScopeContext,
		ContextType:    ContextTypePrincipal,
		ContextID:      as.String(),
		AuthorityLevel: evaluation.AuthorityLevelStandard,
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w (%s)", ErrAssumeNotPermitted, decision.Reason)
	}
	return nil
}
