// A store-less executor running a job over real mTLS: it registers itself
// through the registry route, pulls from a director that holds the stores,
// asks the director before acting on an owner's behalf, and completes —
// everything over one authenticated websocket, nothing on the executor
// side touching a database. Skips without ARCHIPELAGO_TEST_DATABASE_URL.
package routere2e

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	jobsDB "github.com/croge130/archipelago/jobs/storage/dbstore"
	jobsStructure "github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/jobsauth"
	"github.com/croge130/archipelago/jobsdirector"
	"github.com/croge130/archipelago/jobsexec"
	"github.com/croge130/archipelago/registry"
	"github.com/croge130/archipelago/routerauth"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

func TestAStorelessExecutorRunsAnOwnersJobOverRealMTLS(t *testing.T) {
	n := startNode(t)
	ctx := context.Background()

	pool, err := archidb.Open(ctx, archidb.Config{DSN: os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrations, err := jobsDB.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.ProvisionSchemas(ctx, migrations); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Pgx().Exec(ctx, `TRUNCATE jobs_jobs, jobs_task_definitions CASCADE`); err != nil {
		t.Fatal(err)
	}
	deps := jobsauth.Deps{
		GatehouseReader: n.reader, GatehouseWriter: n.writer,
		JobsReader: jobsDB.NewPostgresReader(pool.Pgx()), JobsWriter: jobsDB.NewPostgresWriter(pool.Pgx()),
	}

	// The director side: permissions, a task kind, and the routes.
	for _, f := range []func() error{
		func() error { return facade.RegisterAssumePermissions(ctx, n.reader, n.writer) },
		func() error { return jobsauth.RegisterPermissions(ctx, n.reader, n.writer) },
		func() error { return jobsdirector.RegisterPermissions(ctx, n.reader, n.writer) },
	} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"myapp.report.read", "myapp.admin.all"} {
		if err := facade.RegisterPermission(ctx, n.reader, n.writer, structure.PermissionDefinition{
			PermissionKey: key, RequiredAuthorityLevel: structure.AuthorityLevelStandard,
		}, facade.RegisterPermissionOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	const taskKey, queue = "myapp.report.render", "myapp.reports"
	if err := jobsFacade.RegisterTaskDefinition(ctx, deps.JobsReader, deps.JobsWriter, jobsStructure.TaskDefinition{
		TaskKey: taskKey, DefaultQueueKey: queue,
		Params: []jobsStructure.ParamSpec{
			{Name: "tenant", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
		},
		Scope:      []jobsStructure.ScopeEntry{{PermissionKey: "myapp.report.read", ContextType: "myapp.tenant", ContextIDParam: "tenant"}},
		Idempotent: true, DefaultMaxAttempts: 2, DefaultAttemptTimeout: time.Minute, PriorityCap: jobsStructure.PriorityCritical,
	}, jobsFacade.RegisterTaskOptions{}); err != nil {
		t.Fatal(err)
	}
	dir, err := jobsdirector.New(jobsdirector.Config{Deps: deps, ClaimTTL: 5 * time.Second, Logger: quiet().Logger})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := routerauth.New(n.router, n.reader, n.writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.Register(ctx, reg); err != nil {
		t.Fatal(err)
	}

	// The owner and the executor, as principals.
	grantOn := func(holder uuid.UUID, perm, ctxType, ctxID string) {
		t.Helper()
		now := time.Now().Truncate(time.Microsecond)
		k, ct, ci := perm, ctxType, ctxID
		if err := n.writer.CreateGrant(ctx, structure.Grant{
			GrantID: uuid.New(), SubjectType: structure.GrantSubjectTypePrincipal, SubjectID: holder,
			TargetType: structure.GrantTargetTypePermission, PermissionKey: &k, Scope: structure.GrantScopeContext,
			ContextType: &ct, ContextID: &ci, Effect: structure.GrantEffectAllow, Status: structure.GrantStatusActive,
			Origin: structure.GrantOriginManual, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := facade.EnsurePrincipal(ctx, n.reader, n.writer, "user.owner", structure.PrincipalTypeUser)
	if err != nil {
		t.Fatal(err)
	}
	grantOn(owner.PrincipalID, jobsauth.PermissionSubmit, jobsauth.ContextTypeQueue, queue)
	grantOn(owner.PrincipalID, "myapp.report.read", "myapp.tenant", "acme")
	if _, err := facade.GrantPermission(ctx, n.writer, structure.GrantSubjectTypePrincipal, owner.PrincipalID, "myapp.admin.all"); err != nil {
		t.Fatal(err)
	}

	peer := n.client(t, "service.remote-exec", permRegister, jobsdirector.PermissionExecute)
	executorP, _, _ := n.reader.GetPrincipalByKey(ctx, "service.remote-exec")
	grantOn(executorP.PrincipalID, jobsauth.PermissionClaim, jobsauth.ContextTypeQueue, queue)
	grantOn(executorP.PrincipalID, facade.PermissionAssumeExecute, facade.ContextTypePrincipal, owner.PrincipalID.String())

	submitted, err := jobsauth.Submit(ctx, deps, jobsFacade.SubmitRequest{
		TaskKey: taskKey, Params: json.RawMessage(`{"tenant":"acme"}`), RequestedBy: owner.PrincipalID, AuthorityMode: jobsStructure.AuthorityOwner,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// The executor: a router connection and handlers, no store.
	inst, err := registry.Register(ctxT(t), peer, "workers", nil)
	if err != nil {
		t.Fatalf("registering over the registry route: %v", err)
	}
	if inst.PrincipalID != executorP.PrincipalID {
		t.Fatalf("instance principal = %s, want %s", inst.PrincipalID, executorP.PrincipalID)
	}
	e, err := jobsexec.New(peer, jobsexec.Config{InstanceID: inst.InstanceID, QueueKeys: []string{queue}, HeartbeatInterval: 50 * time.Millisecond, Logger: quiet().Logger})
	if err != nil {
		t.Fatal(err)
	}
	outcome := make(chan string, 1)
	if err := e.Handle(taskKey, func(ctx context.Context, j jobsexec.Job) (json.RawMessage, error) {
		if err := j.Authorize(ctx, "myapp.report.read", "myapp.tenant", "acme"); err != nil {
			return nil, err
		}
		// The owner is an admin, but the task never declared it.
		overreach := j.Authorize(ctx, "myapp.admin.all", "", "")
		if !errors.Is(overreach, jobsexec.ErrOutOfScope) {
			return nil, errors.New("an undeclared permission was not refused: " + errString(overreach))
		}
		outcome <- "rendered for " + j.EffectivePrincipalID.String()
		return json.RawMessage(`{"ok":true}`), nil
	}); err != nil {
		t.Fatal(err)
	}

	if started, err := e.PullOnce(ctxT(t)); err != nil || started != 1 {
		t.Fatalf("PullOnce over mTLS = %d, %v", started, err)
	}
	select {
	case got := <-outcome:
		if got != "rendered for "+owner.PrincipalID.String() {
			t.Errorf("the handler ran as %q, want the owner", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never ran")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, _, _ := deps.JobsReader.GetJob(ctx, submitted.Job.JobID)
		if j.State == jobsStructure.StateSucceeded {
			if j.ActorPrincipalID == nil || *j.ActorPrincipalID != executorP.PrincipalID {
				t.Errorf("actor = %v, want the executor", j.ActorPrincipalID)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job is %q (%s), never succeeded", j.State, j.LastError)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
