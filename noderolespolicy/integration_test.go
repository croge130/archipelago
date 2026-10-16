// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. Policy is the real Policy over Postgres; the
// node role underneath is the real noderoles over the real governor and
// scheduler, on a manual clock.
package noderolespolicy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/policy/evaluation"
	"github.com/croge130/archipelago/policy/facade"
	"github.com/croge130/archipelago/policy/storage/dbstore"
	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ctx() context.Context { return context.Background() }

const actor = "operator:test"

type env struct {
	pool *archidb.Pool
	r    *dbstore.PostgresReader
	w    *dbstore.PostgresWriter
	gov  *governor.Governor
	clk  *scheduler.ManualClock
	rt   *noderoles.Runtime
	b    *Binding
}

func setup(t *testing.T) *env { return setupWithDomain(t, governor.Ceiling{Concurrency: 100}) }

func setupWithDomain(t *testing.T, domain governor.Ceiling) *env {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping noderolespolicy integration test")
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
	if _, err := pool.Pgx().Exec(ctx(), `TRUNCATE policy_instances, policy_context_members, policy_contexts, policy_definitions`); err != nil {
		t.Fatal(err)
	}
	e := &env{pool: pool, r: dbstore.NewPostgresReader(pool.Pgx()), w: dbstore.NewPostgresWriter(pool.Pgx())}
	if err := EnsureDefinitions(ctx(), e.r, e.w, actor); err != nil {
		t.Fatal(err)
	}

	e.gov, err = governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 100}, Domains: map[string]governor.Ceiling{"d": domain}})
	if err != nil {
		t.Fatal(err)
	}
	e.clk = scheduler.NewManualClock(t0)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.rt, err = noderoles.New(noderoles.Config{Governor: e.gov, Domain: "d", Record: scheduler.NewMemoryRecord(), Clock: e.clk, Rand: func() float64 { return 0 }, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.rt.Stop(ctx()); e.gov.Close() })
	e.b, err = NewBinding(BindingConfig{Store: e.r, Domain: "d", Clock: e.clk.Now, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) set(t *testing.T, key string, scope Scope, v any) {
	t.Helper()
	if _, err := Set(ctx(), e.r, e.w, key, scope, v, actor); err != nil {
		t.Fatalf("Set %s at %s: %v", key, scope, err)
	}
}

func (e *env) clear(t *testing.T, key string, scope Scope) bool {
	t.Helper()
	had, err := Clear(ctx(), e.r, e.w, key, scope, actor)
	if err != nil {
		t.Fatalf("Clear %s at %s: %v", key, scope, err)
	}
	return had
}

func (e *env) apply(t *testing.T, a *noderoles.Adopted) Report {
	t.Helper()
	rep, err := e.b.Apply(ctx(), a)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return rep
}

func (e *env) idle() bool {
	if st := e.gov.Stats(); st.Running != 0 || st.Waiting != 0 {
		return false
	}
	for _, r := range e.rt.Status() {
		for _, d := range r.Duties {
			if d.InFlight != 0 {
				return false
			}
		}
	}
	return true
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// step advances the clock once the previous run is fully over.
func (e *env) step(t *testing.T, d time.Duration) {
	t.Helper()
	waitFor(t, "the previous run to be fully over", e.idle)
	e.clk.Advance(d)
}

type counter struct{ n atomic.Int32 }

func (c *counter) run(context.Context, scheduler.Fire) error { c.n.Add(1); return nil }

var tiers = governor.TierTable{
	governor.TierLow: {Concurrency: 4, QueueDepth: 8, RunTimeout: time.Minute},
}

func every(t *testing.T, d time.Duration) recurrence.Schedule {
	t.Helper()
	s, err := recurrence.Every(d, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// adopt starts a node role "myapp.sweeper" with a scheduled duty "sweep"
// (every minute, never more often than every 30s) and an event-driven duty.
func (e *env) adopt(t *testing.T, sweeps, events *counter) *noderoles.Adopted {
	t.Helper()
	ad, err := e.rt.Adopt(ctx(), noderoles.Definition{
		Key: "myapp.sweeper", Tiers: tiers, Priority: governor.Normal,
		Duties: []noderoles.Duty{
			{Name: "sweep", Schedule: every(t, time.Minute), MinInterval: 30 * time.Second, Run: sweeps.run},
			{Name: "onchange", Events: []string{"changed"}, Run: events.run},
		},
	}, noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}})
	if err != nil {
		t.Fatal(err)
	}
	return ad
}

func TestDefinitionsAreIdempotentAndNeverRedefinedSilently(t *testing.T) {
	e := setup(t)
	if err := EnsureDefinitions(ctx(), e.r, e.w, actor); err != nil {
		t.Fatalf("second EnsureDefinitions: %v", err)
	}
	// Someone registered a key with another shape first: that is an error,
	// not a silent redefinition.
	if _, err := e.pool.Pgx().Exec(ctx(), `TRUNCATE policy_instances, policy_definitions`); err != nil {
		t.Fatal(err)
	}
	if _, err := facade.EnsurePolicyDefinition(ctx(), e.r, e.w, KeyConcurrency,
		typedvalue.Count("request", ""), typeconstraints.Set{}, typeconstraints.MergeMaximum,
		structure.ActivationImmediate, structure.BindingInheritedLive, actor); err != nil {
		t.Fatal(err)
	}
	err := EnsureDefinitions(ctx(), e.r, e.w, actor)
	if !errors.Is(err, facade.ErrConflict) || !strings.Contains(err.Error(), KeyConcurrency) {
		t.Fatalf("want a conflict naming %s, got %v", KeyConcurrency, err)
	}
}

func TestSilentPolicyChangesNothing(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	rep := e.apply(t, ad)
	if rep.Changed || rep.Budget != nil || len(rep.Rescheduled)+len(rep.Disabled)+len(rep.Capped) != 0 {
		t.Fatalf("policy is silent, yet %+v", rep)
	}
	if ad.Limits() != ad.Base() {
		t.Fatalf("limits %+v != base %+v", ad.Limits(), ad.Base())
	}
}

func TestABudgetFromPolicyAppliesIsStableAndRestoresWhenRemoved(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	base := ad.Base()

	e.set(t, KeyConcurrency, ForRole("myapp.sweeper"), 2)
	e.set(t, KeyQueueDepth, Global(), 1)
	e.set(t, KeyRunTimeout, ForRole("myapp.sweeper"), 20.0)
	rep := e.apply(t, ad)
	if !rep.Changed || rep.Budget == nil {
		t.Fatalf("report %+v", rep)
	}
	want := governor.Limits{Concurrency: 2, QueueDepth: 1, RunTimeout: 20 * time.Second}
	if ad.Limits() != want {
		t.Fatalf("limits %+v, want %+v", ad.Limits(), want)
	}
	if ad.Base() != base {
		t.Fatalf("policy must not move the adopted base: %+v != %+v", ad.Base(), base)
	}

	if rep := e.apply(t, ad); rep.Changed {
		t.Fatalf("nothing changed, yet %+v", rep)
	}

	for _, k := range []string{KeyConcurrency, KeyRunTimeout} {
		if !e.clear(t, k, ForRole("myapp.sweeper")) {
			t.Fatalf("expected a value for %s to clear", k)
		}
	}
	e.clear(t, KeyQueueDepth, Global())
	if rep := e.apply(t, ad); !rep.Changed {
		t.Fatalf("removing policy should restore the adoption: %+v", rep)
	}
	if ad.Limits() != base {
		t.Fatalf("restored limits %+v, want %+v", ad.Limits(), base)
	}
	if had := e.clear(t, KeyConcurrency, ForRole("myapp.sweeper")); had {
		t.Fatal("clearing nothing must say so")
	}
}

func TestPolicyAboveWhatTheNodeAdoptedIsCappedAndSaysSo(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyConcurrency, Global(), 50)
	e.set(t, KeyQueueDepth, Global(), 50)
	e.set(t, KeyRunTimeout, Global(), 3600.0)
	rep := e.apply(t, ad)
	if len(rep.Capped) != 3 {
		t.Fatalf("want three caps, got %v", rep.Capped)
	}
	if ad.Limits() != ad.Base() {
		t.Fatalf("a capped request must leave the adopted budget: %+v vs %+v", ad.Limits(), ad.Base())
	}
	if rep.Changed {
		t.Fatalf("nothing differs from what runs, so nothing should be applied: %+v", rep)
	}
}

func TestATimeoutIsATighteningWhenTheNodeAdoptedNone(t *testing.T) {
	e := setup(t)
	var sw counter
	ad, err := e.rt.Adopt(ctx(), noderoles.Definition{
		Key: "myapp.untimed", Tiers: governor.TierTable{governor.TierLow: {Concurrency: 1}},
		Duties: []noderoles.Duty{{Name: "d", Schedule: every(t, time.Minute), Run: sw.run}},
	}, noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}})
	if err != nil {
		t.Fatal(err)
	}
	e.set(t, KeyRunTimeout, ForRole("myapp.untimed"), 90.0)
	rep := e.apply(t, ad)
	if len(rep.Capped) != 0 || ad.Limits().RunTimeout != 90*time.Second {
		t.Fatalf("timeout %v, report %+v", ad.Limits().RunTimeout, rep)
	}
}

func TestScopesCombineToTheMoreRestrictiveAndOtherDomainsAreIgnored(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyConcurrency, Global(), 3)
	e.set(t, KeyConcurrency, ForDomain("d"), 2)
	e.set(t, KeyConcurrency, ForRole("myapp.sweeper"), 3)
	e.set(t, KeyConcurrency, ForDomain("somewhere-else"), 1)
	e.set(t, KeyConcurrency, ForRole("myapp.other"), 1)
	e.apply(t, ad)
	if got := ad.Limits().Concurrency; got != 2 {
		t.Fatalf("concurrency %d, want 2 (the domain's, the tightest that applies here)", got)
	}
}

func TestAnIntervalReschedulesFromNowAndRemovalRestoresTheAdoptedSchedule(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.set(t, KeyScheduleInterval, ForDuty("myapp.sweeper", "sweep"), 300.0)
	rep := e.apply(t, ad)
	if len(rep.Rescheduled) != 1 || rep.Rescheduled[0] != "sweep" {
		t.Fatalf("report %+v", rep)
	}
	// Five minutes at five-minute spacing is one run, not the five a minute
	// schedule would have made.
	for i := 0; i < 5; i++ {
		e.step(t, time.Minute)
	}
	waitFor(t, "the single five-minute run", func() bool { return sw.n.Load() == 1 })
	waitFor(t, "quiet", e.idle)
	if got := sw.n.Load(); got != 1 {
		t.Fatalf("%d runs in five minutes at a five-minute interval", got)
	}

	// An unchanged interval must not re-anchor the schedule on a refresh.
	e.step(t, time.Minute)
	if rep := e.apply(t, ad); rep.Changed {
		t.Fatalf("a refresh with nothing new re-applied: %+v", rep)
	}
	e.clear(t, KeyScheduleInterval, ForDuty("myapp.sweeper", "sweep"))
	rep = e.apply(t, ad)
	if len(rep.Restored) != 1 {
		t.Fatalf("report %+v", rep)
	}
	before := sw.n.Load()
	for i := 0; i < 3; i++ {
		e.step(t, time.Minute)
		want := before + int32(i) + 1
		waitFor(t, "a run per minute again", func() bool { return sw.n.Load() == want })
	}
}

func TestAnIntervalBelowTheDutysFloorIsRaisedAndReported(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyScheduleInterval, Global(), 5.0)
	rep := e.apply(t, ad)
	if len(rep.Floored) != 1 || rep.Floored[0] != "sweep" || len(rep.Notes) != 2 {
		t.Fatalf("report %+v", rep)
	}
	// The event duty has no schedule for an interval to replace.
	var noted bool
	for _, n := range rep.Notes {
		noted = noted || strings.Contains(n, "onchange")
	}
	if !noted {
		t.Fatalf("an interval for an event-driven duty must be explained: %v", rep.Notes)
	}
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	// Every 30 seconds, not every 5.
	e.step(t, 29*time.Second)
	e.step(t, time.Second)
	waitFor(t, "the first run at the floor", func() bool { return sw.n.Load() == 1 })
	e.step(t, 20*time.Second)
	waitFor(t, "quiet", e.idle)
	if got := sw.n.Load(); got != 1 {
		t.Fatalf("%d runs; the floor did not hold", got)
	}
}

func TestAnIntervalForAnEventDrivenDutyIsRefusedWithAReasonAndTheScheduleIsUntouched(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyScheduleInterval, ForDuty("myapp.sweeper", "onchange"), 60.0)
	rep := e.apply(t, ad)
	if rep.Changed || len(rep.Rescheduled) != 0 || len(rep.Notes) != 1 {
		t.Fatalf("report %+v", rep)
	}
	if ad.DefaultSchedule("onchange") != nil {
		t.Fatal("test premise: the duty has no schedule")
	}
}

func TestADutyPolicyDisablesStopsItAndOnlyPolicysOwnDisablingIsUndone(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.set(t, KeyScheduleEnabled, ForDuty("myapp.sweeper", "sweep"), false)
	rep := e.apply(t, ad)
	if len(rep.Disabled) != 1 {
		t.Fatalf("report %+v", rep)
	}
	for i := 0; i < 3; i++ {
		e.step(t, time.Minute)
	}
	waitFor(t, "quiet", e.idle)
	if sw.n.Load() != 0 {
		t.Fatalf("a disabled duty ran %d times", sw.n.Load())
	}
	if rep := e.apply(t, ad); rep.Changed {
		t.Fatalf("refresh re-disabled: %+v", rep)
	}

	e.set(t, KeyScheduleEnabled, Global(), true) // true AND false is still false
	e.apply(t, ad)
	e.step(t, time.Minute)
	waitFor(t, "quiet", e.idle)
	if sw.n.Load() != 0 {
		t.Fatal("a global true must not override a duty's false")
	}

	e.clear(t, KeyScheduleEnabled, ForDuty("myapp.sweeper", "sweep"))
	rep = e.apply(t, ad)
	if len(rep.Enabled) != 1 {
		t.Fatalf("report %+v", rep)
	}
	e.step(t, time.Minute)
	waitFor(t, "a run once enabled again", func() bool { return sw.n.Load() == 1 })

	// A duty the app itself switched off is not policy's to switch on.
	if _, err := ad.Reconfigure(noderoles.Update{Enabled: map[string]bool{"sweep": false}}); err != nil {
		t.Fatal(err)
	}
	e.apply(t, ad)
	e.step(t, time.Minute)
	waitFor(t, "quiet", e.idle)
	if sw.n.Load() != 1 {
		t.Fatalf("policy turned back on a duty it never turned off (%d runs)", sw.n.Load())
	}
}

func TestAValueOutsideTheKeysConstraintsIsRejectedAtTheDoor(t *testing.T) {
	e := setup(t)
	for _, tc := range []struct {
		key string
		v   any
	}{{KeyConcurrency, 0}, {KeyQueueDepth, -1}, {KeyRunTimeout, 0.5}, {KeyScheduleInterval, 0.1}} {
		if _, err := Set(ctx(), e.r, e.w, tc.key, Global(), tc.v, actor); err == nil {
			t.Errorf("%s=%v should be rejected", tc.key, tc.v)
		}
	}
	if _, err := Set(ctx(), e.r, e.w, KeyScheduleEnabled, Global(), "perhaps", actor); err == nil {
		t.Error("a non-bool enabled should be rejected")
	}
	if _, err := Set(ctx(), e.r, e.w, "archipelago.noderoles.nope", Global(), 1, actor); err == nil {
		t.Error("an unknown key should be rejected")
	}
	if _, err := Set(ctx(), e.r, e.w, KeyConcurrency, Scope{}, 1, actor); err == nil {
		t.Error("an empty scope should be rejected")
	}
}

func TestACeilingStillAppliesToWhatPolicyAsks(t *testing.T) {
	e := setupWithDomain(t, governor.Ceiling{Concurrency: 2})
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev) // asks for 4; the domain share already holds it to 2
	if ad.Limits().Concurrency != 2 {
		t.Fatalf("premise: %+v", ad.Limits())
	}
	e.set(t, KeyConcurrency, Global(), 3)
	rep := e.apply(t, ad)
	if !rep.Changed || len(rep.Clamps) != 1 || rep.Clamps[0].By != "domain" {
		t.Fatalf("policy asked for 3; the domain's ceiling of 2 must still apply and be reported: %+v", rep)
	}
	if ad.Limits().Concurrency != 2 {
		t.Fatalf("limits %+v", ad.Limits())
	}
}

func TestAStoreFailureLeavesTheNodeRoleExactlyAsItWas(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyConcurrency, Global(), 1)
	e.apply(t, ad)
	if ad.Limits().Concurrency != 1 {
		t.Fatal("premise")
	}
	// A binding whose store is gone.
	if _, err := e.pool.Pgx().Exec(ctx(), `ALTER TABLE policy_definitions RENAME TO policy_definitions_gone`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := e.pool.Pgx().Exec(ctx(), `ALTER TABLE policy_definitions_gone RENAME TO policy_definitions`); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := e.b.Apply(ctx(), ad); err == nil {
		t.Fatal("a failing store must be an error")
	}
	if ad.Limits().Concurrency != 1 {
		t.Fatalf("a store failure changed the running budget: %+v", ad.Limits())
	}
}

func TestRefreshAppliesPolicyEditsToRunningNodeRolesOnItsOwnSchedule(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	rd, err := RefreshRole(e.rt, e.b, time.Minute, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.Adopt(ctx(), rd, noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}}); err != nil {
		t.Fatal(err)
	}
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.set(t, KeyConcurrency, Global(), 1)
	e.step(t, time.Minute)
	waitFor(t, "policy to reach the running node role", func() bool { return ad.Limits().Concurrency == 1 })
	e.clear(t, KeyConcurrency, Global())
	e.step(t, time.Minute)
	waitFor(t, "removal to reach it too", func() bool { return ad.Limits() == ad.Base() })
}

// failingFor fails policy reads for one node role and passes the rest.
type failingFor struct {
	evaluation.Store
	role string
}

func (f failingFor) RefInstances(ctx context.Context, id uuid.UUID, refs []structure.Ref) ([]structure.PolicyInstance, error) {
	for _, r := range refs {
		if r.Kind == RefRole && r.Key == f.role {
			return nil, errors.New("store unavailable for this role")
		}
	}
	return f.Store.RefInstances(ctx, id, refs)
}

func TestRefreshKeepsGoingPastANodeRoleItCannotResolveAndReportsIt(t *testing.T) {
	e := setup(t)
	var sw, ev, other counter
	good := e.adopt(t, &sw, &ev)
	bad, err := e.rt.Adopt(ctx(), noderoles.Definition{
		Key: "myapp.bad", Tiers: tiers,
		Duties: []noderoles.Duty{{Name: "d", Schedule: every(t, time.Minute), Run: other.run}},
	}, noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBinding(BindingConfig{Store: failingFor{e.r, "myapp.bad"}, Domain: "d", Clock: e.clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	e.set(t, KeyConcurrency, Global(), 1)
	err = RefreshAll(ctx(), e.rt, b)
	if err == nil || !strings.Contains(err.Error(), "myapp.bad") {
		t.Fatalf("the failing node role must be reported, got %v", err)
	}
	if good.Limits().Concurrency != 1 {
		t.Fatal("one node role failing stopped the others from being refreshed")
	}
	if bad.Limits() != bad.Base() {
		t.Fatal("a node role whose policy could not be read must keep its settings")
	}
}

func TestSilentPolicyDoesNotDisturbABudgetACeilingAlreadyReduced(t *testing.T) {
	e := setupWithDomain(t, governor.Ceiling{Concurrency: 2})
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev) // asks for 4, holds 2
	if rep := e.apply(t, ad); rep.Changed {
		t.Fatalf("policy is silent and the node role runs as adopted; nothing to apply: %+v", rep)
	}
}

func TestAReAdoptedNodeRoleGetsPolicyAgain(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyConcurrency, Global(), 1)
	e.apply(t, ad)
	if err := ad.Close(ctx()); err != nil {
		t.Fatal(err)
	}
	again := e.adopt(t, &sw, &ev)
	if again.Limits().Concurrency != 4 {
		t.Fatalf("premise: a fresh adoption runs as adopted, got %+v", again.Limits())
	}
	if rep := e.apply(t, again); !rep.Changed || again.Limits().Concurrency != 1 {
		t.Fatalf("a re-adopted node role must get policy again: %+v, limits %+v", rep, again.Limits())
	}
}

func TestTheLongestIntervalAnyScopeAsksForWins(t *testing.T) {
	e := setup(t)
	var sw, ev counter
	ad := e.adopt(t, &sw, &ev)
	e.set(t, KeyScheduleInterval, Global(), 120.0)
	e.set(t, KeyScheduleInterval, ForDuty("myapp.sweeper", "sweep"), 300.0)
	got, err := NewResolver(e.r, "d").Resolve(ctx(), ad)
	if err != nil {
		t.Fatal(err)
	}
	if iv := got.Duties["sweep"].Interval; iv != 5*time.Minute {
		t.Fatalf("interval %v, want the longer one (5m)", iv)
	}
	if len(got.Sources[KeyScheduleInterval+"@myapp.sweeper/sweep"]) != 2 {
		t.Fatalf("both instances should be named as sources: %v", got.Sources)
	}
}

func TestPolicyCannotDisableTheRefresherItself(t *testing.T) {
	e := setup(t)
	rd, err := RefreshRole(e.rt, e.b, time.Minute, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.Adopt(ctx(), rd, noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}}); err != nil {
		t.Fatal(err)
	}
	e.set(t, KeyScheduleEnabled, ForDuty(RefreshKey, "refresh"), false)
	e.set(t, KeyConcurrency, ForRole(RefreshKey), 1)
	if err := RefreshAll(ctx(), e.rt, e.b); err != nil {
		t.Fatal(err)
	}
	for _, r := range e.rt.Status() {
		if r.Key != RefreshKey {
			continue
		}
		for _, d := range r.Duties {
			if d.Disabled {
				t.Fatal("policy disabled the node role that applies policy, which could then never be re-enabled by policy")
			}
		}
	}
}
