// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. A director (with the stores) and
// executors (holding only a router connection) are joined over the
// in-memory pipe, with the accepting side reporting each executor's
// verified identity, so the whole path — routerauth, peerauth, jobsauth,
// Gatehouse-core — is the real one. The executors never touch a store.
package jobsdirector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	jobsDB "github.com/croge130/archipelago/jobs/storage/dbstore"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/jobsauth"
	"github.com/croge130/archipelago/jobsexec"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/routerauth"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

const (
	taskKey = "myapp.report.render"
	queue   = "myapp.reports"
)

type fx struct {
	t    *testing.T
	d    jobsauth.Deps
	pool *archidb.Pool
	rt   *router.Router
	reg  *routerauth.Registrar

	owner, stranger, purpose gatehouseStructure.Principal
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func setup(t *testing.T) *fx {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping jobsdirector integration test")
	}
	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	for _, m := range []func() ([]archidb.Migration, error){gatehouseDB.Migrations, jobsDB.Migrations} {
		migrations, err := m()
		if err != nil {
			t.Fatal(err)
		}
		if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
			t.Fatal(err)
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
	d := jobsauth.Deps{
		GatehouseReader: gatehouseDB.NewPostgresReader(pool.Pgx()), GatehouseWriter: gatehouseDB.NewPostgresWriter(pool.Pgx()),
		JobsReader: jobsDB.NewPostgresReader(pool.Pgx()), JobsWriter: jobsDB.NewPostgresWriter(pool.Pgx()),
	}
	ctx := context.Background()
	for _, reg := range []func() error{
		func() error {
			return gatehouseFacade.RegisterAssumePermissions(ctx, d.GatehouseReader, d.GatehouseWriter)
		},
		func() error { return jobsauth.RegisterPermissions(ctx, d.GatehouseReader, d.GatehouseWriter) },
		func() error { return RegisterPermissions(ctx, d.GatehouseReader, d.GatehouseWriter) },
	} {
		if err := reg(); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"myapp.report.read", "myapp.audit.write", "myapp.admin.all"} {
		if err := gatehouseFacade.RegisterPermission(ctx, d.GatehouseReader, d.GatehouseWriter,
			gatehouseStructure.PermissionDefinition{PermissionKey: key, RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard},
			gatehouseFacade.RegisterPermissionOptions{}); err != nil {
			t.Fatal(err)
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
		t.Fatal(err)
	}

	rt := router.New(router.Options{Logger: quiet()})
	reg, err := routerauth.New(rt, d.GatehouseReader, d.GatehouseWriter)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := New(Config{Deps: d, ClaimTTL: 5 * time.Second, Logger: quiet()})
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.Register(ctx, reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	f := &fx{t: t, d: d, pool: pool, rt: rt, reg: reg}
	f.owner = f.principal("user.owner", gatehouseStructure.PrincipalTypeUser)
	f.stranger = f.principal("user.stranger", gatehouseStructure.PrincipalTypeUser)
	f.purpose = f.principal("purpose.restore", gatehouseStructure.PrincipalTypeServiceAccount)
	f.grantOn(f.owner.PrincipalID, jobsauth.PermissionSubmit, jobsauth.ContextTypeQueue, queue)
	return f
}

func (f *fx) principal(key string, typ gatehouseStructure.PrincipalType) gatehouseStructure.Principal {
	f.t.Helper()
	p, err := gatehouseFacade.EnsurePrincipal(context.Background(), f.d.GatehouseReader, f.d.GatehouseWriter, key, typ)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fx) grantOn(holder uuid.UUID, permissionKey, contextType, contextID string) {
	f.t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	key, ct, ci := permissionKey, contextType, contextID
	if err := f.d.GatehouseWriter.CreateGrant(context.Background(), gatehouseStructure.Grant{
		GrantID: uuid.New(), SubjectType: gatehouseStructure.GrantSubjectTypePrincipal, SubjectID: holder,
		TargetType: gatehouseStructure.GrantTargetTypePermission, PermissionKey: &key,
		Scope: gatehouseStructure.GrantScopeContext, ContextType: &ct, ContextID: &ci,
		Effect: gatehouseStructure.GrantEffectAllow, Status: gatehouseStructure.GrantStatusActive,
		Origin: gatehouseStructure.GrantOriginManual, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fx) grantGlobal(holder uuid.UUID, permissionKey string) {
	f.t.Helper()
	if _, err := gatehouseFacade.GrantPermission(context.Background(), f.d.GatehouseWriter, gatehouseStructure.GrantSubjectTypePrincipal, holder, permissionKey); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fx) revokeGrants(holder uuid.UUID, permissionKey string) {
	f.t.Helper()
	if _, err := f.pool.Pgx().Exec(context.Background(),
		`UPDATE gatehouse_grants SET status = 'revoked' WHERE subject_id = $1 AND permission_key = $2`, holder.String(), permissionKey); err != nil {
		f.t.Fatal(err)
	}
}

// node is one executor: a principal bound to a certificate fingerprint, a
// registered instance, and a router connection to the director.
type node struct {
	principal gatehouseStructure.Principal
	instance  uuid.UUID
	peer      *router.Peer
}

type nodeOpts struct {
	skipExecute, skipClaim bool
}

func (f *fx) node(key string, o nodeOpts) *node {
	f.t.Helper()
	ctx := context.Background()
	p := f.principal(key, gatehouseStructure.PrincipalTypeServiceAccount)
	fp := "fp:" + key
	if _, err := gatehouseFacade.EnsureMTLSCredential(ctx, f.d.GatehouseReader, f.d.GatehouseWriter, p.PrincipalID, fp); err != nil {
		f.t.Fatal(err)
	}
	if !o.skipExecute {
		f.grantGlobal(p.PrincipalID, PermissionExecute)
	}
	if !o.skipClaim {
		f.grantOn(p.PrincipalID, jobsauth.PermissionClaim, jobsauth.ContextTypeQueue, queue)
	}
	inst, err := gatehouseFacade.RegisterInstance(ctx, f.d.GatehouseWriter, p.PrincipalID, "workers", nil)
	if err != nil {
		f.t.Fatal(err)
	}
	dc, ac := inmem.NewPipe()
	ac.WithPeerIdentity(transit.PeerIdentity{Present: true, Fingerprint: fp})
	a := f.rt.Accept(ac)
	peer, err := router.New(router.Options{Logger: quiet()}).Connect(ctxT(f.t), dc, key)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { peer.Close(); a.Close() })
	return &node{principal: p, instance: inst.InstanceID, peer: peer}
}

func (f *fx) executor(n *node, cfg jobsexec.Config) *jobsexec.Executor {
	f.t.Helper()
	cfg.InstanceID, cfg.QueueKeys = n.instance, []string{queue}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 20 * time.Millisecond
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 25 * time.Millisecond
	}
	cfg.Logger = quiet()
	e, err := jobsexec.New(n.peer, cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

func params(report string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"report_id": report, "tenant": "acme"})
	return b
}

func (f *fx) submit(requester uuid.UUID, mode structure.AuthorityMode, runAs *uuid.UUID, report string) structure.Job {
	f.t.Helper()
	res, err := jobsauth.Submit(ctxT(f.t), f.d, jobsFacade.SubmitRequest{TaskKey: taskKey, Params: params(report), RequestedBy: requester, AuthorityMode: mode, RunAs: runAs})
	if err != nil {
		f.t.Fatalf("Submit: %v", err)
	}
	return res.Job
}

func (f *fx) job(id uuid.UUID) structure.Job {
	f.t.Helper()
	j, found, err := f.d.JobsReader.GetJob(ctxT(f.t), id)
	if err != nil || !found {
		f.t.Fatalf("GetJob: %v %v", found, err)
	}
	return j
}

func (f *fx) waitState(id uuid.UUID, want structure.State) structure.Job {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		j := f.job(id)
		if j.State == want {
			return j
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("job %s is %q, wanted %q", id, j.State, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func remoteCode(t *testing.T, err error, want wire.ErrorCode) {
	t.Helper()
	var re *router.RemoteError
	if !errors.As(err, &re) || re.Code != want {
		t.Fatalf("error = %v, want remote code %q", err, want)
	}
}

func TestAServiceJobRunsOnAnExecutorThatHoldsNoStore(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	job := f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r1")

	e := f.executor(n, jobsexec.Config{})
	var saw jobsexec.Job
	if err := e.Handle(taskKey, func(_ context.Context, j jobsexec.Job) (json.RawMessage, error) {
		saw = j
		return json.RawMessage(`{"rendered":true}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	if started, err := e.PullOnce(ctxT(t)); err != nil || started != 1 {
		t.Fatalf("PullOnce = %d, %v", started, err)
	}
	done := f.waitState(job.JobID, structure.StateSucceeded)
	if string(done.Result) != `{"rendered": true}` && string(done.Result) != `{"rendered":true}` {
		t.Errorf("result = %s", done.Result)
	}
	if done.Attempt != 1 || done.ActorPrincipalID == nil || *done.ActorPrincipalID != n.principal.PrincipalID || done.ClaimedBy == nil || *done.ClaimedBy != n.instance {
		t.Errorf("claim identity wrong: attempt %d actor %v claimedBy %v", done.Attempt, done.ActorPrincipalID, done.ClaimedBy)
	}
	if saw.ID != job.JobID || saw.Attempt != 1 || saw.EffectivePrincipalID != n.principal.PrincipalID || saw.AssumedSessionID != nil {
		t.Errorf("the handler saw %+v", saw)
	}
}

func TestAnOwnersJobAuthorizesThroughTheDirectorAndADenialIsTerminal(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	f.grantOn(n.principal.PrincipalID, gatehouseFacade.PermissionAssumeExecute, gatehouseFacade.ContextTypePrincipal, f.owner.PrincipalID.String())
	f.grantOn(f.owner.PrincipalID, "myapp.report.read", "myapp.tenant", "acme")
	f.grantGlobal(f.owner.PrincipalID, "myapp.admin.all")
	f.grantGlobal(n.principal.PrincipalID, "myapp.audit.write") // the executor holds it; the owner does not
	job := f.submit(f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")

	e := f.executor(n, jobsexec.Config{})
	var mu sync.Mutex
	got := map[string]error{}
	if err := e.Handle(taskKey, func(ctx context.Context, j jobsexec.Job) (json.RawMessage, error) {
		rec := func(name string, err error) { mu.Lock(); got[name] = err; mu.Unlock() }
		rec("in scope", j.Authorize(ctx, "myapp.report.read", "myapp.tenant", "acme"))
		rec("other tenant", j.Authorize(ctx, "myapp.report.read", "myapp.tenant", "globex"))
		rec("undeclared", j.Authorize(ctx, "myapp.admin.all", "", ""))
		denied := j.Authorize(ctx, "myapp.audit.write", "", "")
		rec("held only by the executor", denied)
		return nil, denied // the handler gives up on the denial
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PullOnce(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	dead := f.waitState(job.JobID, structure.StateDead)
	mu.Lock()
	defer mu.Unlock()
	if got["in scope"] != nil {
		t.Errorf("a declared, held permission: %v", got["in scope"])
	}
	if !errors.Is(got["other tenant"], jobsexec.ErrOutOfScope) || !errors.Is(got["undeclared"], jobsexec.ErrOutOfScope) {
		t.Errorf("scope: other tenant %v, undeclared %v; want ErrOutOfScope for both", got["other tenant"], got["undeclared"])
	}
	if err := got["held only by the executor"]; !errors.Is(err, jobsexec.ErrAuthorityDenied) || errors.Is(err, jobsexec.ErrOutOfScope) {
		t.Errorf("a permission only the executor holds: %v; want a plain denial (it is evaluated as the owner)", err)
	}
	// Terminal despite the kind being idempotent with attempts left.
	if dead.Attempt != 1 {
		t.Errorf("attempt = %d; a permission failure must not be retried", dead.Attempt)
	}
	if dead.AssumedSessionID == nil {
		t.Fatal("the owner's job should have run under an assumed session")
	}
	if _, err := gatehouseFacade.ValidateAssumedSession(ctxT(t), f.d.GatehouseReader, *dead.AssumedSessionID); !errors.Is(err, gatehouseFacade.ErrSessionRevoked) {
		t.Errorf("after the job ended its session: %v, want ErrSessionRevoked", err)
	}
}

func TestAnOrdinaryFailureIsRetriedOnTheNextPull(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	job := f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	e := f.executor(n, jobsexec.Config{})
	var calls atomic.Int32
	_ = e.Handle(taskKey, func(context.Context, jobsexec.Job) (json.RawMessage, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("flaky")
		}
		return nil, nil
	})
	if _, err := e.PullOnce(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	f.waitState(job.JobID, structure.StatePending)
	if j := f.job(job.JobID); j.LastError == "" || j.Attempt != 1 {
		t.Fatalf("after the failure: %+v", j)
	}
	// The task's backoff (tested in the jobs base) would hold the retry for
	// seconds; make it due now so this test is about delivery, not waiting.
	if _, err := f.pool.Pgx().Exec(context.Background(), `UPDATE jobs_jobs SET run_at = now() WHERE job_id = $1`, job.JobID); err != nil {
		t.Fatal(err)
	}
	if started, err := e.PullOnce(ctxT(t)); err != nil || started != 1 {
		t.Fatalf("the retry pull = %d, %v", started, err)
	}
	f.waitState(job.JobID, structure.StateSucceeded)
	if j := f.job(job.JobID); j.Attempt != 2 || calls.Load() != 2 {
		t.Errorf("attempt %d, handler calls %d; want 2 and 2", j.Attempt, calls.Load())
	}
}

func TestTheProtocolRefusesWhatItShould(t *testing.T) {
	f := setup(t)
	f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	pullAs := func(n *node, instance uuid.UUID) error {
		payload, _ := json.Marshal(jobsexec.PullRequest{InstanceID: instance, TaskKeys: []string{taskKey}, QueueKeys: []string{queue}, Limit: 1})
		_, err := n.peer.Call(ctxT(t), jobsexec.RoutePull, payload)
		return err
	}

	noExec := f.node("exec.noexec", nodeOpts{skipExecute: true})
	remoteCode(t, pullAs(noExec, noExec.instance), wire.ErrUnauthorized)

	noClaim := f.node("exec.noclaim", nodeOpts{skipClaim: true})
	remoteCode(t, pullAs(noClaim, noClaim.instance), wire.ErrUnauthorized)

	good := f.node("exec.good", nodeOpts{})
	thief := f.node("exec.thief", nodeOpts{})
	// Someone else's instance, and one that does not exist, are not usable.
	remoteCode(t, pullAs(thief, good.instance), wire.ErrUnauthorized)
	remoteCode(t, pullAs(thief, uuid.New()), wire.ErrUnauthorized)

	for name, payload := range map[string]string{
		"no kinds":  `{"instance_id":"` + good.instance.String() + `","queue_keys":["q"],"limit":1}`,
		"no queues": `{"instance_id":"` + good.instance.String() + `","task_keys":["k"],"limit":1}`,
		"no limit":  `{"instance_id":"` + good.instance.String() + `","task_keys":["k"],"queue_keys":["q"]}`,
		"not json":  `[`,
	} {
		_, err := good.peer.Call(ctxT(t), jobsexec.RoutePull, json.RawMessage(payload))
		if name != "" {
			remoteCode(t, err, wire.ErrInvalid)
		}
	}

	// The refused callers took nothing.
	jobs, _ := f.d.JobsReader.ListJobs(ctxT(t), structure.ListFilter{})
	for _, j := range jobs {
		if j.State != structure.StatePending {
			t.Errorf("job %s is %q after only refused pulls", j.JobID, j.State)
		}
	}
}

func TestFinishingCallsAreFencedAndSafeToRetry(t *testing.T) {
	f := setup(t)
	a := f.node("exec.a", nodeOpts{})
	b := f.node("exec.b", nodeOpts{})
	j1 := f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	j2 := f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r2")

	pull := func(n *node, limit int) []jobsexec.PulledJob {
		payload, _ := json.Marshal(jobsexec.PullRequest{InstanceID: n.instance, TaskKeys: []string{taskKey}, QueueKeys: []string{queue}, Limit: limit})
		raw, err := n.peer.Call(ctxT(t), jobsexec.RoutePull, payload)
		if err != nil {
			t.Fatalf("pull: %v", err)
		}
		var r jobsexec.PullReply
		_ = json.Unmarshal(raw, &r)
		return r.Jobs
	}
	call := func(n *node, route string, req any) jobsexec.FinishReply {
		payload, _ := json.Marshal(req)
		raw, err := n.peer.Call(ctxT(t), route, payload)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		var r jobsexec.FinishReply
		_ = json.Unmarshal(raw, &r)
		return r
	}
	got := pull(a, 2)
	if len(got) != 2 {
		t.Fatalf("pulled %d", len(got))
	}
	ref := func(n *node, pj jobsexec.PulledJob, attempt int) jobsexec.ClaimRef {
		return jobsexec.ClaimRef{JobID: pj.JobID, Attempt: attempt, InstanceID: n.instance}
	}
	var one, two jobsexec.PulledJob
	for _, pj := range got {
		if pj.JobID == j1.JobID {
			one = pj
		} else {
			two = pj
		}
	}

	// Another executor cannot finish a claim it does not hold.
	if r := call(b, jobsexec.RouteComplete, jobsexec.CompleteRequest{ClaimRef: ref(b, one, one.Attempt)}); r.Applied {
		t.Error("another executor completed a claim it does not hold")
	}
	// Nor the wrong attempt.
	if r := call(a, jobsexec.RouteComplete, jobsexec.CompleteRequest{ClaimRef: ref(a, one, one.Attempt+1)}); r.Applied {
		t.Error("a wrong attempt was applied")
	}
	// The right one lands; sending it again is confirmed, not applied twice.
	first := call(a, jobsexec.RouteComplete, jobsexec.CompleteRequest{ClaimRef: ref(a, one, one.Attempt), Result: json.RawMessage(`1`)})
	again := call(a, jobsexec.RouteComplete, jobsexec.CompleteRequest{ClaimRef: ref(a, one, one.Attempt), Result: json.RawMessage(`2`)})
	if !first.Applied || first.Replayed || !again.Applied || !again.Replayed {
		t.Errorf("first %+v, retry %+v; want applied, then applied+replayed", first, again)
	}
	if res := f.job(j1.JobID).Result; string(res) != "1" {
		t.Errorf("result = %s; the retry must not overwrite it", res)
	}
	// A failure can't be reported for a job that already succeeded.
	if r := call(a, jobsexec.RouteFail, jobsexec.FailRequest{ClaimRef: ref(a, one, one.Attempt), Error: "late"}); r.Applied {
		t.Error("a failure was applied to a succeeded job")
	}

	// fail is retry-safe too: it leaves the job pending, and a repeat says so.
	f1 := call(a, jobsexec.RouteFail, jobsexec.FailRequest{ClaimRef: ref(a, two, two.Attempt), Error: "boom"})
	f2 := call(a, jobsexec.RouteFail, jobsexec.FailRequest{ClaimRef: ref(a, two, two.Attempt), Error: "boom"})
	if !f1.Applied || f1.Replayed || !f2.Applied || !f2.Replayed || f2.State != string(structure.StatePending) {
		t.Errorf("fail %+v then %+v", f1, f2)
	}
	if j := f.job(j2.JobID); j.Attempt != 1 {
		t.Errorf("the repeated fail spent another attempt: %d", j.Attempt)
	}
}

func TestACancelRequestStopsTheHandlerAndCancelsTheJob(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	f.grantOn(n.principal.PrincipalID, gatehouseFacade.PermissionAssumeExecute, gatehouseFacade.ContextTypePrincipal, f.owner.PrincipalID.String())
	job := f.submit(f.owner.PrincipalID, structure.AuthorityOwner, nil, "r1")
	e := f.executor(n, jobsexec.Config{})
	started := make(chan struct{})
	cause := make(chan error, 1)
	_ = e.Handle(taskKey, func(ctx context.Context, _ jobsexec.Job) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		cause <- context.Cause(ctx)
		return nil, ctx.Err()
	})
	if _, err := e.PullOnce(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	<-started
	if res, err := jobsauth.Cancel(ctxT(t), f.d, f.owner.PrincipalID, job.JobID); err != nil {
		t.Fatalf("Cancel: %v (%+v)", err, res)
	}
	select {
	case c := <-cause:
		if !errors.Is(c, jobsexec.ErrCancelRequested) {
			t.Errorf("handler context cause = %v, want ErrCancelRequested", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was never told to stop")
	}
	f.waitState(job.JobID, structure.StateCancelled)
}

func TestALostClaimStopsTheHandlerAndReportsNothing(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	job := f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	e := f.executor(n, jobsexec.Config{})
	started := make(chan struct{})
	cause := make(chan error, 1)
	_ = e.Handle(taskKey, func(ctx context.Context, _ jobsexec.Job) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		cause <- context.Cause(ctx)
		return json.RawMessage(`"too late"`), nil // a handler that finishes anyway
	})
	if _, err := e.PullOnce(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	<-started
	// The claim is taken away under the executor's feet.
	if res, err := f.d.JobsWriter.ReleaseJob(ctxT(t), job.JobID, 1, 0); err != nil || !res.Applied {
		t.Fatalf("ReleaseJob: %+v %v", res, err)
	}
	select {
	case c := <-cause:
		if !errors.Is(c, jobsexec.ErrClaimLost) {
			t.Errorf("cause = %v, want ErrClaimLost", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was never told the claim was lost")
	}
	time.Sleep(200 * time.Millisecond) // time for a wrong report to land
	if j := f.job(job.JobID); j.State != structure.StatePending || len(j.Result) != 0 {
		t.Errorf("job after the lost claim: state %q result %s; the stale executor must report nothing", j.State, j.Result)
	}
}

func TestShuttingDownHandsClaimsBackWithoutSpendingAnAttempt(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	job := f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, "r1")
	e := f.executor(n, jobsexec.Config{})
	started := make(chan struct{})
	_ = e.Handle(taskKey, func(ctx context.Context, _ jobsexec.Job) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	runCtx, stop := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	go func() { ran <- e.Run(runCtx) }()
	<-started
	stop()
	select {
	case err := <-ran:
		if err != nil {
			t.Errorf("Run = %v on a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after shutdown")
	}
	j := f.job(job.JobID)
	if j.State != structure.StatePending || j.ClaimedBy != nil {
		t.Errorf("after shutdown: state %q claimedBy %v; want the claim handed back", j.State, j.ClaimedBy)
	}
}

func TestRunStopsWhenTheDirectorRefusesTheExecutor(t *testing.T) {
	f := setup(t)
	n := f.node("exec.noexec", nodeOpts{skipExecute: true})
	e := f.executor(n, jobsexec.Config{})
	_ = e.Handle(taskKey, func(context.Context, jobsexec.Job) (json.RawMessage, error) { return nil, nil })
	done := make(chan error, 1)
	go func() { done <- e.Run(ctxT(t)) }()
	select {
	case err := <-done:
		remoteCode(t, errors.Unwrap(err), wire.ErrUnauthorized)
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept retrying a refusal that waiting cannot fix")
	}
}

func TestPullsNeverExceedTheExecutorsFreeCapacity(t *testing.T) {
	f := setup(t)
	n := f.node("exec.one", nodeOpts{})
	var jobs []structure.Job
	for _, r := range []string{"r1", "r2", "r3"} {
		jobs = append(jobs, f.submit(f.owner.PrincipalID, structure.AuthorityService, nil, r))
	}
	e := f.executor(n, jobsexec.Config{MaxConcurrent: 2})
	release := make(chan struct{})
	_ = e.Handle(taskKey, func(ctx context.Context, _ jobsexec.Job) (json.RawMessage, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	if started, err := e.PullOnce(ctxT(t)); err != nil || started != 2 {
		t.Fatalf("first pull = %d, %v; want 2", started, err)
	}
	if started, err := e.PullOnce(ctxT(t)); err != nil || started != 0 || e.InFlight() != 2 {
		t.Fatalf("second pull = %d, %v with %d in flight; want 0 while full", started, err, e.InFlight())
	}
	pending := 0
	for _, j := range jobs {
		if f.job(j.JobID).State == structure.StatePending {
			pending++
		}
	}
	if pending != 1 {
		t.Errorf("%d jobs still pending, want exactly the one not claimed", pending)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for e.InFlight() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if started, err := e.PullOnce(ctxT(t)); err != nil || started != 1 {
		t.Fatalf("pull once capacity returned = %d, %v; want the third job", started, err)
	}
}

func TestEndpointsListShowsTheProtocolOnlyToExecutors(t *testing.T) {
	f := setup(t)
	if err := f.reg.HandleEndpointsList(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	exec := f.node("exec.one", nodeOpts{})
	plain := f.node("exec.plain", nodeOpts{skipExecute: true, skipClaim: true})
	keys := func(n *node) map[string]bool {
		infos, err := routerauth.ListEndpoints(ctxT(t), n.peer)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, i := range infos {
			m[i.Key] = true
		}
		return m
	}
	if got := keys(exec); len(got) != 7 || !got[jobsexec.RoutePull] || !got[jobsexec.RouteAuthorize] {
		t.Errorf("an executor sees %v; want the six routes and endpoints.list", got)
	}
	if got := keys(plain); len(got) != 1 || !got[routerauth.EndpointsListRoute] {
		t.Errorf("a peer without jobs.execute sees %v; want endpoints.list only", got)
	}
}
