// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. They exercise what sdk's own unit tests
// cannot: that real stores from OpenDB compose through sdk.New/Seed, and
// that narrowed stores (read-only, scoped) behave as 17-sdk-model.md says.
package sdkdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/croge130/archipelago/aliasauth"
	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	jobsStructure "github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/jobsauth"
	"github.com/croge130/archipelago/sdk"
	vitalsStructure "github.com/croge130/archipelago/vitals/structure"
	"github.com/croge130/archipelago/vitalsauth"
	"github.com/google/uuid"
)

var allModes = sdk.Modes{Gatehouse: true, Policy: true, Alias: true, Vitals: true, Jobs: true}

func openStores(t *testing.T, modes sdk.Modes) (sdk.Stores, *archidb.Pool) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping sdkdb integration test")
	}
	stores, pool, err := OpenDB(context.Background(), archidb.Config{DSN: dsn}, modes)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(pool.Close)

	// Fresh state for every test. Tables for bases a test didn't enable
	// may not exist, so truncate only what exists.
	for _, table := range []string{
		"gatehouse_grants", "gatehouse_permission_definitions", "gatehouse_principals",
		"vitals_reading_history", "vitals_current_readings", "vitals_instances", "vitals_definitions",
		"policy_instances", "policy_definitions", "jobs_jobs", "jobs_task_definitions",
	} {
		_, _ = pool.Pgx().Exec(context.Background(), "TRUNCATE "+table+" CASCADE")
	}
	return stores, pool
}

func newApp(t *testing.T, stores sdk.Stores, modes sdk.Modes) *sdk.App {
	t.Helper()
	app, err := sdk.New(stores, modes, sdk.Config{})
	if err != nil {
		t.Fatalf("sdk.New: %v", err)
	}
	return app
}

func ctx5(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestFullAppSeedsIdempotentlyAndComposesAcrossModules(t *testing.T) {
	stores, _ := openStores(t, allModes)
	ctx := ctx5(t)

	// Seeding twice in a row — the second app is a "restart" — must
	// succeed both times with every step run.
	for i := 0; i < 2; i++ {
		app := newApp(t, stores, allModes)
		report, err := app.Seed(ctx)
		if err != nil {
			t.Fatalf("Seed #%d: %v (report %+v)", i+1, err, report)
		}
		if len(report) != 6 {
			t.Fatalf("Seed #%d report has %d steps, want 6: %+v", i+1, len(report), report)
		}
		for _, step := range report {
			if step.Status != sdk.SeedRan {
				t.Fatalf("Seed #%d step %s = %s, want ran", i+1, step.Name, step.Status)
			}
		}
	}

	// Seeding registered Gatehouse-core's own assume permissions.
	check := newApp(t, stores, allModes)
	for _, key := range []string{gatehouseFacade.PermissionAssumeCause, gatehouseFacade.PermissionAssumeExecute} {
		if _, found, err := check.Gatehouse.Reader.GetPermissionDefinition(ctx, key); err != nil || !found {
			t.Fatalf("%s not registered by Seed: found=%v err=%v", key, found, err)
		}
	}

	// One real cross-module flow through the exposed fields: principal +
	// grant in Gatehouse-core, then a Vitals write authorized through
	// vitalsauth — the composition, not each base's own behavior.
	app := newApp(t, stores, allModes)
	p, err := gatehouseFacade.EnsurePrincipal(ctx, app.Gatehouse.Reader, app.Gatehouse.Writer, "svc.sdk-test", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	if _, err := gatehouseFacade.GrantPermission(ctx, app.Gatehouse.Writer, gatehouseStructure.GrantSubjectTypePrincipal, p.PrincipalID, vitalsauth.PermissionWrite); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}

	now := time.Now().Truncate(time.Microsecond)
	def := vitalsStructure.Definition{DefinitionID: uuid.New(), DefinitionKey: "myapp.sdk.test", DefinitionVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := app.Vitals.Writer.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	inst := vitalsStructure.Instance{InstanceID: uuid.New(), ScopeType: "gatehouse.context", ScopeID: "myapp", InstanceKey: "sdk.test", DefinitionID: def.DefinitionID, CreatedAt: now, UpdatedAt: now}
	if err := app.Vitals.Writer.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	// The global grant above covers the scoped check vitalsauth makes.
	if _, err := vitalsauth.WriteReading(ctx, app.Gatehouse.Reader, app.Vitals.Reader, app.Vitals.Writer, p.PrincipalID, vitalsStructure.Reading{InstanceID: inst.InstanceID, State: vitalsStructure.StateOK}); err != nil {
		t.Fatalf("vitalsauth.WriteReading through SDK stores: %v", err)
	}
}

func TestReadOnlyAppReadsExistingDataAndSeedsNothing(t *testing.T) {
	stores, _ := openStores(t, allModes)
	ctx := ctx5(t)

	// A writer node seeds first, so the read-only node has data to read.
	if _, err := newApp(t, stores, allModes).Seed(ctx); err != nil {
		t.Fatalf("writer Seed: %v", err)
	}

	// The read-only node: same Readers, every Writer dropped.
	ro := stores
	ro.Gatehouse.Writer = nil
	ro.Policy.Writer = nil
	ro.Alias.Writer = nil
	ro.Vitals.Writer = nil
	ro.Jobs.Writer = nil
	app := newApp(t, ro, allModes)

	report, err := app.Seed(ctx)
	if err != nil {
		t.Fatalf("read-only Seed returned error: %v", err)
	}
	if len(report) != 6 {
		t.Fatalf("report has %d steps, want 6: %+v", len(report), report)
	}
	for _, step := range report {
		if step.Status != sdk.SeedSkipped {
			t.Errorf("step %s = %s, want skipped on a read-only node", step.Name, step.Status)
		}
	}

	// Reads still work against data the writer node seeded.
	def, found, err := app.Gatehouse.Reader.GetPermissionDefinition(ctx, aliasauth.PermissionCreate)
	if err != nil || !found {
		t.Fatalf("read-only node cannot read seeded permission: found=%v err=%v", found, err)
	}
	if def.PermissionKey != aliasauth.PermissionCreate {
		t.Fatalf("PermissionKey = %q", def.PermissionKey)
	}
}

func TestScopedAppWithoutGatehouseSeedsOnlyWhatItCan(t *testing.T) {
	stores, _ := openStores(t, allModes)
	ctx := ctx5(t)

	// A scoped node holding only Vitals: Gatehouse, Policy and Alias are
	// not part of its access at all.
	scoped := sdk.Stores{Vitals: stores.Vitals}
	modes := sdk.Modes{Vitals: true}
	app := newApp(t, scoped, modes)

	report, err := app.Seed(ctx)
	if err != nil {
		t.Fatalf("Seed: %v (report %+v)", err, report)
	}
	if len(report) != 1 || report[0].Name != "vitals.SeedBuiltinDefinitions" || report[0].Status != sdk.SeedRan {
		t.Fatalf("report = %+v, want exactly the vitals base step, ran", report)
	}

	// And New rejects a mode that needs a base the node doesn't have.
	if _, err := sdk.New(scoped, sdk.Modes{Vitals: true, Alias: true}, sdk.Config{}); err == nil {
		t.Fatal("New accepted Alias mode with no Alias store")
	}
}

func TestJobsFlowThroughTheSDKsOwnStores(t *testing.T) {
	stores, _ := openStores(t, allModes)
	ctx := ctx5(t)
	app := newApp(t, stores, allModes)
	if _, err := app.Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	deps, err := app.JobsAuth()
	if err != nil {
		t.Fatalf("JobsAuth: %v", err)
	}

	mk := func(key string, typ gatehouseStructure.PrincipalType) gatehouseStructure.Principal {
		p, err := gatehouseFacade.EnsurePrincipal(ctx, app.Gatehouse.Reader, app.Gatehouse.Writer, key, typ)
		if err != nil {
			t.Fatalf("EnsurePrincipal: %v", err)
		}
		return p
	}
	owner := mk("user.owner", gatehouseStructure.PrincipalTypeUser)
	runner := mk("runner.one", gatehouseStructure.PrincipalTypeServiceAccount)

	grantOn := func(holder uuid.UUID, key, ctxType, ctxID string) {
		now := time.Now().Truncate(time.Microsecond)
		k, ct, ci := key, ctxType, ctxID
		if err := app.Gatehouse.Writer.CreateGrant(ctx, gatehouseStructure.Grant{
			GrantID: uuid.New(), SubjectType: gatehouseStructure.GrantSubjectTypePrincipal, SubjectID: holder,
			TargetType: gatehouseStructure.GrantTargetTypePermission, PermissionKey: &k,
			Scope: gatehouseStructure.GrantScopeContext, ContextType: &ct, ContextID: &ci,
			Effect: gatehouseStructure.GrantEffectAllow, Status: gatehouseStructure.GrantStatusActive,
			Origin: gatehouseStructure.GrantOriginManual, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateGrant: %v", err)
		}
	}

	if err := gatehouseFacade.RegisterPermission(ctx, app.Gatehouse.Reader, app.Gatehouse.Writer,
		gatehouseStructure.PermissionDefinition{PermissionKey: "myapp.audit.write", RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard},
		gatehouseFacade.RegisterPermissionOptions{}); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	def := jobsStructure.TaskDefinition{
		TaskKey: "myapp.audit.flush", DefaultQueueKey: "myapp.audit",
		Scope:      []jobsStructure.ScopeEntry{{PermissionKey: "myapp.audit.write"}},
		Idempotent: true, DefaultMaxAttempts: 2, DefaultAttemptTimeout: time.Minute, PriorityCap: jobsStructure.PriorityNormal,
	}
	if err := jobsFacade.RegisterTaskDefinition(ctx, app.Jobs.Reader, app.Jobs.Writer, def, jobsFacade.RegisterTaskOptions{}); err != nil {
		t.Fatalf("RegisterTaskDefinition: %v", err)
	}

	grantOn(owner.PrincipalID, jobsauth.PermissionSubmit, jobsauth.ContextTypeQueue, "myapp.audit")
	grantOn(runner.PrincipalID, jobsauth.PermissionClaim, jobsauth.ContextTypeQueue, "myapp.audit")
	grantOn(runner.PrincipalID, gatehouseFacade.PermissionAssumeExecute, gatehouseFacade.ContextTypePrincipal, owner.PrincipalID.String())
	if _, err := gatehouseFacade.GrantPermission(ctx, app.Gatehouse.Writer, gatehouseStructure.GrantSubjectTypePrincipal, owner.PrincipalID, "myapp.audit.write"); err != nil {
		t.Fatalf("GrantPermission: %v", err)
	}

	res, err := jobsauth.Submit(ctx, deps, jobsFacade.SubmitRequest{TaskKey: def.TaskKey, RequestedBy: owner.PrincipalID})
	if err != nil || !res.Created {
		t.Fatalf("Submit: %+v err=%v", res, err)
	}
	ready, skipped, err := jobsauth.Claim(ctx, deps, jobsauth.ClaimRequest{
		ExecutorInstanceID: uuid.New(), ExecutorPrincipalID: runner.PrincipalID,
		TaskKeys: []string{def.TaskKey}, QueueKeys: []string{"myapp.audit"}, Limit: 1, ClaimTTL: time.Minute,
	})
	if err != nil || len(skipped) != 0 || len(ready) != 1 {
		t.Fatalf("Claim: ready=%d skipped=%+v err=%v", len(ready), skipped, err)
	}
	cj := ready[0]
	if cj.EffectivePrincipalID != owner.PrincipalID || cj.SessionID == nil {
		t.Fatalf("an owner-mode job should run as the owner under an assumed session: %+v", cj)
	}
	if err := jobsauth.Authorize(ctx, deps, cj, "myapp.audit.write", "", ""); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if ok, err := jobsauth.Complete(ctx, deps, cj, nil); err != nil || !ok {
		t.Fatalf("Complete: ok=%v err=%v", ok, err)
	}

	// A read-only node over the same stores can still read the result, and
	// its writes fail clearly instead of panicking.
	ro := stores
	ro.Gatehouse.Writer, ro.Jobs.Writer = nil, nil
	roApp := newApp(t, ro, allModes)
	roDeps, err := roApp.JobsAuth()
	if err != nil {
		t.Fatalf("JobsAuth (read-only): %v", err)
	}
	got, err := jobsauth.GetJob(ctx, roDeps, owner.PrincipalID, res.Job.JobID)
	if err != nil || got.State != jobsStructure.StateSucceeded {
		t.Fatalf("read-only GetJob: %+v err=%v, want succeeded", got, err)
	}
	if _, err := jobsauth.Cancel(ctx, roDeps, owner.PrincipalID, res.Job.JobID); err == nil {
		t.Error("Cancel on a read-only node should fail clearly")
	}
}
