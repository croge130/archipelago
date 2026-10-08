// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. They run the facade through dbstore's
// real reader and writer, proving the interfaces are satisfied by the
// concrete implementation.
package facade

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/jobs/storage/dbstore"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

func setup(t *testing.T) (Reader, Writer) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping jobs facade integration test")
	}
	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	migrations, err := dbstore.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
	}
	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE jobs_jobs, jobs_task_definitions CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx())
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return c
}

func renderDef() structure.TaskDefinition {
	return structure.TaskDefinition{
		TaskKey: "myapp.report.render", DefaultQueueKey: "myapp.reports",
		Params: []structure.ParamSpec{
			{Name: "report_id", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
			{Name: "copies", Definition: typedvalue.Definition{Storage: typedvalue.StorageInt}},
		},
		Idempotent: true, DefaultMaxAttempts: 4, DefaultAttemptTimeout: 30 * time.Second,
		PriorityCap: structure.PriorityImportant,
	}
}

func register(t *testing.T, r Reader, w Writer, d structure.TaskDefinition) {
	t.Helper()
	if err := RegisterTaskDefinition(ctxT(t), r, w, d, RegisterTaskOptions{}); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}
}

func TestRegisterTaskDefinitionIsIdempotentAndDetectsConflict(t *testing.T) {
	r, w := setup(t)
	d := renderDef()
	register(t, r, w, d)
	register(t, r, w, d) // identical: a no-op, including nil lists vs stored empty ones

	changed := d
	changed.Idempotent = false
	if err := RegisterTaskDefinition(ctxT(t), r, w, changed, RegisterTaskOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("a different definition under the same key: err = %v, want ErrConflict", err)
	}
	bad := d
	bad.TaskKey = "Not A Key"
	if err := RegisterTaskDefinition(ctxT(t), r, w, bad, RegisterTaskOptions{}); err == nil {
		t.Error("an invalid definition was registered")
	}
}

func TestRegisterTaskDefinitionReservedNamespaces(t *testing.T) {
	r, w := setup(t)
	d := renderDef()
	d.TaskKey = "jobs.builtin.cleanup"
	if err := RegisterTaskDefinition(ctxT(t), r, w, d, RegisterTaskOptions{}); err == nil {
		t.Error("a task in a reserved namespace was registered by default")
	}
	if err := RegisterTaskDefinition(ctxT(t), r, w, d, RegisterTaskOptions{AllowReservedNamespace: true}); err != nil {
		t.Errorf("the explicit override should allow it: %v", err)
	}
	// An integration's own list replaces the default.
	e := renderDef()
	e.TaskKey = "vitals.retention.sweep"
	if err := RegisterTaskDefinition(ctxT(t), r, w, e, RegisterTaskOptions{ReservedNamespaces: []string{"vitals"}}); err == nil {
		t.Error("a caller-supplied reserved namespace was not enforced")
	}
}

func TestSubmitAppliesDefinitionDefaultsAndFixesTheHash(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef())
	owner := uuid.New()

	res, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":" r1 ","copies":2.0}`), RequestedBy: owner})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	j := res.Job
	if !res.Created || j.QueueKey != "myapp.reports" || j.State != structure.StatePending ||
		!j.Retryable || j.MaxAttempts != 4 || j.AttemptTimeout != 30*time.Second ||
		j.Backoff != structure.DefaultBackoff || j.Priority != structure.PriorityNormal ||
		j.AuthorityMode != structure.AuthorityOwner || j.RequestedBy != owner {
		t.Errorf("definition defaults not applied: %+v", j)
	}
	if string(j.Params) != `{"copies": 2, "report_id": "r1"}` || j.ParamsHash == "" {
		t.Errorf("params not canonicalized: %s hash=%q", j.Params, j.ParamsHash)
	}
	if j.RunAt.IsZero() || j.CreatedAt.IsZero() {
		t.Error("database-stamped times were not read back")
	}
}

func TestSubmitNonIdempotentKindHasExactlyOneAttempt(t *testing.T) {
	r, w := setup(t)
	d := renderDef()
	d.TaskKey = "myapp.report.email"
	d.Idempotent = false
	d.DefaultMaxAttempts = 5 // ignored for a non-idempotent kind
	register(t, r, w, d)
	res, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: d.TaskKey, Params: json.RawMessage(`{"report_id":"r1"}`), RequestedBy: uuid.New()})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.Job.Retryable || res.Job.MaxAttempts != 1 {
		t.Errorf("a non-idempotent kind must not be retried: %+v", res.Job)
	}
}

func TestSubmitClampsPriorityToTheCap(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef()) // cap: important
	res, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r1"}`), RequestedBy: uuid.New(), Priority: structure.PriorityCritical})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.Job.Priority != structure.PriorityImportant {
		t.Errorf("priority = %s, want it clamped to the cap, important", res.Job.Priority)
	}
	lower, _ := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r2"}`), RequestedBy: uuid.New(), Priority: structure.PriorityBackground})
	if lower.Job.Priority != structure.PriorityBackground {
		t.Errorf("a priority below the cap should be kept, got %s", lower.Job.Priority)
	}
	if _, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r3"}`), RequestedBy: uuid.New(), Priority: "urgent"}); err == nil {
		t.Error("an invalid priority was accepted")
	}
}

func TestSubmitRejections(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef())
	owner := uuid.New()
	good := json.RawMessage(`{"report_id":"r1"}`)

	cases := map[string]SubmitRequest{
		"unknown kind":          {TaskKey: "no.such.kind", Params: good, RequestedBy: owner},
		"unknown parameter":     {TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r1","cmd":"rm -rf /"}`), RequestedBy: owner},
		"missing parameter":     {TaskKey: "myapp.report.render", Params: json.RawMessage(`{}`), RequestedBy: owner},
		"no owner":              {TaskKey: "myapp.report.render", Params: good},
		"assumed without RunAs": {TaskKey: "myapp.report.render", Params: good, RequestedBy: owner, AuthorityMode: structure.AuthorityAssumed},
		"negative delay":        {TaskKey: "myapp.report.render", Params: good, RequestedBy: owner, RunAfter: -time.Second},
	}
	for name, req := range cases {
		if _, err := Submit(ctxT(t), r, w, req); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	if _, err := Submit(ctxT(t), r, w, cases["unknown kind"]); !errors.Is(err, ErrUnknownTask) {
		t.Errorf("unknown kind: err = %v, want ErrUnknownTask", err)
	}
	if jobs, _ := r.ListJobs(ctxT(t), structure.ListFilter{}); len(jobs) != 0 {
		t.Errorf("%d jobs were written by rejected submissions", len(jobs))
	}
}

func TestSubmitIdempotencyKey(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef())
	owner := uuid.New()
	req := SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r1"}`), RequestedBy: owner, IdempotencyKey: "render:r1"}

	first, err := Submit(ctxT(t), r, w, req)
	if err != nil || !first.Created {
		t.Fatalf("first: %+v err=%v", first, err)
	}
	// A repeat — even spelled differently — returns the same job.
	req.Params = json.RawMessage(`{"report_id":"  r1  "}`)
	again, err := Submit(ctxT(t), r, w, req)
	if err != nil || again.Created || again.Job.JobID != first.Job.JobID {
		t.Fatalf("repeat: %+v err=%v, want the same job and Created=false", again, err)
	}
	// The same key with different parameters is a conflict, not a silent reuse.
	req.Params = json.RawMessage(`{"report_id":"r2"}`)
	if _, err := Submit(ctxT(t), r, w, req); !errors.Is(err, ErrConflict) {
		t.Errorf("same key, different params: err = %v, want ErrConflict", err)
	}
}

func TestSubmitRunAfterDelaysTheClaim(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef())
	res, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r1"}`), RequestedBy: uuid.New(), RunAfter: time.Hour})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !res.Job.RunAt.After(res.Job.CreatedAt.Add(50 * time.Minute)) {
		t.Errorf("RunAt %v should be about an hour after CreatedAt %v", res.Job.RunAt, res.Job.CreatedAt)
	}
	got, err := w.ClaimJobs(ctxT(t), structure.ClaimRequest{ClaimerInstanceID: uuid.New(), TaskKeys: []string{"myapp.report.render"}, Limit: 5, ClaimTTL: time.Minute})
	if err != nil || len(got) != 0 {
		t.Errorf("a delayed job was claimable early: %d err=%v", len(got), err)
	}
}

func TestFailAttemptUsesTheJobsBackoffAndTerminalNeverRetries(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef())
	submit := func(id string) structure.Job {
		res, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"` + id + `"}`), RequestedBy: uuid.New()})
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		return res.Job
	}
	a, b := submit("a"), submit("b")
	claimed, err := w.ClaimJobs(ctxT(t), structure.ClaimRequest{ClaimerInstanceID: uuid.New(), TaskKeys: []string{"myapp.report.render"}, Limit: 10, ClaimTTL: time.Minute})
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim: %d err=%v", len(claimed), err)
	}
	byID := map[uuid.UUID]structure.Job{}
	for _, j := range claimed {
		byID[j.JobID] = j
	}

	res, err := FailAttempt(ctxT(t), w, byID[a.JobID], errors.New("transient"), false)
	if err != nil || !res.Applied || res.State != structure.StatePending {
		t.Fatalf("retryable failure: %+v err=%v, want pending", res, err)
	}
	stored, _, _ := r.GetJob(ctxT(t), a.JobID)
	if stored.LastError != "transient" || !stored.RunAt.After(stored.UpdatedAt.Add(2*time.Second)) {
		t.Errorf("the retry should be delayed by the job's backoff (5s base): %+v", stored)
	}

	res, err = FailAttempt(ctxT(t), w, byID[b.JobID], errors.New("permission denied"), true)
	if err != nil || !res.Applied || res.State != structure.StateDead {
		t.Fatalf("terminal failure: %+v err=%v, want dead — an authority denial is never retried", res, err)
	}
}

func TestCancelDistinguishesUnknownFromFinished(t *testing.T) {
	r, w := setup(t)
	register(t, r, w, renderDef())
	res, err := Submit(ctxT(t), r, w, SubmitRequest{TaskKey: "myapp.report.render", Params: json.RawMessage(`{"report_id":"r1"}`), RequestedBy: uuid.New(), RunAfter: time.Hour})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	out, err := Cancel(ctxT(t), r, w, res.Job.JobID)
	if err != nil || !out.Applied || out.State != structure.StateCancelled {
		t.Fatalf("cancel: %+v err=%v", out, err)
	}
	if _, err := Cancel(ctxT(t), r, w, res.Job.JobID); !errors.Is(err, ErrJobFinished) {
		t.Errorf("cancelling a finished job: err = %v, want ErrJobFinished", err)
	}
	if _, err := Cancel(ctxT(t), r, w, uuid.New()); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("cancelling an unknown job: err = %v, want ErrJobNotFound", err)
	}
}
