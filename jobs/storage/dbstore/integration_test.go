// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. They exercise what unit tests cannot:
// that the single-statement SQL decides claims, fences and failures the
// way evaluation's pure rules say it should, including under
// concurrency.
package dbstore

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/jobs/evaluation"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

type fixture struct {
	r    *PostgresReader
	w    *PostgresWriter
	pool *archidb.Pool
}

func setup(t *testing.T) fixture {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping jobs dbstore integration test")
	}
	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
	}
	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE jobs_jobs, jobs_task_definitions CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return fixture{r: NewPostgresReader(pool.Pgx()), w: NewPostgresWriter(pool.Pgx()), pool: pool}
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return c
}

func (f fixture) def(t *testing.T, key string, idempotent bool) structure.TaskDefinition {
	t.Helper()
	d := structure.TaskDefinition{
		TaskKey: key, DefaultQueueKey: "q.default", Idempotent: idempotent,
		DefaultMaxAttempts: 3, DefaultAttemptTimeout: time.Minute, PriorityCap: structure.PriorityCritical,
	}
	if err := f.w.RegisterTaskDefinition(ctxT(t), d); err != nil {
		t.Fatalf("RegisterTaskDefinition(%s): %v", key, err)
	}
	return d
}

type jobOpt func(*structure.Job)

func (f fixture) enqueue(t *testing.T, taskKey string, runAfter, expiresAfter time.Duration, opts ...jobOpt) structure.Job {
	t.Helper()
	j := structure.Job{
		JobID: uuid.New(), TaskKey: taskKey, QueueKey: "q.default",
		Params: json.RawMessage(`{}`), ParamsHash: "h", State: structure.StatePending, Priority: structure.PriorityNormal,
		Retryable: true, MaxAttempts: 3, AttemptTimeout: time.Minute, Backoff: structure.DefaultBackoff,
		RequestedBy: uuid.New(), AuthorityMode: structure.AuthorityOwner,
	}
	for _, o := range opts {
		o(&j)
	}
	created, err := f.w.EnqueueJob(ctxT(t), j, runAfter, expiresAfter)
	if err != nil || !created {
		t.Fatalf("EnqueueJob: created=%v err=%v", created, err)
	}
	return j
}

func (f fixture) get(t *testing.T, id uuid.UUID) structure.Job {
	t.Helper()
	j, found, err := f.r.GetJob(ctxT(t), id)
	if err != nil || !found {
		t.Fatalf("GetJob: found=%v err=%v", found, err)
	}
	return j
}

func claimReq(kinds ...string) structure.ClaimRequest {
	return structure.ClaimRequest{ClaimerInstanceID: uuid.New(), TaskKeys: kinds, Limit: 10, ClaimTTL: time.Minute}
}

// lapse makes the current claim on id expire by the database's clock,
// without waiting: the tests are about what the store does after a
// lapse, not about how long it takes.
func (f fixture) lapse(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Pgx().Exec(ctxT(t), `UPDATE jobs_jobs SET claimed_until = now() - interval '1 second' WHERE job_id = $1`, id.String()); err != nil {
		t.Fatalf("lapse: %v", err)
	}
}

func TestTaskDefinitionRoundTripsThroughStorage(t *testing.T) {
	f := setup(t)
	in := structure.TaskDefinition{
		TaskKey: "myapp.report.render", Description: "render", DefaultQueueKey: "myapp.reports",
		Params: []structure.ParamSpec{
			{Name: "report_id", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
			{Name: "timeout", Definition: typedvalue.Definition{Storage: typedvalue.StorageInt, Semantic: typedvalue.SemanticDuration, UnitName: "second"}},
		},
		Scope:      []structure.ScopeEntry{{PermissionKey: "myapp.report.read", ContextType: "myapp.tenant", ContextIDParam: "report_id"}},
		Idempotent: true, DefaultMaxAttempts: 4, DefaultAttemptTimeout: 90 * time.Second,
		DefaultBackoff: structure.BackoffPolicy{Kind: structure.BackoffFixed, Base: 2 * time.Second},
		PriorityCap:    structure.PriorityImportant, Metadata: json.RawMessage(`{"owner":"team-a"}`),
	}
	if err := f.w.RegisterTaskDefinition(ctxT(t), in); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
	out, found, err := f.r.GetTaskDefinition(ctxT(t), in.TaskKey)
	if err != nil || !found {
		t.Fatalf("GetTaskDefinition: found=%v err=%v", found, err)
	}
	if !evaluation.SameTaskDefinition(in, out) {
		t.Errorf("definition changed through storage:\n in:  %+v\n out: %+v", in, out)
	}
	if out.DefaultAttemptTimeout != 90*time.Second || out.PriorityCap != structure.PriorityImportant {
		t.Errorf("scalar fields did not survive: %+v", out)
	}
	all, err := f.r.ListTaskDefinitions(ctxT(t))
	if err != nil || len(all) != 1 {
		t.Fatalf("ListTaskDefinitions: %d err=%v", len(all), err)
	}
	if _, found, _ := f.r.GetTaskDefinition(ctxT(t), "no.such.task"); found {
		t.Error("an unknown task definition was found")
	}
}

func TestSameTaskDefinitionSurvivesNilVersusEmpty(t *testing.T) {
	f := setup(t)
	in := f.def(t, "myapp.minimal", false) // nil Params, nil Scope, nil Metadata
	out, _, err := f.r.GetTaskDefinition(ctxT(t), "myapp.minimal")
	if err != nil {
		t.Fatalf("GetTaskDefinition: %v", err)
	}
	if !evaluation.SameTaskDefinition(in, out) {
		t.Error("a freshly built definition (nil lists) must equal the same definition read back (empty lists)")
	}
}

func TestEnqueueRoundTripAndIdempotencyKey(t *testing.T) {
	f := setup(t)
	f.def(t, "t.one", true)
	sc := logging.NewRootSpan()
	runAs := uuid.New()
	j := f.enqueue(t, "t.one", 0, time.Hour, func(j *structure.Job) {
		j.IdempotencyKey = "k1"
		j.TraceContext = &sc
		j.AuthorityMode = structure.AuthorityAssumed
		j.RunAs = &runAs
		j.Priority = structure.PriorityImportant
		j.AuthoritySourceRef = "schedule:nightly"
	})

	got := f.get(t, j.JobID)
	if got.State != structure.StatePending || got.Attempt != 0 || got.Priority != structure.PriorityImportant ||
		got.AuthorityMode != structure.AuthorityAssumed || got.RunAs == nil || *got.RunAs != runAs ||
		got.AuthoritySourceRef != "schedule:nightly" || got.TraceContext == nil || got.TraceContext.TraceID != sc.TraceID {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.After(got.CreatedAt) {
		t.Errorf("ExpiresAt should be after CreatedAt: %v %v", got.ExpiresAt, got.CreatedAt)
	}
	if got.Backoff != structure.DefaultBackoff {
		t.Errorf("backoff did not round trip: %+v", got.Backoff)
	}

	// A second enqueue under the same key writes nothing.
	dup := j
	dup.JobID = uuid.New()
	created, err := f.w.EnqueueJob(ctxT(t), dup, 0, 0)
	if err != nil || created {
		t.Fatalf("duplicate enqueue: created=%v err=%v; want created=false", created, err)
	}
	byKey, found, err := f.r.GetJobByIdempotencyKey(ctxT(t), "t.one", "k1")
	if err != nil || !found || byKey.JobID != j.JobID {
		t.Fatalf("GetJobByIdempotencyKey: %+v found=%v err=%v", byKey, found, err)
	}
	// Different task, same key, is a different job; no key never collides.
	f.def(t, "t.two", true)
	f.enqueue(t, "t.two", 0, 0, func(j *structure.Job) { j.IdempotencyKey = "k1" })
	f.enqueue(t, "t.one", 0, 0)
	f.enqueue(t, "t.one", 0, 0)
}

func TestClaimOnlyTakesDueJobsOfTheRequestedKinds(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	f.def(t, "t.b", true)
	due := f.enqueue(t, "t.a", 0, 0)
	f.enqueue(t, "t.a", time.Hour, 0)                   // not due yet
	f.enqueue(t, "t.b", 0, 0)                           // wrong kind
	expired := f.enqueue(t, "t.a", 0, time.Millisecond) // expires almost at once
	time.Sleep(30 * time.Millisecond)

	got, err := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if err != nil {
		t.Fatalf("ClaimJobs: %v", err)
	}
	if len(got) != 1 || got[0].JobID != due.JobID {
		t.Fatalf("claimed %d jobs, want exactly the one due t.a job: %+v", len(got), got)
	}
	c := got[0]
	if c.State != structure.StateClaimed || c.Attempt != 1 || c.ClaimedBy == nil || c.ClaimedUntil == nil || !c.ClaimedUntil.After(c.UpdatedAt) {
		t.Errorf("claim did not stamp the job: %+v", c)
	}
	if f.get(t, expired.JobID).State != structure.StatePending {
		t.Error("an expired pending job must not be claimed")
	}
}

func TestClaimRequiresKindsLimitAndTTL(t *testing.T) {
	f := setup(t)
	if _, err := f.w.ClaimJobs(ctxT(t), structure.ClaimRequest{ClaimerInstanceID: uuid.New(), Limit: 1, ClaimTTL: time.Minute}); err == nil {
		t.Error("a claim with no TaskKeys was accepted: a node claims only what it can run")
	}
	if _, err := f.w.ClaimJobs(ctxT(t), structure.ClaimRequest{ClaimerInstanceID: uuid.New(), TaskKeys: []string{"t"}, ClaimTTL: time.Minute}); err == nil {
		t.Error("a claim with no limit was accepted")
	}
	if _, err := f.w.ClaimJobs(ctxT(t), structure.ClaimRequest{ClaimerInstanceID: uuid.New(), TaskKeys: []string{"t"}, Limit: 1}); err == nil {
		t.Error("a claim with no TTL was accepted")
	}
}

func TestClaimOrdersByPriorityThenRunAtAndHonorsLimitQueueAndTarget(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	low := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.Priority = structure.PriorityBackground })
	high := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.Priority = structure.PriorityCritical })
	mid := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.Priority = structure.PriorityImportant })
	otherQueue := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.QueueKey = "q.other"; j.Priority = structure.PriorityCritical })
	me := uuid.New()
	targeted := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.TargetInstanceID = &me; j.Priority = structure.PriorityBackground })

	req := claimReq("t.a")
	req.QueueKeys = []string{"q.default"}
	req.Limit = 2
	got, err := f.w.ClaimJobs(ctxT(t), req)
	if err != nil {
		t.Fatalf("ClaimJobs: %v", err)
	}
	if len(got) != 2 || got[0].JobID != high.JobID || got[1].JobID != mid.JobID {
		t.Fatalf("want the critical then the important job, got %+v", got)
	}
	// The other queue's job and the job aimed at someone else stay pending.
	if f.get(t, otherQueue.JobID).State != structure.StatePending {
		t.Error("a job in an unrequested queue was claimed")
	}
	if f.get(t, targeted.JobID).State != structure.StatePending {
		t.Error("a job aimed at another instance was claimed by a stranger")
	}
	// The targeted instance can claim its own job.
	mine := claimReq("t.a")
	mine.ClaimerInstanceID = me
	mine.QueueKeys = []string{"q.default"}
	mineGot, err := f.w.ClaimJobs(ctxT(t), mine)
	if err != nil {
		t.Fatalf("ClaimJobs (targeted): %v", err)
	}
	found := map[uuid.UUID]bool{}
	for _, j := range mineGot {
		found[j.JobID] = true
	}
	if !found[targeted.JobID] || !found[low.JobID] {
		t.Errorf("the targeted instance should get its job and the remaining low one: %+v", mineGot)
	}
}

func TestConcurrentClaimersNeverShareAJob(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	const jobs, claimers = 40, 8
	for i := 0; i < jobs; i++ {
		f.enqueue(t, "t.a", 0, 0)
	}
	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for c := 0; c < claimers; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				req := claimReq("t.a")
				req.Limit = 3
				got, err := f.w.ClaimJobs(context.Background(), req)
				if err != nil {
					t.Errorf("ClaimJobs: %v", err)
					return
				}
				if len(got) == 0 {
					return
				}
				mu.Lock()
				for _, j := range got {
					seen[j.JobID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != jobs {
		t.Errorf("%d distinct jobs claimed, want %d", len(seen), jobs)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s was claimed %d times", id, n)
		}
	}
}

func TestLapsedClaimIsReclaimedOnlyWhenRetryableAndNotCancelled(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)

	retry := f.enqueue(t, "t.a", 0, 0)
	noRetry := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.Retryable = false; j.MaxAttempts = 1 })
	cancelled := f.enqueue(t, "t.a", 0, 0)
	exhausted := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.MaxAttempts = 1 })

	got, err := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if err != nil || len(got) != 4 {
		t.Fatalf("first claim: %d jobs err=%v, want 4", len(got), err)
	}
	if _, err := f.w.CancelJob(ctxT(t), cancelled.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	for _, j := range []structure.Job{retry, noRetry, cancelled, exhausted} {
		f.lapse(t, j.JobID)
	}

	again, err := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 1 || again[0].JobID != retry.JobID || again[0].Attempt != 2 {
		t.Fatalf("only the retryable job with attempts left may be reclaimed (attempt 2), got %+v", again)
	}

	reaped, err := f.w.ReapJobs(ctxT(t))
	if err != nil {
		t.Fatalf("ReapJobs: %v", err)
	}
	if reaped.LapsedDead != 2 || reaped.LapsedCancelled != 1 {
		t.Errorf("reap = %+v, want 2 dead (no retry, out of attempts) and 1 cancelled", reaped)
	}
	if s := f.get(t, noRetry.JobID).State; s != structure.StateDead {
		t.Errorf("non-retryable lapsed job is %s, want dead", s)
	}
	if s := f.get(t, exhausted.JobID).State; s != structure.StateDead {
		t.Errorf("out-of-attempts lapsed job is %s, want dead", s)
	}
	if s := f.get(t, cancelled.JobID).State; s != structure.StateCancelled {
		t.Errorf("cancelled lapsed job is %s, want cancelled", s)
	}
	// Reaping again changes nothing.
	again2, err := f.w.ReapJobs(ctxT(t))
	if err != nil || again2 != (structure.ReapResult{}) {
		t.Errorf("second reap = %+v err=%v, want nothing", again2, err)
	}
}

func TestStaleClaimantIsFencedOut(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	j := f.enqueue(t, "t.a", 0, 0)

	first, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if len(first) != 1 {
		t.Fatal("expected a claim")
	}
	f.lapse(t, j.JobID)
	second, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if len(second) != 1 || second[0].Attempt != 2 {
		t.Fatalf("expected the lapsed job to be reclaimed as attempt 2, got %+v", second)
	}

	// The first claimant, still holding attempt 1, is refused everywhere.
	if hb, err := f.w.HeartbeatJob(ctxT(t), j.JobID, 1, time.Minute); err != nil || hb.OK {
		t.Errorf("stale heartbeat: %+v err=%v, want refused", hb, err)
	}
	if ok, err := f.w.CompleteJob(ctxT(t), j.JobID, 1, nil); err != nil || ok {
		t.Errorf("stale complete: ok=%v err=%v, want refused", ok, err)
	}
	if res, err := f.w.FailJob(ctxT(t), structure.FailRequest{JobID: j.JobID, Attempt: 1}); err != nil || res.Applied {
		t.Errorf("stale fail: %+v err=%v, want refused", res, err)
	}
	sid := uuid.New()
	if ok, err := f.w.AttachClaimIdentity(ctxT(t), j.JobID, 1, &sid); err != nil || ok {
		t.Errorf("stale AttachClaimIdentity: ok=%v err=%v, want refused", ok, err)
	}
	// The live claimant succeeds.
	if ok, err := f.w.CompleteJob(ctxT(t), j.JobID, 2, json.RawMessage(`{"done":true}`)); err != nil || !ok {
		t.Fatalf("live complete: ok=%v err=%v", ok, err)
	}
	done := f.get(t, j.JobID)
	if done.State != structure.StateSucceeded || done.FinishedAt == nil || string(done.Result) != `{"done": true}` {
		t.Errorf("completed job: %+v (result %s)", done, done.Result)
	}
	// A finished job cannot be completed, failed or heartbeaten again.
	if ok, _ := f.w.CompleteJob(ctxT(t), j.JobID, 2, nil); ok {
		t.Error("a finished job was completed twice")
	}
}

func TestHeartbeatExtendsTheClaimAndSurfacesCancelRequests(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	j := f.enqueue(t, "t.a", 0, 0)
	claimed, _ := f.w.ClaimJobs(ctxT(t), structure.ClaimRequest{ClaimerInstanceID: uuid.New(), TaskKeys: []string{"t.a"}, Limit: 1, ClaimTTL: time.Second})
	before := *claimed[0].ClaimedUntil

	hb, err := f.w.HeartbeatJob(ctxT(t), j.JobID, 1, time.Hour)
	if err != nil || !hb.OK || hb.CancelRequested {
		t.Fatalf("heartbeat: %+v err=%v", hb, err)
	}
	if !f.get(t, j.JobID).ClaimedUntil.After(before.Add(time.Minute)) {
		t.Error("heartbeat did not extend the claim")
	}
	res, err := f.w.CancelJob(ctxT(t), j.JobID)
	if err != nil || !res.Applied || res.State != structure.StateClaimed {
		t.Fatalf("cancel of a claimed job: %+v err=%v, want applied and still claimed", res, err)
	}
	hb, _ = f.w.HeartbeatJob(ctxT(t), j.JobID, 1, time.Minute)
	if !hb.OK || !hb.CancelRequested {
		t.Errorf("heartbeat after cancel: %+v, want OK with CancelRequested", hb)
	}
}

func TestFailJobMatchesEvaluationAfterFailureForEveryCombination(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	for _, retryable := range []bool{true, false} {
		for _, max := range []int{1, 3} {
			if !retryable && max != 1 {
				continue // structure.Job.Validate: a non-retryable job has one attempt
			}
			for attempt := 1; attempt <= max; attempt++ {
				for _, terminal := range []bool{false, true} {
					for _, cancel := range []bool{false, true} {
						j := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.Retryable = retryable; j.MaxAttempts = max })
						req := claimReq("t.a")
						req.Limit = 100
						if got, err := f.w.ClaimJobs(ctxT(t), req); err != nil || len(got) != 1 {
							t.Fatalf("claim: %d err=%v", len(got), err)
						}
						// Put the job at the attempt number under test.
						if _, err := f.pool.Pgx().Exec(ctxT(t), `UPDATE jobs_jobs SET attempt = $2, cancel_requested = $3 WHERE job_id = $1`, j.JobID.String(), attempt, cancel); err != nil {
							t.Fatalf("set up: %v", err)
						}
						res, err := f.w.FailJob(ctxT(t), structure.FailRequest{JobID: j.JobID, Attempt: attempt, Error: "boom", Terminal: terminal, RetryDelay: time.Hour})
						want := evaluation.AfterFailure(retryable, attempt, max, terminal, cancel)
						if err != nil || !res.Applied || res.State != want {
							t.Errorf("retryable=%v attempt=%d/%d terminal=%v cancel=%v: SQL=%v applied=%v err=%v, evaluation says %s",
								retryable, attempt, max, terminal, cancel, res.State, res.Applied, err, want)
						}
						got := f.get(t, j.JobID)
						if want == structure.StatePending {
							if got.FinishedAt != nil || got.ClaimedBy != nil || got.LastError != "boom" || got.RunAt.Before(got.UpdatedAt.Add(30*time.Minute)) {
								t.Errorf("a retried job should be unclaimed, unfinished, and delayed: %+v", got)
							}
						} else if got.FinishedAt == nil {
							t.Errorf("a %s job should have FinishedAt set", want)
						}
					}
				}
			}
		}
	}
}

func TestRetriedJobWaitsForItsDelayThenRunsAgain(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	j := f.enqueue(t, "t.a", 0, 0)
	first, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if res, err := f.w.FailJob(ctxT(t), structure.FailRequest{JobID: j.JobID, Attempt: first[0].Attempt, Error: "x", RetryDelay: 150 * time.Millisecond}); err != nil || res.State != structure.StatePending {
		t.Fatalf("fail: %+v err=%v", res, err)
	}
	if got, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a")); len(got) != 0 {
		t.Fatal("a retried job was claimable before its backoff elapsed")
	}
	time.Sleep(250 * time.Millisecond)
	got, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if len(got) != 1 || got[0].Attempt != 2 {
		t.Fatalf("after the delay the job should run again as attempt 2, got %+v", got)
	}
}

func TestCancelJob(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	pending := f.enqueue(t, "t.a", time.Hour, 0)
	res, err := f.w.CancelJob(ctxT(t), pending.JobID)
	if err != nil || !res.Applied || res.State != structure.StateCancelled {
		t.Fatalf("cancel pending: %+v err=%v", res, err)
	}
	if got := f.get(t, pending.JobID); got.FinishedAt == nil {
		t.Error("a cancelled job should have FinishedAt set")
	}
	if res, _ := f.w.CancelJob(ctxT(t), pending.JobID); res.Applied {
		t.Error("cancelling an already-cancelled job was applied")
	}
	if res, _ := f.w.CancelJob(ctxT(t), uuid.New()); res.Applied {
		t.Error("cancelling an unknown job was applied")
	}
}

func TestReapExpiredPendingAndPrune(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	expiring := f.enqueue(t, "t.a", time.Hour, time.Millisecond)
	live := f.enqueue(t, "t.a", time.Hour, time.Hour)
	time.Sleep(30 * time.Millisecond)

	res, err := f.w.ReapJobs(ctxT(t))
	if err != nil || res.ExpiredPending != 1 {
		t.Fatalf("reap = %+v err=%v, want 1 expired pending", res, err)
	}
	if f.get(t, expiring.JobID).State != structure.StateDead || f.get(t, live.JobID).State != structure.StatePending {
		t.Error("only the expired pending job should have been moved")
	}

	// Prune: finished long ago is deleted; recently finished and
	// unfinished stay.
	if _, err := f.pool.Pgx().Exec(ctxT(t), `UPDATE jobs_jobs SET finished_at = now() - interval '2 days' WHERE job_id = $1`, expiring.JobID.String()); err != nil {
		t.Fatalf("age: %v", err)
	}
	recent := f.enqueue(t, "t.a", time.Hour, 0)
	if _, err := f.w.CancelJob(ctxT(t), recent.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	n, err := f.w.PruneFinishedJobs(ctxT(t), 24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("prune = %d err=%v, want 1", n, err)
	}
	if _, found, _ := f.r.GetJob(ctxT(t), expiring.JobID); found {
		t.Error("the old finished job survived pruning")
	}
	for _, id := range []uuid.UUID{live.JobID, recent.JobID} {
		if _, found, _ := f.r.GetJob(ctxT(t), id); !found {
			t.Errorf("job %s was pruned but should have stayed", id)
		}
	}
}

func TestAttachClaimIdentityAndListAndCount(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	owner := uuid.New()
	a := f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.RequestedBy = owner })
	f.enqueue(t, "t.a", 0, 0, func(j *structure.Job) { j.QueueKey = "q.other" })
	claimed, _ := f.w.ClaimJobs(ctxT(t), func() structure.ClaimRequest {
		r := claimReq("t.a")
		r.QueueKeys = []string{"q.default"}
		actor := uuid.New()
		r.ActorPrincipalID = &actor
		return r
	}())
	sid := uuid.New()
	if ok, err := f.w.AttachClaimIdentity(ctxT(t), a.JobID, claimed[0].Attempt, &sid); err != nil || !ok {
		t.Fatalf("AttachClaimIdentity: ok=%v err=%v", ok, err)
	}
	got := f.get(t, a.JobID)
	if got.AssumedSessionID == nil || *got.AssumedSessionID != sid || got.ActorPrincipalID == nil {
		t.Errorf("claim identity not recorded: %+v", got)
	}

	owned, err := f.r.ListJobs(ctxT(t), structure.ListFilter{RequestedBy: &owner})
	if err != nil || len(owned) != 1 || owned[0].JobID != a.JobID {
		t.Fatalf("ListJobs by owner: %+v err=%v", owned, err)
	}
	inQueue, _ := f.r.ListJobs(ctxT(t), structure.ListFilter{QueueKeys: []string{"q.other"}})
	if len(inQueue) != 1 {
		t.Errorf("ListJobs by queue: %d, want 1", len(inQueue))
	}
	claimedOnly, _ := f.r.ListJobs(ctxT(t), structure.ListFilter{States: []structure.State{structure.StateClaimed}})
	if len(claimedOnly) != 1 {
		t.Errorf("ListJobs by state: %d, want 1", len(claimedOnly))
	}
	counts, err := f.r.CountJobsByState(ctxT(t), "q.default")
	if err != nil || counts[structure.StateClaimed] != 1 || len(counts) != 1 {
		t.Errorf("CountJobsByState = %v err=%v, want one claimed job", counts, err)
	}
}

func TestReleaseJobGivesBackTheClaimWithoutSpendingAnAttempt(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	j := f.enqueue(t, "t.a", 0, 0)
	claimed, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if len(claimed) != 1 || claimed[0].Attempt != 1 {
		t.Fatalf("setup claim: %+v", claimed)
	}
	sid := uuid.New()
	_, _ = f.w.AttachClaimIdentity(ctxT(t), j.JobID, 1, &sid)

	// A stale attempt number is refused.
	if res, err := f.w.ReleaseJob(ctxT(t), j.JobID, 7, time.Second); err != nil || res.Applied {
		t.Fatalf("stale release: %+v err=%v, want refused", res, err)
	}
	res, err := f.w.ReleaseJob(ctxT(t), j.JobID, 1, time.Hour)
	if err != nil || !res.Applied || res.State != structure.StatePending {
		t.Fatalf("release: %+v err=%v", res, err)
	}
	got := f.get(t, j.JobID)
	if got.Attempt != 0 || got.State != structure.StatePending || got.ClaimedBy != nil || got.AssumedSessionID != nil || got.ActorPrincipalID != nil {
		t.Errorf("a released job should be back to attempt 0, unclaimed, with no identity: %+v", got)
	}
	if !got.RunAt.After(got.UpdatedAt.Add(30 * time.Minute)) {
		t.Errorf("the release delay was not applied: run_at %v", got.RunAt)
	}
	if again, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a")); len(again) != 0 {
		t.Error("a released job was claimable again before its delay")
	}
	// Releasing something that is not claimed does nothing.
	if res, _ := f.w.ReleaseJob(ctxT(t), j.JobID, 0, time.Second); res.Applied {
		t.Error("released a job that was not claimed")
	}
}

func TestReleaseJobHonorsAPendingCancel(t *testing.T) {
	f := setup(t)
	f.def(t, "t.a", true)
	j := f.enqueue(t, "t.a", 0, 0)
	claimed, _ := f.w.ClaimJobs(ctxT(t), claimReq("t.a"))
	if _, err := f.w.CancelJob(ctxT(t), j.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	res, err := f.w.ReleaseJob(ctxT(t), j.JobID, claimed[0].Attempt, time.Second)
	if err != nil || !res.Applied || res.State != structure.StateCancelled {
		t.Fatalf("release with a cancel pending: %+v err=%v, want cancelled", res, err)
	}
	if f.get(t, j.JobID).FinishedAt == nil {
		t.Error("a cancelled job should have FinishedAt set")
	}
}
