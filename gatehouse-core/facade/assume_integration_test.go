// Tests in this file need a real PostgreSQL instance, like the rest of
// the facade's integration tests, and skip without
// ARCHIPELAGO_TEST_DATABASE_URL.
package facade

import (
	"context"
	"errors"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

type assumeFixture struct {
	reader  Reader
	writer  Writer
	pool    *archidb.Pool
	a, b, c structure.Principal // creator/runner, requester, effective
}

func newAssumeFixture(t *testing.T) assumeFixture {
	t.Helper()
	reader, writer, pool := setupFacadeTestWithPool(t)
	ctx := context.Background()
	if err := RegisterAssumePermissions(ctx, reader, writer); err != nil {
		t.Fatalf("RegisterAssumePermissions: %v", err)
	}
	mk := func(key string, typ structure.PrincipalType) structure.Principal {
		p, err := EnsurePrincipal(ctx, reader, writer, key, typ)
		if err != nil {
			t.Fatalf("EnsurePrincipal(%s): %v", key, err)
		}
		return p
	}
	return assumeFixture{
		reader: reader, writer: writer, pool: pool,
		a: mk("runner.a", structure.PrincipalTypeServiceAccount),
		b: mk("user.b", structure.PrincipalTypeUser),
		c: mk("purpose.c", structure.PrincipalTypeServiceAccount),
	}
}

// grantOn gives holder permissionKey scoped to the context
// (contextType, contextID).
func (f assumeFixture) grantOn(t *testing.T, holder uuid.UUID, permissionKey, contextType, contextID string) {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	key, ct, ci := permissionKey, contextType, contextID
	err := f.writer.CreateGrant(context.Background(), structure.Grant{
		GrantID:       uuid.New(),
		SubjectType:   structure.GrantSubjectTypePrincipal,
		SubjectID:     holder,
		TargetType:    structure.GrantTargetTypePermission,
		PermissionKey: &key,
		Scope:         structure.GrantScopeContext,
		ContextType:   &ct,
		ContextID:     &ci,
		Effect:        structure.GrantEffectAllow,
		Status:        structure.GrantStatusActive,
		Origin:        structure.GrantOriginManual,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
}

func (f assumeFixture) allowCause(t *testing.T, requester, as uuid.UUID) {
	t.Helper()
	f.grantOn(t, requester, PermissionAssumeCause, ContextTypePrincipal, as.String())
}

func (f assumeFixture) allowExecute(t *testing.T, creator, as uuid.UUID) {
	t.Helper()
	f.grantOn(t, creator, PermissionAssumeExecute, ContextTypePrincipal, as.String())
}

func (f assumeFixture) assumedSessionCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.Pgx().QueryRow(context.Background(), `SELECT count(*) FROM gatehouse_sessions WHERE kind = 'assumed'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func (f assumeFixture) req(ttl time.Duration) AssumeRequest {
	return AssumeRequest{CreatorPrincipalID: f.a.PrincipalID, RequesterPrincipalID: f.b.PrincipalID, AsPrincipalID: f.c.PrincipalID, TTL: ttl}
}

func TestRegisterAssumePermissionsIsIdempotent(t *testing.T) {
	f := newAssumeFixture(t)
	if err := RegisterAssumePermissions(context.Background(), f.reader, f.writer); err != nil {
		t.Fatalf("second RegisterAssumePermissions: %v", err)
	}
	for _, key := range []string{PermissionAssumeCause, PermissionAssumeExecute} {
		def, found, err := f.reader.GetPermissionDefinition(context.Background(), key)
		if err != nil || !found {
			t.Fatalf("%s: found=%v err=%v", key, found, err)
		}
		if def.WildcardIncludable {
			t.Errorf("%s must not be wildcard-includable", key)
		}
	}
}

func TestAssumeSessionRequiresBothEdgesForAThirdPrincipal(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()

	// Neither edge: denied, and nothing is written.
	if _, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute)); !errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatalf("no edges: err = %v, want ErrAssumeNotPermitted", err)
	}
	// Only the requester's edge.
	f.allowCause(t, f.b.PrincipalID, f.c.PrincipalID)
	if _, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute)); !errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatalf("cause edge only: err = %v, want ErrAssumeNotPermitted", err)
	}
	if n := f.assumedSessionCount(t); n != 0 {
		t.Fatalf("%d assumed sessions written by denied attempts, want 0", n)
	}
	// Both edges.
	f.allowExecute(t, f.a.PrincipalID, f.c.PrincipalID)
	s, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute))
	if err != nil {
		t.Fatalf("both edges: %v", err)
	}

	if s.Kind != structure.SessionKindAssumed || s.PrincipalID != f.c.PrincipalID ||
		s.AuthorityLevel != structure.AuthorityLevelStandard || s.CredentialID != nil || s.ExpiresAt == nil {
		t.Fatalf("unexpected session shape: %+v", s)
	}
	got, found, err := f.reader.GetSession(ctx, s.SessionID)
	if err != nil || !found {
		t.Fatalf("GetSession: found=%v err=%v", found, err)
	}
	if got.AssertedByPrincipalID == nil || *got.AssertedByPrincipalID != f.a.PrincipalID {
		t.Errorf("creator A did not round-trip: %v", got.AssertedByPrincipalID)
	}
	if got.RequestedByPrincipalID == nil || *got.RequestedByPrincipalID != f.b.PrincipalID {
		t.Errorf("requester B did not round-trip: %v", got.RequestedByPrincipalID)
	}
}

func TestAssumeSessionOnlyOneEdgeIsNeededWhenTheOtherIsCircular(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()

	// Owner mode: C is the requester B. B needs no cause edge; only the
	// runner A must be allowed to execute as B.
	owner := AssumeRequest{CreatorPrincipalID: f.a.PrincipalID, RequesterPrincipalID: f.b.PrincipalID, AsPrincipalID: f.b.PrincipalID, TTL: time.Minute}
	if _, err := AssumeSession(ctx, f.reader, f.writer, owner); !errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatalf("owner mode without an execute edge: err = %v, want ErrAssumeNotPermitted", err)
	}
	f.allowExecute(t, f.a.PrincipalID, f.b.PrincipalID)
	if _, err := AssumeSession(ctx, f.reader, f.writer, owner); err != nil {
		t.Fatalf("owner mode with the execute edge: %v", err)
	}

	// Service mode: C is the creator A. A needs no execute edge; only
	// the requester B must be allowed to cause work as A.
	service := AssumeRequest{CreatorPrincipalID: f.a.PrincipalID, RequesterPrincipalID: f.b.PrincipalID, AsPrincipalID: f.a.PrincipalID, TTL: time.Minute}
	if _, err := AssumeSession(ctx, f.reader, f.writer, service); !errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatalf("service mode without a cause edge: err = %v, want ErrAssumeNotPermitted", err)
	}
	f.allowCause(t, f.b.PrincipalID, f.a.PrincipalID)
	if _, err := AssumeSession(ctx, f.reader, f.writer, service); err != nil {
		t.Fatalf("service mode with the cause edge: %v", err)
	}
}

func TestAssumeEdgesAreScopedToTheTargetPrincipal(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()
	other, err := EnsurePrincipal(ctx, f.reader, f.writer, "purpose.other", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	// Both edges granted for a DIFFERENT principal must not authorize C.
	f.allowCause(t, f.b.PrincipalID, other.PrincipalID)
	f.allowExecute(t, f.a.PrincipalID, other.PrincipalID)
	if _, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute)); !errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatalf("edges for another principal: err = %v, want ErrAssumeNotPermitted", err)
	}
}

func TestAssumeSessionRejectsBadInput(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()
	f.allowCause(t, f.b.PrincipalID, f.c.PrincipalID)
	f.allowExecute(t, f.a.PrincipalID, f.c.PrincipalID)

	if _, err := AssumeSession(ctx, f.reader, f.writer, f.req(0)); err == nil {
		t.Error("TTL of zero accepted")
	}
	if _, err := AssumeSession(ctx, f.reader, f.writer, f.req(-time.Second)); err == nil {
		t.Error("negative TTL accepted")
	}
	bad := f.req(time.Minute)
	bad.AsPrincipalID = uuid.New()
	if _, err := AssumeSession(ctx, f.reader, f.writer, bad); !errors.Is(err, ErrPrincipalNotFound) {
		t.Errorf("unknown effective principal: err = %v, want ErrPrincipalNotFound", err)
	}
}

func TestValidateAssumedSessionLifecycle(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()
	f.allowCause(t, f.b.PrincipalID, f.c.PrincipalID)
	f.allowExecute(t, f.a.PrincipalID, f.c.PrincipalID)

	s, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute))
	if err != nil {
		t.Fatalf("AssumeSession: %v", err)
	}
	if got, err := ValidateAssumedSession(ctx, f.reader, s.SessionID); err != nil || got.PrincipalID != f.c.PrincipalID {
		t.Fatalf("fresh session: %+v err=%v", got, err)
	}

	// Revoked.
	if err := RevokeSession(ctx, f.writer, s.SessionID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := ValidateAssumedSession(ctx, f.reader, s.SessionID); !errors.Is(err, ErrSessionRevoked) {
		t.Errorf("revoked: err = %v, want ErrSessionRevoked", err)
	}

	// Expired.
	short, err := AssumeSession(ctx, f.reader, f.writer, f.req(20*time.Millisecond))
	if err != nil {
		t.Fatalf("AssumeSession (short): %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := ValidateAssumedSession(ctx, f.reader, short.SessionID); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("expired: err = %v, want ErrSessionExpired", err)
	}

	// Wrong kind, and unknown.
	svc, err := CreateSession(ctx, f.writer, structure.Session{PrincipalID: f.c.PrincipalID, Kind: structure.SessionKindService, AuthorityLevel: structure.AuthorityLevelStandard})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := ValidateAssumedSession(ctx, f.reader, svc.SessionID); !errors.Is(err, ErrNotAssumedSession) {
		t.Errorf("service session: err = %v, want ErrNotAssumedSession", err)
	}
	if _, err := ValidateAssumedSession(ctx, f.reader, uuid.New()); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("unknown session: err = %v, want ErrSessionNotFound", err)
	}
}

func TestValidateAssumedSessionRechecksTheEdges(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()
	f.allowCause(t, f.b.PrincipalID, f.c.PrincipalID)
	f.allowExecute(t, f.a.PrincipalID, f.c.PrincipalID)
	s, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute))
	if err != nil {
		t.Fatalf("AssumeSession: %v", err)
	}

	// Revoke the requester's grant after the session exists. The Writer
	// has no revoke-grant operation yet, so go to the database directly.
	if _, err := f.pool.Pgx().Exec(ctx,
		`UPDATE gatehouse_grants SET status = 'revoked' WHERE permission_key = $1`, PermissionAssumeCause); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if _, err := ValidateAssumedSession(ctx, f.reader, s.SessionID); !errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatalf("after revoking the cause edge: err = %v, want ErrAssumeNotPermitted", err)
	}
}

func TestRequireAssumedPermissionEvaluatesAsTheEffectivePrincipal(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()
	f.allowCause(t, f.b.PrincipalID, f.c.PrincipalID)
	f.allowExecute(t, f.a.PrincipalID, f.c.PrincipalID)

	// Only C holds this; neither A nor B does.
	if err := RegisterPermission(ctx, f.reader, f.writer, structure.PermissionDefinition{PermissionKey: "myapp.backup.restore", RequiredAuthorityLevel: structure.AuthorityLevelStandard}, RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	if _, err := GrantPermission(ctx, f.writer, structure.GrantSubjectTypePrincipal, f.c.PrincipalID, "myapp.backup.restore"); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
	s, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute))
	if err != nil {
		t.Fatalf("AssumeSession: %v", err)
	}

	if err := RequireAssumedPermission(ctx, f.reader, s.SessionID, "myapp.backup.restore"); err != nil {
		t.Fatalf("C's permission should be usable through the session: %v", err)
	}
	// The runner's own and requester's own authority are not the point,
	// and a permission nobody granted C is denied.
	if err := RequireAssumedPermission(ctx, f.reader, s.SessionID, PermissionAssumeCause); err == nil {
		t.Error("a permission C does not hold was allowed")
	}
	// A revoked session denies everything, even what C holds.
	if err := RevokeSession(ctx, f.writer, s.SessionID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if err := RequireAssumedPermission(ctx, f.reader, s.SessionID, "myapp.backup.restore"); !errors.Is(err, ErrSessionRevoked) {
		t.Errorf("revoked session: err = %v, want ErrSessionRevoked", err)
	}
}

// failingPermissionReader makes the edge checks hit a store error, to
// prove it reaches the caller instead of becoming a denial.
type failingPermissionReader struct {
	Reader
	err error
}

func (r failingPermissionReader) GetPermissionDefinition(context.Context, string) (structure.PermissionDefinition, bool, error) {
	return structure.PermissionDefinition{}, false, r.err
}

func TestAssumeSessionStoreErrorsAreNotDenials(t *testing.T) {
	f := newAssumeFixture(t)
	sentinel := errors.New("store unreachable")
	_, err := AssumeSession(context.Background(), failingPermissionReader{Reader: f.reader, err: sentinel}, f.writer, f.req(time.Minute))
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap the store's own error", err)
	}
	if errors.Is(err, ErrAssumeNotPermitted) {
		t.Fatal("a store failure was reported as a denial")
	}
}

func TestEdgeDenialsSayWhichEdgeFailed(t *testing.T) {
	f := newAssumeFixture(t)
	ctx := context.Background()

	err := RequireCanCauseAs(ctx, f.reader, f.b.PrincipalID, f.c.PrincipalID)
	if !errors.Is(err, ErrAssumeCauseDenied) || !errors.Is(err, ErrAssumeNotPermitted) || errors.Is(err, ErrAssumeExecuteDenied) {
		t.Fatalf("cause edge: err = %v, want ErrAssumeCauseDenied (and ErrAssumeNotPermitted), not the execute error", err)
	}
	err = RequireCanExecuteAs(ctx, f.reader, f.a.PrincipalID, f.c.PrincipalID)
	if !errors.Is(err, ErrAssumeExecuteDenied) || !errors.Is(err, ErrAssumeNotPermitted) || errors.Is(err, ErrAssumeCauseDenied) {
		t.Fatalf("execute edge: err = %v, want ErrAssumeExecuteDenied (and ErrAssumeNotPermitted), not the cause error", err)
	}
	// Circular edges are implicit.
	if err := RequireCanCauseAs(ctx, f.reader, f.b.PrincipalID, f.b.PrincipalID); err != nil {
		t.Errorf("a principal causing work as itself: %v", err)
	}
	if err := RequireCanExecuteAs(ctx, f.reader, f.a.PrincipalID, f.a.PrincipalID); err != nil {
		t.Errorf("a principal executing as itself: %v", err)
	}
	// AssumeSession reports the failing edge too.
	f.allowCause(t, f.b.PrincipalID, f.c.PrincipalID)
	if _, err := AssumeSession(ctx, f.reader, f.writer, f.req(time.Minute)); !errors.Is(err, ErrAssumeExecuteDenied) {
		t.Errorf("AssumeSession with only the cause edge: err = %v, want ErrAssumeExecuteDenied", err)
	}
}
