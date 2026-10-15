// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. The permission checks run against real
// grants; the runtime underneath is the real noderoles over the real governor
// and scheduler, on a manual clock.
package noderolesauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/google/uuid"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ctx() context.Context { return context.Background() }

type fx struct {
	reader *dbstore.PostgresReader
	writer *dbstore.PostgresWriter
	pool   *archidb.Pool
	node   structure.Principal
}

func setup(t *testing.T) *fx {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping noderolesauth integration test")
	}
	pool, err := archidb.Open(ctx(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrations, err := dbstore.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.ProvisionSchemas(ctx(), migrations); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Pgx().Exec(ctx(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_leases, gatehouse_instances, gatehouse_endpoint_definitions,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`); err != nil {
		t.Fatal(err)
	}
	f := &fx{reader: dbstore.NewPostgresReader(pool.Pgx()), writer: dbstore.NewPostgresWriter(pool.Pgx()), pool: pool}
	f.node, err = facade.EnsurePrincipal(ctx(), f.reader, f.writer, "node.one", structure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fx) register(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if err := facade.RegisterPermission(ctx(), f.reader, f.writer, structure.PermissionDefinition{
			PermissionKey: k, RequiredAuthorityLevel: structure.AuthorityLevelStandard,
		}, facade.RegisterPermissionOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fx) grant(t *testing.T, key string) {
	t.Helper()
	if _, err := facade.GrantPermission(ctx(), f.writer, structure.GrantSubjectTypePrincipal, f.node.PrincipalID, key); err != nil {
		t.Fatal(err)
	}
}

func (f *fx) revoke(t *testing.T, key string) {
	t.Helper()
	if _, err := f.pool.Pgx().Exec(ctx(), `UPDATE gatehouse_grants SET status = 'revoked' WHERE subject_id = $1 AND permission_key = $2`, f.node.PrincipalID.String(), key); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizerNamesEveryMissingPermissionAndFailsClosedOnUnknownOnes(t *testing.T) {
	f := setup(t)
	f.register(t, "myapp.audit.write", "myapp.audit.read", "myapp.audit.purge")
	f.grant(t, "myapp.audit.write")
	auth := Authorizer(f.reader, f.node.PrincipalID)

	if err := auth(ctx(), []string{"myapp.audit.write"}); err != nil {
		t.Errorf("a held permission: %v", err)
	}
	if err := auth(ctx(), nil); err != nil {
		t.Errorf("a node role needing nothing: %v", err)
	}
	err := auth(ctx(), []string{"myapp.audit.write", "myapp.audit.read", "myapp.audit.purge", "myapp.never.registered"})
	var miss *MissingPermissionsError
	if !errors.As(err, &miss) {
		t.Fatalf("err = %v, want a MissingPermissionsError", err)
	}
	if got := strings.Join(miss.Missing, ","); got != "myapp.audit.read,myapp.audit.purge,myapp.never.registered" {
		t.Errorf("missing = %s; want every one it lacks, including the key nobody registered", got)
	}
	if !errors.Is(err, evaluation.ErrDenied) || miss.PrincipalID != f.node.PrincipalID {
		t.Errorf("the error should be a denial for this principal: %v", err)
	}
}

func TestAStoreFailureIsNotMistakenForARefusal(t *testing.T) {
	f := setup(t)
	f.register(t, "myapp.audit.write")
	f.grant(t, "myapp.audit.write")
	cancelled, cancel := context.WithCancel(ctx())
	cancel() // the database cannot be reached under this context
	err := Authorizer(f.reader, f.node.PrincipalID)(cancelled, []string{"myapp.audit.write"})
	if err == nil {
		t.Fatal("an unreachable store authorized the node (it must fail closed)")
	}
	if errors.Is(err, evaluation.ErrDenied) {
		t.Errorf("an outage was reported as a denial: %v", err)
	}
}

// ---- through the runtime -------------------------------------------------

type rig struct {
	gov *governor.Governor
	clk *scheduler.ManualClock
	rt  *noderoles.Runtime
}

func newRig(t *testing.T) *rig {
	t.Helper()
	gov, err := governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 4}})
	if err != nil {
		t.Fatal(err)
	}
	clk := scheduler.NewManualClock(t0)
	rt, err := noderoles.New(noderoles.Config{Governor: gov, Domain: "d", Clock: clk, Rand: func() float64 { return 0 },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Stop(ctx()); gov.Close() })
	return &rig{gov: gov, clk: clk, rt: rt}
}

func (r *rig) idle() bool {
	if st := r.gov.Stats(); st.Running != 0 || st.Waiting != 0 {
		return false
	}
	for _, s := range r.rt.Status() {
		for _, d := range s.Duties {
			if d.InFlight != 0 {
				return false
			}
		}
	}
	return true
}

func (r *rig) step(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !r.idle() {
		if time.Now().After(deadline) {
			t.Fatal("the previous run never finished")
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.clk.Advance(d)
	for !r.idle() {
		if time.Now().After(deadline.Add(3 * time.Second)) {
			t.Fatal("the run never finished")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func every(t *testing.T, d time.Duration) recurrence.Schedule {
	t.Helper()
	s, err := recurrence.Every(d, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func definition(t *testing.T, key string, perms []string, run func(context.Context, scheduler.Fire) error) noderoles.Definition {
	return noderoles.Definition{
		Key: key, Permissions: perms, Default: &governor.Limits{Concurrency: 1, QueueDepth: 1},
		Duties: []noderoles.Duty{{Name: "work", Schedule: every(t, time.Minute), Events: []string{"poke"}, Run: run}},
	}
}

func useDefault() noderoles.Adoption {
	return noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}
}

func TestANodeRoleIsAdoptedOnlyWithItsGrantsAndStopsWhenTheyAreRevoked(t *testing.T) {
	f := setup(t)
	f.register(t, "myapp.audit.write", "myapp.audit.read")
	r := newRig(t)
	var mu sync.Mutex
	runs := 0
	d := definition(t, "myapp.auditor", []string{"myapp.audit.write", "myapp.audit.read"}, func(context.Context, scheduler.Fire) error {
		mu.Lock()
		runs++
		mu.Unlock()
		return nil
	})
	count := func() int { mu.Lock(); defer mu.Unlock(); return runs }

	// Not granted: adoption fails closed and says exactly what is missing.
	_, err := r.rt.Adopt(ctx(), d, Bind(useDefault(), f.reader, f.node.PrincipalID))
	if !errors.Is(err, noderoles.ErrNotAuthorized) || !strings.Contains(err.Error(), "myapp.audit.write,myapp.audit.read") && !strings.Contains(err.Error(), "myapp.audit.write, myapp.audit.read") {
		t.Fatalf("adopting without grants: %v", err)
	}
	var miss *MissingPermissionsError
	if !errors.As(err, &miss) || len(miss.Missing) != 2 {
		t.Errorf("the refusal should say what is missing: %v", err)
	}

	f.grant(t, "myapp.audit.write")
	f.grant(t, "myapp.audit.read")
	if _, err := r.rt.Adopt(ctx(), d, Bind(useDefault(), f.reader, f.node.PrincipalID)); err != nil {
		t.Fatalf("adopting with grants: %v", err)
	}
	r.rt.Start()
	r.step(t, time.Minute)
	if count() != 1 {
		t.Fatalf("runs = %d", count())
	}

	f.revoke(t, "myapp.audit.read")
	r.step(t, time.Minute)
	if count() != 1 {
		t.Errorf("a duty ran after a needed grant was revoked (%d runs)", count())
	}
	if st := r.rt.Status()[0]; st.Unauthorized != 1 {
		t.Errorf("Unauthorized = %d", st.Unauthorized)
	}

	f.grant(t, "myapp.audit.read") // restored: the node role works again with no restart
	r.step(t, time.Minute)
	if count() != 2 {
		t.Errorf("runs = %d after the grant came back", count())
	}
}

func TestEachRunRecordsWhoItRanAsAndWhy(t *testing.T) {
	f := setup(t)
	r := newRig(t)
	got := make(chan Authority, 4)
	d := definition(t, "myapp.recorded", nil, func(ctx context.Context, fire scheduler.Fire) error {
		a, ok := AuthorityFrom(ctx)
		if !ok {
			t.Error("a run had no authority in its context")
		}
		got <- a
		return nil
	})
	if _, err := r.rt.Adopt(ctx(), d, Bind(useDefault(), f.reader, f.node.PrincipalID)); err != nil {
		t.Fatal(err)
	}
	r.rt.Start()
	r.step(t, time.Minute)
	scheduled := <-got
	r.rt.Emit("poke")
	var event Authority
	select {
	case event = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("the event run never happened")
	}

	for name, a := range map[string]Authority{"scheduled": scheduled, "event": event} {
		if a.ActorPrincipalID != f.node.PrincipalID || a.RequestedByPrincipalID != f.node.PrincipalID || a.Ref != "myapp.recorded/work" {
			t.Errorf("%s run: %+v; a node acting under its own authority is both actor and requester", name, a)
		}
	}
	if scheduled.Source != SourceScheduledTask || event.Source != SourceServiceAction {
		t.Errorf("sources: scheduled=%q event=%q, want scheduled_task and service_action", scheduled.Source, event.Source)
	}
	if _, ok := AuthorityFrom(ctx()); ok {
		t.Error("a context that no run created claims an authority")
	}
}

func TestRegisterPermissionsRegistersWhatNodeRolesNeedOnceAndHonoursTheNamespaceGuard(t *testing.T) {
	f := setup(t)
	noop := func(context.Context, scheduler.Fire) error { return nil }
	a := definition(t, "myapp.a", []string{"myapp.a.read", "myapp.shared"}, noop)
	b := definition(t, "myapp.b", []string{"myapp.b.write", "myapp.shared"}, noop)

	if err := RegisterPermissions(ctx(), f.reader, f.writer, facade.RegisterPermissionOptions{}, a, b); err != nil {
		t.Fatal(err)
	}
	if err := RegisterPermissions(ctx(), f.reader, f.writer, facade.RegisterPermissionOptions{}, a, b); err != nil {
		t.Errorf("registering twice: %v", err)
	}
	for _, k := range []string{"myapp.a.read", "myapp.b.write", "myapp.shared"} {
		def, found, err := f.reader.GetPermissionDefinition(ctx(), k)
		if err != nil || !found || def.RequiredAuthorityLevel != structure.AuthorityLevelStandard {
			t.Errorf("%s: found=%v level=%v err=%v", k, found, def.RequiredAuthorityLevel, err)
		}
	}

	// A core node role lives under a reserved namespace and says so.
	core := definition(t, "archipelago.core", []string{"archipelago.core.run"}, noop)
	if err := RegisterPermissions(ctx(), f.reader, f.writer, facade.RegisterPermissionOptions{}, core); err == nil {
		t.Error("a reserved-namespace permission was registered without the explicit override")
	}
	if err := RegisterPermissions(ctx(), f.reader, f.writer, facade.RegisterPermissionOptions{AllowReservedNamespace: true}, core); err != nil {
		t.Errorf("with the override: %v", err)
	}
}

func TestBindLeavesTheRestOfTheAdoptionAlone(t *testing.T) {
	f := setup(t)
	in := noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}, Priority: governor.Critical}
	out := Bind(in, f.reader, f.node.PrincipalID)
	if out.Budget.Tier != governor.TierLow || out.Priority != governor.Critical || out.Authorize == nil || out.Annotate == nil {
		t.Errorf("Bind changed more than it should or set too little: %+v", out)
	}
	if in.Authorize != nil {
		t.Error("Bind modified its argument")
	}
	_ = uuid.Nil
}
