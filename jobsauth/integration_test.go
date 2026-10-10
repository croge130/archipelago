// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. Both Gatehouse-core's and jobs'
// schemas are provisioned against the same database (their tables live
// in disjoint namespaces), so each test exercises the integration over
// real data in both, not fakes.
package jobsauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	jobsDB "github.com/croge130/archipelago/jobs/storage/dbstore"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

const taskKey = "myapp.report.render"
const queue = "myapp.reports"

type fx struct {
	d    Deps
	pool *archidb.Pool

	owner, runner, runner2, stranger, purpose gatehouseStructure.Principal
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func setup(t *testing.T) fx {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping jobsauth integration test")
	}
	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	for _, m := range []func() ([]archidb.Migration, error){gatehouseDB.Migrations, jobsDB.Migrations} {
		migrations, err := m()
		if err != nil {
			t.Fatalf("Migrations: %v", err)
		}
		if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
			t.Fatalf("ProvisionSchemas: %v", err)
		}
	}
	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_leases, gatehouse_instances, gatehouse_endpoint_definitions,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation, jobs_jobs, jobs_task_definitions
		CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	d := Deps{
		GatehouseReader: gatehouseDB.NewPostgresReader(pool.Pgx()),
		GatehouseWriter: gatehouseDB.NewPostgresWriter(pool.Pgx()),
		JobsReader:      jobsDB.NewPostgresReader(pool.Pgx()),
		JobsWriter:      jobsDB.NewPostgresWriter(pool.Pgx()),
	}
	ctx := context.Background()
	if err := gatehouseFacade.RegisterAssumePermissions(ctx, d.GatehouseReader, d.GatehouseWriter); err != nil {
		t.Fatalf("RegisterAssumePermissions: %v", err)
	}
	if err := RegisterPermissions(ctx, d.GatehouseReader, d.GatehouseWriter); err != nil {
		t.Fatalf("RegisterPermissions: %v", err)
	}
	for _, key := range []string{"myapp.report.read", "myapp.audit.write", "myapp.admin.all"} {
		if err := gatehouseFacade.RegisterPermission(ctx, d.GatehouseReader, d.GatehouseWriter,
			gatehouseStructure.PermissionDefinition{PermissionKey: key, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard},
			gatehouseFacade.RegisterPermissionOptions{}); err != nil {
			t.Fatalf("RegisterPermission(%s): %v", key, err)
		}
	}
	def := structure.TaskDefinition{
		TaskKey: taskKey, DefaultQueueKey: queue,
		Params: []structure.ParamSpec{
			{Name: "report_id", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
			{Name: "tenant", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
		},
		Scope: []structure.ScopeEntry{
			{PermissionKey: "myapp.report.read", ContextType: "myapp.tenant", ContextIDParam: "tenant"},
			{PermissionKey: "myapp.audit.write"},
		},
		Idempotent: true, DefaultMaxAttempts: 3, DefaultAttemptTimeout: time.Minute, PriorityCap: structure.PriorityCritical,
	}
	if err := jobsFacade.RegisterTaskDefinition(ctx, d.JobsReader, d.JobsWriter, def, jobsFacade.RegisterTaskOptions{}); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	mk := func(key string, typ gatehouseStructure.PrincipalType) gatehouseStructure.Principal {
		p, err := gatehouseFacade.EnsurePrincipal(ctx, d.GatehouseReader, d.GatehouseWriter, key, typ)
		if err != nil {
			t.Fatalf("EnsurePrincipal(%s): %v", key, err)
		}
		return p
	}
	return fx{d: d, pool: pool,
		owner:    mk("user.owner", gatehouseStructure.PrincipalTypeUser),
		runner:   mk("runner.one", gatehouseStructure.PrincipalTypeServiceAccount),
		runner2:  mk("runner.two", gatehouseStructure.PrincipalTypeServiceAccount),
		stranger: mk("user.stranger", gatehouseStructure.PrincipalTypeUser),
		purpose:  mk("purpose.restore", gatehouseStructure.PrincipalTypeServiceAccount),
	}
}

func (f fx) grantOn(t *testing.T, holder uuid.UUID, permissionKey, contextType, contextID string) {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	key, ct, ci := permissionKey, contextType, contextID
	if err := f.d.GatehouseWriter.CreateGrant(context.Background(), gatehouseStructure.Grant{
		GrantID: uuid.New(), SubjectType: gatehouseStructure.GrantSubjectTypePrincipal, SubjectID: holder,
		TargetType: gatehouseStructure.GrantTargetTypePermission, PermissionKey: &key,
		Scope: gatehouseStructure.GrantScopeContext, ContextType: &ct, ContextID: &ci,
		Effect: gatehouseStructure.GrantEffectAllow, Status: gatehouseStructure.GrantStatusActive,
		Origin: gatehouseStructure.GrantOriginManual, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
}

func (f fx) grantGlobal(t *testing.T, holder uuid.UUID, permissionKey string) {
	t.Helper()
	if _, err := gatehouseFacade.GrantPermission(context.Background(), f.d.GatehouseWriter, gatehouseStructure.GrantSubjectTypePrincipal, holder, permissionKey); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}
}

func (f fx) revokeGrants(t *testing.T, holder uuid.UUID, permissionKey string) {
	t.Helper()
	if _, err := f.pool.Pgx().Exec(context.Background(),
		`UPDATE gatehouse_grants SET status = 'revoked' WHERE subject_id = $1 AND permission_key = $2`, holder.String(), permissionKey); err != nil {
		t.Fatalf("revoke: %v", err)
	}
}

func (f fx) allowSubmit(t *testing.T, p uuid.UUID) {
	f.grantOn(t, p, PermissionSubmit, ContextTypeQueue, queue)
}
func (f fx) allowClaim(t *testing.T, p uuid.UUID) {
	f.grantOn(t, p, PermissionClaim, ContextTypeQueue, queue)
}
func (f fx) allowExecuteAs(t *testing.T, a, c uuid.UUID) {
	f.grantOn(t, a, gatehouseFacade.PermissionAssumeExecute, gatehouseFacade.ContextTypePrincipal, c.String())
}
func (f fx) allowCauseAs(t *testing.T, b, c uuid.UUID) {
	f.grantOn(t, b, gatehouseFacade.PermissionAssumeCause, gatehouseFacade.ContextTypePrincipal, c.String())
}

func params(report, tenant string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"report_id": report, "tenant": tenant})
	return b
}

func (f fx) submit(t *testing.T, requester uuid.UUID, mode structure.AuthorityMode, runAs *uuid.UUID, report string) structure.Job {
	t.Helper()
	res, err := Submit(ctxT(t), f.d, jobsFacade.SubmitRequest{TaskKey: taskKey, Params: params(report, "acme"), RequestedBy: requester, AuthorityMode: mode, RunAs: runAs})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return res.Job
}

func (f fx) claimReq(runner gatehouseStructure.Principal) ClaimRequest {
	return ClaimRequest{ExecutorInstanceID: uuid.New(), ExecutorPrincipalID: runner.PrincipalID, TaskKeys: []string{taskKey}, QueueKeys: []string{queue}, Limit: 5, ClaimTTL: time.Minute, ReleaseDelay: 20 * time.Millisecond}
}

func (f fx) job(t *testing.T, id uuid.UUID) structure.Job {
	t.Helper()
	j, found, err := f.d.JobsReader.GetJob(ctxT(t), id)
	if err != nil || !found {
		t.Fatalf("GetJob: found=%v err=%v", found, err)
	}
	return j
}

func TestRegisterPermissionsIsIdempotent(t *testing.T) {
	f := setup(t)
	if err := RegisterPermissions(ctxT(t), f.d.GatehouseReader, f.d.GatehouseWriter); err != nil {
		t.Fatalf("second RegisterPermissions: %v", err)
	}
	for _, key := range []string{PermissionSubmit, PermissionRead, PermissionClaim, PermissionCancel, PermissionExecute} {
		if _, found, err := f.d.GatehouseReader.GetPermissionDefinition(ctxT(t), key); err != nil || !found {
			t.Errorf("%s not registered: found=%v err=%v", key, found, err)
		}
	}
}

func TestSubmitIsAuthorizedPerQueue(t *testing.T) {
	f := setup(t)
	req := jobsFacade.SubmitRequest{TaskKey: taskKey, Params: params("r1", "acme"), RequestedBy: f.owner.PrincipalID}

	if _, err := Submit(ctxT(t), f.d, req); !errors.Is(err, evaluation.ErrDenied) {
		t.Fatalf("without a grant: err = %v, want a denial", err)
	}
	// A grant on a different queue must not help.
	f.grantOn(t, f.owner.PrincipalID, PermissionSubmit, ContextTypeQueue, "some.other.queue")
	if _, err := Submit(ctxT(t), f.d, req); !errors.Is(err, evaluation.ErrDenied) {
		t.Fatalf("with a grant on another queue: err = %v, want a denial", err)
	}
	f.allowSubmit(t, f.owner.PrincipalID)
	res, err := Submit(ctxT(t), f.d, req)
	if err != nil || !res.Created || res.Job.QueueKey != queue {
		t.Fatalf("with the grant: %+v err=%v", res, err)
	}
	// An unknown requester, and an unknown kind, are refused.
	bad := req
	bad.RequestedBy = uuid.New()
	if _, err := Submit(ctxT(t), f.d, bad); !errors.Is(err, ErrPrincipalNotFound) {
		t.Errorf("unknown requester: err = %v, want ErrPrincipalNotFound", err)
	}
	bad = req
	bad.TaskKey = "no.such.kind"
	if _, err := Submit(ctxT(t), f.d, bad); !errors.Is(err, jobsFacade.ErrUnknownTask) {
		t.Errorf("unknown kind: err = %v, want ErrUnknownTask", err)
	}
}

func TestSubmitAssumedChecksTheCauseEdgeAndTheTargetExists(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	c := f.purpose.PrincipalID
	req := jobsFacade.SubmitRequest{TaskKey: taskKey, Params: params("r1", "acme"), RequestedBy: f.owner.PrincipalID, AuthorityMode: structure.AuthorityAssumed, RunAs: &c}

	if _, err := Submit(ctxT(t), f.d, req); !errors.Is(err, gatehouseFacade.ErrAssumeCauseDenied) {
		t.Fatalf("without the cause edge: err = %v, want ErrAssumeCauseDenied", err)
	}
	f.allowCauseAs(t, f.owner.PrincipalID, c)
	if _, err := Submit(ctxT(t), f.d, req); err != nil {
		t.Fatalf("with the cause edge: %v", err)
	}
	ghost := uuid.New()
	req.RunAs = &ghost
	req.IdempotencyKey = "other"
	if _, err := Submit(ctxT(t), f.d, req); !errors.Is(err, ErrPrincipalNotFound) {
		t.Errorf("RunAs that does not exist: err = %v, want ErrPrincipalNotFound", err)
	}
	req.RunAs = nil
	if _, err := Submit(ctxT(t), f.d, req); err == nil {
		t.Error("assumed authority with no RunAs was accepted")
	}
}

func TestClaimNeedsExplicitQueuesAndTheClaimPermissionOnEach(t *testing.T) {
	f := setup(t)
	r := f.claimReq(f.runner)

	noQueues := r
	noQueues.QueueKeys = nil
	if _, _, err := Claim(ctxT(t), f.d, noQueues); err == nil {
		t.Error("a claim naming no queues was accepted: 'any queue' cannot be checked against a permission")
	}
	if _, _, err := Claim(ctxT(t), f.d, r); !errors.Is(err, evaluation.ErrDenied) {
		t.Fatalf("without jobs.claim: err = %v, want a denial", err)
	}
	f.allowClaim(t, f.runner.PrincipalID)
	if _, _, err := Claim(ctxT(t), f.d, r); err != nil {
		t.Fatalf("with jobs.claim: %v", err)
	}
	two := r
	two.QueueKeys = []string{queue, "q.not.granted"}
	if _, _, err := Claim(ctxT(t), f.d, two); !errors.Is(err, evaluation.ErrDenied) {
		t.Errorf("one ungranted queue among several must refuse the whole claim: err = %v", err)
	}
}

func TestOwnerModeRunsAsTheOwnerNarrowedToScopeAndRevokesItsSession(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	f.allowExecuteAs(t, f.runner.PrincipalID, f.owner.PrincipalID)
	// The owner holds the declared permission for acme, and something the
	// task never declared. The runner holds a permission the owner lacks.
	f.grantOn(t, f.owner.PrincipalID, "myapp.report.read", "myapp.tenant", "acme")
	f.grantGlobal(t, f.owner.PrincipalID, "myapp.admin.all")
	f.grantGlobal(t, f.runner.PrincipalID, "myapp.audit.write")

	job := f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")
	ready, skipped, err := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(skipped) != 0 || len(ready) != 1 {
		t.Fatalf("claim: ready=%d skipped=%+v err=%v", len(ready), skipped, err)
	}
	cj := ready[0]
	if cj.JobID != job.JobID || cj.EffectivePrincipalID != f.owner.PrincipalID || cj.ActorPrincipalID != f.runner.PrincipalID || cj.SessionID == nil {
		t.Fatalf("identities wrong: %+v", cj)
	}

	// The session is C = owner, created by A = runner at B = owner's request.
	sess, found, _ := f.d.GatehouseReader.GetSession(ctxT(t), *cj.SessionID)
	if !found || sess.Kind != gatehouseStructure.SessionKindAssumed || sess.PrincipalID != f.owner.PrincipalID ||
		*sess.AssertedByPrincipalID != f.runner.PrincipalID || *sess.RequestedByPrincipalID != f.owner.PrincipalID {
		t.Fatalf("session wrong: %+v", sess)
	}
	if want := time.Now().Add(time.Minute); sess.ExpiresAt.After(want.Add(5*time.Second)) || sess.ExpiresAt.Before(want.Add(-5*time.Second)) {
		t.Errorf("session expiry %v should be about the job's attempt timeout (1m) from now", sess.ExpiresAt)
	}
	stored := f.job(t, job.JobID)
	if stored.AssumedSessionID == nil || *stored.AssumedSessionID != *cj.SessionID || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != f.runner.PrincipalID {
		t.Errorf("claim identity not recorded on the job: %+v", stored)
	}

	// In scope and held by the owner: allowed, in its tenant context.
	if err := Authorize(ctxT(t), f.d, cj, "myapp.report.read", "myapp.tenant", "acme"); err != nil {
		t.Errorf("a declared, held permission was refused: %v", err)
	}
	// Declared but for a different tenant: outside the resolved scope.
	if err := Authorize(ctxT(t), f.d, cj, "myapp.report.read", "myapp.tenant", "globex"); !errors.Is(err, ErrOutOfScope) {
		t.Errorf("another tenant: err = %v, want ErrOutOfScope", err)
	}
	// Held by the owner but never declared: narrowing keeps it out of reach.
	if err := Authorize(ctxT(t), f.d, cj, "myapp.admin.all", "", ""); !errors.Is(err, ErrOutOfScope) {
		t.Errorf("an undeclared permission the owner holds: err = %v, want ErrOutOfScope", err)
	}
	// Declared, but held only by the RUNNER: evaluated as the owner, so denied.
	if err := Authorize(ctxT(t), f.d, cj, "myapp.audit.write", "", ""); !errors.Is(err, ErrAuthorityDenied) {
		t.Errorf("a permission only the runner holds: err = %v, want ErrAuthorityDenied (it must be evaluated as the owner)", err)
	}
	f.grantGlobal(t, f.owner.PrincipalID, "myapp.audit.write")
	if err := Authorize(ctxT(t), f.d, cj, "myapp.audit.write", "", ""); err != nil {
		t.Errorf("once the owner holds it: %v", err)
	}
	// Withdrawn mid-run: the next check sees it.
	f.revokeGrants(t, f.owner.PrincipalID, "myapp.audit.write")
	if err := Authorize(ctxT(t), f.d, cj, "myapp.audit.write", "", ""); !errors.Is(err, ErrAuthorityDenied) {
		t.Errorf("after the owner lost the permission: err = %v, want ErrAuthorityDenied", err)
	}

	// Completing revokes the session.
	if ok, err := Complete(ctxT(t), f.d, cj, json.RawMessage(`{"ok":true}`)); err != nil || !ok {
		t.Fatalf("Complete: ok=%v err=%v", ok, err)
	}
	if _, err := gatehouseFacade.ValidateAssumedSession(ctxT(t), f.d.GatehouseReader, *cj.SessionID); !errors.Is(err, gatehouseFacade.ErrSessionRevoked) {
		t.Errorf("after completion the session: err = %v, want ErrSessionRevoked", err)
	}
	if err := Authorize(ctxT(t), f.d, cj, "myapp.report.read", "myapp.tenant", "acme"); !errors.Is(err, gatehouseFacade.ErrSessionRevoked) {
		t.Errorf("Authorize after the session was revoked: err = %v, want ErrSessionRevoked (not an authority denial)", err)
	}
}

func TestExecutorWithoutTheExecuteEdgeReleasesWithoutSpendingAnAttempt(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	f.allowClaim(t, f.runner2.PrincipalID)
	f.allowExecuteAs(t, f.runner2.PrincipalID, f.owner.PrincipalID) // only runner2 may run owner's work
	job := f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")

	ready, skipped, err := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(ready) != 0 || len(skipped) != 1 || !skipped[0].Released {
		t.Fatalf("runner without the edge: ready=%d skipped=%+v err=%v, want it released", len(ready), skipped, err)
	}
	got := f.job(t, job.JobID)
	if got.State != structure.StatePending || got.Attempt != 0 || got.AssumedSessionID != nil {
		t.Fatalf("a released job should be pending with its attempt restored: %+v", got)
	}
	var sessions int
	_ = f.pool.Pgx().QueryRow(ctxT(t), `SELECT count(*) FROM gatehouse_sessions WHERE kind = 'assumed'`).Scan(&sessions)
	if sessions != 0 {
		t.Errorf("%d assumed sessions were created for a claim that was refused", sessions)
	}

	time.Sleep(60 * time.Millisecond) // past the release delay
	ready, skipped, err = Claim(ctxT(t), f.d, f.claimReq(f.runner2))
	if err != nil || len(skipped) != 0 || len(ready) != 1 || ready[0].JobID != job.JobID || ready[0].Attempt != 1 {
		t.Fatalf("runner2 with the edge: ready=%+v skipped=%+v err=%v, want the job as attempt 1", ready, skipped, err)
	}
}

func TestAssumedModeNeedsBothEdgesAndLosingTheCauseEdgeIsTerminal(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	c := f.purpose.PrincipalID
	f.allowCauseAs(t, f.owner.PrincipalID, c)
	f.allowExecuteAs(t, f.runner.PrincipalID, c)
	f.grantGlobal(t, c, "myapp.audit.write")

	ok := f.submit(t, f.owner.PrincipalID, structure.AuthorityAssumed, &c, "r-ok")
	ready, skipped, err := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(ready) != 1 || len(skipped) != 0 {
		t.Fatalf("both edges: ready=%d skipped=%+v err=%v", len(ready), skipped, err)
	}
	cj := ready[0]
	if cj.JobID != ok.JobID || cj.EffectivePrincipalID != c {
		t.Fatalf("effective principal should be the purpose principal: %+v", cj)
	}
	// Evaluated as C: C holds audit.write, the owner and runner do not.
	if err := Authorize(ctxT(t), f.d, cj, "myapp.audit.write", "", ""); err != nil {
		t.Errorf("C's permission through the session: %v", err)
	}
	if res, err := Fail(ctxT(t), f.d, cj, errors.New("transient")); err != nil || res.State != structure.StatePending {
		t.Fatalf("an ordinary failure on an idempotent kind: %+v err=%v, want pending", res, err)
	}
	if _, err := gatehouseFacade.ValidateAssumedSession(ctxT(t), f.d.GatehouseReader, *cj.SessionID); !errors.Is(err, gatehouseFacade.ErrSessionRevoked) {
		t.Errorf("Fail should revoke the session: err = %v", err)
	}

	// The requester loses the cause edge after the job was submitted.
	f.revokeGrants(t, f.owner.PrincipalID, gatehouseFacade.PermissionAssumeCause)
	if _, err := f.d.JobsWriter.CancelJob(ctxT(t), ok.JobID); err != nil { // retire the first job
		t.Fatalf("CancelJob: %v", err)
	}
	// (the first job's retry sits pending; submit a fresh one with the edge already gone)
	f.allowCauseAs(t, f.owner.PrincipalID, c)
	second := f.submit(t, f.owner.PrincipalID, structure.AuthorityAssumed, &c, "r-two")
	f.revokeGrants(t, f.owner.PrincipalID, gatehouseFacade.PermissionAssumeCause)

	ready, skipped, err = Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(ready) != 0 || len(skipped) != 1 || skipped[0].Released {
		t.Fatalf("lost cause edge: ready=%d skipped=%+v err=%v, want a terminal skip", len(ready), skipped, err)
	}
	got := f.job(t, second.JobID)
	if got.State != structure.StateDead {
		t.Errorf("a job whose requester may no longer cause it must end dead, got %s", got.State)
	}
}

func TestServiceModeRunsAsTheExecutorWithNoSession(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	f.grantGlobal(t, f.runner.PrincipalID, "myapp.audit.write")
	f.grantGlobal(t, f.owner.PrincipalID, "myapp.admin.all")

	f.submit(t, f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	ready, _, err := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(ready) != 1 {
		t.Fatalf("claim: %d err=%v", len(ready), err)
	}
	cj := ready[0]
	if cj.SessionID != nil || cj.EffectivePrincipalID != f.runner.PrincipalID {
		t.Fatalf("service mode runs as the executor with no session: %+v", cj)
	}
	if err := Authorize(ctxT(t), f.d, cj, "myapp.audit.write", "", ""); err != nil {
		t.Errorf("the executor's own permission: %v", err)
	}
	// The declared scope still bounds a service job.
	if err := Authorize(ctxT(t), f.d, cj, "myapp.admin.all", "", ""); !errors.Is(err, ErrOutOfScope) {
		t.Errorf("an undeclared permission: err = %v, want ErrOutOfScope", err)
	}
}

func TestAuthorityDenialIsTerminalButOrdinaryFailureRetries(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	f.submit(t, f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	f.submit(t, f.owner.PrincipalID, structure.AuthorityService, nil, "r2")
	ready, _, err := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(ready) != 2 {
		t.Fatalf("claim: %d err=%v", len(ready), err)
	}

	denied := Authorize(ctxT(t), f.d, ready[0], "myapp.audit.write", "", "") // the runner does not hold it
	if !errors.Is(denied, ErrAuthorityDenied) {
		t.Fatalf("setup: expected a denial, got %v", denied)
	}
	if res, err := Fail(ctxT(t), f.d, ready[0], denied); err != nil || res.State != structure.StateDead {
		t.Errorf("an authority denial: %+v err=%v, want dead — retrying a permission failure never helps", res, err)
	}
	if res, err := Fail(ctxT(t), f.d, ready[1], errors.New("timeout talking to a dependency")); err != nil || res.State != structure.StatePending {
		t.Errorf("an ordinary failure: %+v err=%v, want pending", res, err)
	}
}

func TestStaleExecutorIsRefusedAndItsSessionRevoked(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	f.allowClaim(t, f.runner2.PrincipalID)
	f.allowExecuteAs(t, f.runner.PrincipalID, f.owner.PrincipalID)
	f.allowExecuteAs(t, f.runner2.PrincipalID, f.owner.PrincipalID)
	job := f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")

	first, _, _ := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if len(first) != 1 {
		t.Fatal("expected the first claim")
	}
	// The first runner stalls; its claim lapses and another takes over.
	if _, err := f.pool.Pgx().Exec(ctxT(t), `UPDATE jobs_jobs SET claimed_until = now() - interval '1 second' WHERE job_id = $1`, job.JobID.String()); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	second, _, _ := Claim(ctxT(t), f.d, f.claimReq(f.runner2))
	if len(second) != 1 || second[0].Attempt != 2 {
		t.Fatalf("expected the takeover as attempt 2: %+v", second)
	}

	ok, err := Complete(ctxT(t), f.d, first[0], nil)
	if err != nil || ok {
		t.Fatalf("the stale executor's Complete: ok=%v err=%v, want refused", ok, err)
	}
	if _, err := gatehouseFacade.ValidateAssumedSession(ctxT(t), f.d.GatehouseReader, *first[0].SessionID); !errors.Is(err, gatehouseFacade.ErrSessionRevoked) {
		t.Errorf("the stale attempt's session must be revoked: err = %v", err)
	}
	if _, err := gatehouseFacade.ValidateAssumedSession(ctxT(t), f.d.GatehouseReader, *second[0].SessionID); err != nil {
		t.Errorf("the live attempt's session must be unaffected: %v", err)
	}
	if ok, err := Complete(ctxT(t), f.d, second[0], nil); err != nil || !ok {
		t.Errorf("the live executor's Complete: ok=%v err=%v", ok, err)
	}
}

func TestOwnerCanCancelAndReadOwnJobsOthersNeedThePermission(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	job := f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")

	// Read.
	if _, err := GetJob(ctxT(t), f.d, f.owner.PrincipalID, job.JobID); err != nil {
		t.Errorf("the owner reading their own job: %v", err)
	}
	if _, err := GetJob(ctxT(t), f.d, f.stranger.PrincipalID, job.JobID); !errors.Is(err, evaluation.ErrDenied) {
		t.Errorf("a stranger reading: err = %v, want a denial", err)
	}
	f.grantOn(t, f.stranger.PrincipalID, PermissionRead, ContextTypeQueue, queue)
	if _, err := GetJob(ctxT(t), f.d, f.stranger.PrincipalID, job.JobID); err != nil {
		t.Errorf("a stranger with jobs.read on the queue: %v", err)
	}
	if _, err := GetJob(ctxT(t), f.d, f.owner.PrincipalID, uuid.New()); !errors.Is(err, jobsFacade.ErrJobNotFound) {
		t.Errorf("unknown job: err = %v, want ErrJobNotFound", err)
	}

	// Cancel.
	if _, err := Cancel(ctxT(t), f.d, f.stranger.PrincipalID, job.JobID); !errors.Is(err, evaluation.ErrDenied) {
		t.Errorf("a stranger cancelling: err = %v, want a denial", err)
	}
	res, err := Cancel(ctxT(t), f.d, f.owner.PrincipalID, job.JobID)
	if err != nil || !res.Applied || res.State != structure.StateCancelled {
		t.Errorf("the owner cancelling: %+v err=%v", res, err)
	}
	other := f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "r2")
	f.grantOn(t, f.stranger.PrincipalID, PermissionCancel, ContextTypeQueue, queue)
	if res, err := Cancel(ctxT(t), f.d, f.stranger.PrincipalID, other.JobID); err != nil || !res.Applied {
		t.Errorf("a stranger with jobs.cancel on the queue: %+v err=%v", res, err)
	}
}

func TestListJobsReturnsOnlyWhatThePrincipalMayRead(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowSubmit(t, f.stranger.PrincipalID)
	mine := f.submit(t, f.stranger.PrincipalID, structure.AuthorityOwner, nil, "mine")
	theirs := f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "theirs")

	list := func(p uuid.UUID) map[uuid.UUID]bool {
		jobs, err := ListJobs(ctxT(t), f.d, p, structure.ListFilter{})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		out := map[uuid.UUID]bool{}
		for _, j := range jobs {
			out[j.JobID] = true
		}
		return out
	}
	got := list(f.stranger.PrincipalID)
	if !got[mine.JobID] || got[theirs.JobID] {
		t.Fatalf("a principal without jobs.read sees only its own jobs, got %v", got)
	}
	f.grantOn(t, f.stranger.PrincipalID, PermissionRead, ContextTypeQueue, queue)
	got = list(f.stranger.PrincipalID)
	if !got[mine.JobID] || !got[theirs.JobID] {
		t.Fatalf("with jobs.read on the queue both should be visible, got %v", got)
	}
}

func TestReadOnlyDepsFailClearlyOnWrites(t *testing.T) {
	f := setup(t)
	ro := Deps{GatehouseReader: f.d.GatehouseReader, JobsReader: f.d.JobsReader}
	if _, _, err := Claim(ctxT(t), ro, f.claimReq(f.runner)); err == nil {
		t.Error("Claim with no writers should fail clearly")
	}
	if _, err := Submit(ctxT(t), ro, jobsFacade.SubmitRequest{TaskKey: taskKey}); err == nil {
		t.Error("Submit with no jobs writer should fail clearly")
	}
	if _, err := Cancel(ctxT(t), ro, f.owner.PrincipalID, uuid.New()); err == nil {
		t.Error("Cancel with no jobs writer should fail clearly")
	}
}

func TestResumeRebuildsAClaimFromTheStoreAndRefusesAnythingElse(t *testing.T) {
	f := setup(t)
	f.allowSubmit(t, f.owner.PrincipalID)
	f.allowClaim(t, f.runner.PrincipalID)
	f.allowExecuteAs(t, f.runner.PrincipalID, f.owner.PrincipalID)
	f.grantOn(t, f.owner.PrincipalID, "myapp.report.read", "myapp.tenant", "acme")
	f.submit(t, f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")
	ready, _, err := Claim(ctxT(t), f.d, f.claimReq(f.runner))
	if err != nil || len(ready) != 1 {
		t.Fatalf("claim: %d %v", len(ready), err)
	}
	cj := ready[0]

	got, err := Resume(ctxT(t), f.d, f.runner.PrincipalID, cj.JobID, cj.Attempt)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got.JobID != cj.JobID || got.EffectivePrincipalID != cj.EffectivePrincipalID || got.SessionID == nil || *got.SessionID != *cj.SessionID || len(got.Scope) != len(cj.Scope) {
		t.Fatalf("resumed %+v, want it to match the claim %+v", got, cj)
	}
	// A resumed claim authorizes exactly as the original does.
	if err := Authorize(ctxT(t), f.d, got, "myapp.report.read", "myapp.tenant", "acme"); err != nil {
		t.Errorf("Authorize on a resumed claim: %v", err)
	}
	if err := Authorize(ctxT(t), f.d, got, "myapp.admin.all", "", ""); !errors.Is(err, ErrOutOfScope) {
		t.Errorf("an undeclared permission on a resumed claim: %v", err)
	}

	// Someone else's claim, the wrong attempt and an unknown job are all
	// ErrClaimLost and reveal nothing.
	for name, call := range map[string]func() (ClaimedJob, error){
		"another actor":   func() (ClaimedJob, error) { return Resume(ctxT(t), f.d, f.runner2.PrincipalID, cj.JobID, cj.Attempt) },
		"another attempt": func() (ClaimedJob, error) { return Resume(ctxT(t), f.d, f.runner.PrincipalID, cj.JobID, cj.Attempt+1) },
		"unknown job":     func() (ClaimedJob, error) { return Resume(ctxT(t), f.d, f.runner.PrincipalID, uuid.New(), 1) },
	} {
		r, err := call()
		if !errors.Is(err, ErrClaimLost) || r.JobID != uuid.Nil {
			t.Errorf("%s: got %+v, %v; want ErrClaimLost and an empty job", name, r, err)
		}
	}

	// Once finished, the same actor and attempt is told so, with the record,
	// so a retried completion can recognise that the first one landed.
	if ok, err := Complete(ctxT(t), f.d, cj, nil); err != nil || !ok {
		t.Fatalf("Complete: %v %v", ok, err)
	}
	r, err := Resume(ctxT(t), f.d, f.runner.PrincipalID, cj.JobID, cj.Attempt)
	if !errors.Is(err, ErrClaimLost) || r.State != structure.StateSucceeded {
		t.Errorf("after completion: state %q, err %v; want succeeded and ErrClaimLost", r.State, err)
	}
}
