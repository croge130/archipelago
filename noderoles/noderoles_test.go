package noderoles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type env struct {
	gov *governor.Governor
	clk *scheduler.ManualClock
	rt  *Runtime
	rec *scheduler.MemoryRecord
}

func setup(t *testing.T, node governor.Ceiling) *env {
	t.Helper()
	gov, err := governor.New(governor.Config{Node: node, Domains: map[string]governor.Ceiling{"d": {Concurrency: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	clk := scheduler.NewManualClock(t0)
	rec := scheduler.NewMemoryRecord()
	rt, err := New(Config{Governor: gov, Domain: "d", Record: rec, Clock: clk, Rand: func() float64 { return 0 }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()); gov.Close() })
	return &env{gov: gov, clk: clk, rt: rt, rec: rec}
}

func every(t *testing.T, d time.Duration) recurrence.Schedule {
	t.Helper()
	s, err := recurrence.Every(d, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var tiers = governor.TierTable{
	governor.TierLow:  {Concurrency: 1, QueueDepth: 0},
	governor.TierHigh: {Concurrency: 4, QueueDepth: 8},
}

func tierLow() Adoption { return Adoption{Budget: governor.Adoption{Tier: governor.TierLow}} }

// counter is a duty body that records how often it ran.
type counter struct{ n atomic.Int32 }

func (c *counter) run(context.Context, scheduler.Fire) error { c.n.Add(1); return nil }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// tick moves the clock one minute and waits for every run it caused to end, so
// the next minute does not overlap a run that completes asynchronously.
func (e *env) tick(t *testing.T) {
	t.Helper()
	e.clk.Advance(time.Minute)
	waitFor(t, "runs to finish", func() bool {
		st := e.gov.Stats()
		return st.Running == 0 && st.Waiting == 0
	})
	time.Sleep(5 * time.Millisecond) // the scheduler clears its own in-flight count on a goroutine
}

func def(key string, c *counter, schedule recurrence.Schedule) Definition {
	return Definition{
		Key: key, Tiers: tiers, Default: &governor.Limits{Concurrency: 2, QueueDepth: 2}, Priority: governor.Normal,
		Duties: []Duty{{Name: "work", Schedule: schedule, Run: c.run}},
	}
}

func TestAnAdoptedNodeRolesDutiesRunThroughTheGovernorOnSchedule(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var c counter
	ad, err := e.rt.Adopt(context.Background(), def("myapp.sweeper", &c, every(t, time.Minute)), tierLow())
	if err != nil {
		t.Fatal(err)
	}
	if ad.Limits.Concurrency != 1 || len(ad.Clamps) != 0 {
		t.Errorf("limits %+v clamps %v", ad.Limits, ad.Clamps)
	}
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		e.tick(t)
	}
	waitFor(t, "three runs", func() bool { return c.n.Load() == 3 })

	st := e.rt.Status()[0]
	if st.Key != "myapp.sweeper" || st.Governor.Completed != 3 || len(st.Duties) != 1 || st.Duties[0].Completed != 3 {
		t.Errorf("status %+v", st)
	}
}

func TestAdoptionMustStateABudgetAndAFailureTouchesNothingElse(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var good, bad counter
	if _, err := e.rt.Adopt(context.Background(), def("myapp.good", &good, every(t, time.Minute)), tierLow()); err != nil {
		t.Fatal(err)
	}
	_, err := e.rt.Adopt(context.Background(), def("myapp.bad", &bad, every(t, time.Minute)), Adoption{})
	if !errors.Is(err, governor.ErrNoBudgetStated) {
		t.Fatalf("an adoption stating no budget: %v", err)
	}
	if _, err := e.rt.Adopt(context.Background(), def("myapp.bad", &bad, every(t, time.Minute)), Adoption{Budget: governor.Adoption{Tier: "extreme"}}); !errors.Is(err, governor.ErrUnknownTier) {
		t.Errorf("a tier the node role never defined: %v", err)
	}
	// The failed node role left nothing registered, so it can be adopted properly now.
	if _, err := e.rt.Adopt(context.Background(), def("myapp.bad", &bad, every(t, time.Minute)), Adoption{Budget: governor.Adoption{UseDefault: true}}); err != nil {
		t.Errorf("re-adopting after a refused adoption: %v", err)
	}
	e.rt.Start()
	e.clk.Advance(time.Minute)
	waitFor(t, "both roles to run", func() bool { return good.n.Load() == 1 && bad.n.Load() == 1 })
}

func TestTheNodeCeilingCapsABudgetAndTheAdoptionSaysSo(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 2})
	var c counter
	ad, err := e.rt.Adopt(context.Background(), def("myapp.big", &c, every(t, time.Minute)), Adoption{Budget: governor.Adoption{Tier: governor.TierHigh}})
	if err != nil {
		t.Fatal(err)
	}
	if ad.Limits.Concurrency != 2 || len(ad.Clamps) != 1 || ad.Clamps[0].By != "node" {
		t.Errorf("limits %+v clamps %v: tier high asks for 4, the node allows 2", ad.Limits, ad.Clamps)
	}
	if st := e.rt.Status()[0]; len(st.Clamps) != 1 {
		t.Errorf("status does not surface the clamp: %+v", st.Clamps)
	}
}

func TestADutyThatMeetsAFullBudgetIsShedOrDeferredAsItDeclared(t *testing.T) {
	for _, c := range []struct {
		name     string
		overflow governor.Overflow
		wantDrop uint64
		wantDef  uint64
	}{{"drop", governor.OverflowDrop, 1, 0}, {"defer", governor.OverflowDefer, 0, 1}} {
		t.Run(c.name, func(t *testing.T) {
			e := setup(t, governor.Ceiling{Concurrency: 4})
			started, release := make(chan struct{}, 4), make(chan struct{})
			d := Definition{Key: "myapp.slow", Tiers: tiers, Duties: []Duty{{
				Name: "work", Schedule: every(t, time.Minute), Overlap: scheduler.OverlapAllow, Overflow: c.overflow, DeferRetry: time.Hour,
				Run: func(ctx context.Context, f scheduler.Fire) error {
					started <- struct{}{}
					select {
					case <-release:
					case <-ctx.Done():
					}
					return nil
				}}}}
			if _, err := e.rt.Adopt(context.Background(), d, tierLow()); err != nil { // concurrency 1, no queue
				t.Fatal(err)
			}
			e.rt.Start()
			e.clk.Advance(time.Minute)
			<-started
			e.clk.Advance(time.Minute) // second run due while the first holds the only slot
			waitFor(t, "the second offer to be refused", func() bool {
				ds := e.rt.Status()[0].Duties[0]
				return ds.Dropped == c.wantDrop && ds.Deferred == c.wantDef
			})
			close(release)
		})
	}
}

func TestAuthorizationIsCheckedAtAdoptionAndBeforeEveryRun(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var allowed atomic.Bool
	var asked []string
	var mu sync.Mutex
	auth := func(_ context.Context, perms []string) error {
		mu.Lock()
		asked = perms
		mu.Unlock()
		if allowed.Load() {
			return nil
		}
		return errors.New("no grant")
	}
	var c counter
	d := def("myapp.audited", &c, every(t, time.Minute))
	d.Permissions = []string{"myapp.audit.write", "myapp.audit.read"}
	a := tierLow()
	a.Authorize = auth

	_, err := e.rt.Adopt(context.Background(), d, a)
	if !errors.Is(err, ErrNotAuthorized) || !strings.Contains(err.Error(), "myapp.audit.write") {
		t.Fatalf("adopting without the permissions: %v (want it to fail closed and name what it needs)", err)
	}
	allowed.Store(true)
	if _, err := e.rt.Adopt(context.Background(), d, a); err != nil {
		t.Fatalf("adopting with them: %v", err)
	}
	mu.Lock()
	got := fmt.Sprint(asked)
	mu.Unlock()
	if got != "[myapp.audit.write myapp.audit.read]" {
		t.Errorf("the check was given %s", got)
	}
	e.rt.Start()
	e.clk.Advance(time.Minute)
	waitFor(t, "a run", func() bool { return c.n.Load() == 1 })

	allowed.Store(false) // the grant is revoked after adoption
	e.clk.Advance(time.Minute)
	waitFor(t, "the refused run to be counted", func() bool { return e.rt.Status()[0].Unauthorized == 1 })
	if c.n.Load() != 1 {
		t.Errorf("a duty ran after the node lost its authority (%d runs)", c.n.Load())
	}
	waitFor(t, "the governor to record the refusal as a failed run", func() bool { return e.rt.Status()[0].Governor.Failed == 1 })
	if last, _, _ := e.rec.LastRun(context.Background(), "myapp.audited", "work"); last.After(t0.Add(time.Minute)) {
		t.Errorf("a run that was refused was recorded as done at %v", last)
	}
}

type fakeGate struct {
	mu   sync.Mutex
	held bool
	lost chan struct{}
}

func (g *fakeGate) Acquire() (<-chan struct{}, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lost, g.held
}
func (g *fakeGate) lose() { g.mu.Lock(); g.held = false; close(g.lost); g.mu.Unlock() }

func TestAnExclusiveNodeRoleNeedsAGateRunsOnlyWhileHeldAndStopsWhenItIsLost(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	started := make(chan struct{}, 2)
	ctxEnded := make(chan error, 1)
	d := Definition{Key: "myapp.leader", Exclusive: true, Tiers: tiers, Duties: []Duty{{
		Name: "lead", Schedule: every(t, time.Minute), Run: func(ctx context.Context, f scheduler.Fire) error {
			started <- struct{}{}
			<-ctx.Done()
			ctxEnded <- ctx.Err()
			return ctx.Err()
		}}}}
	if _, err := e.rt.Adopt(context.Background(), d, tierLow()); !errors.Is(err, ErrNeedsGate) {
		t.Fatalf("an exclusive node role with no gate: %v", err)
	}

	gate := &fakeGate{lost: make(chan struct{})}
	a := tierLow()
	a.Gate = gate
	if _, err := e.rt.Adopt(context.Background(), d, a); err != nil {
		t.Fatal(err)
	}
	e.rt.Start()
	e.clk.Advance(time.Minute) // not held: the duty must not run
	waitFor(t, "the not-held skip", func() bool { return e.rt.Status()[0].NotHeld == 1 })
	select {
	case <-started:
		t.Fatal("an exclusive duty ran on a node that does not hold it")
	default:
	}

	gate.mu.Lock()
	gate.held = true
	gate.mu.Unlock()
	e.clk.Advance(time.Minute)
	<-started
	gate.lose() // the lease is lost mid-run
	select {
	case err := <-ctxEnded:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("run context ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("losing the lease did not cancel the run in progress")
	}
}

func TestSetupAndTeardownBracketTheNodeRole(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var events []string
	var mu sync.Mutex
	note := func(s string) { mu.Lock(); events = append(events, s); mu.Unlock() }
	var c counter
	d := def("myapp.bracketed", &c, every(t, time.Minute))
	d.Setup = func(context.Context) error { note("setup"); return nil }
	d.Teardown = func(context.Context) error { note("teardown"); return nil }
	ad, err := e.rt.Adopt(context.Background(), d, tierLow())
	if err != nil {
		t.Fatal(err)
	}
	if err := ad.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	ad.Close(context.Background()) // idempotent
	mu.Lock()
	got := fmt.Sprint(events)
	mu.Unlock()
	if got != "[setup teardown]" {
		t.Errorf("events %s", got)
	}
	// Closed means gone: not scheduled, not in the governor, adoptable again.
	e.rt.Start()
	e.clk.Advance(5 * time.Minute)
	if c.n.Load() != 0 || len(e.rt.Status()) != 0 {
		t.Errorf("a closed node role still runs (%d runs, %d roles)", c.n.Load(), len(e.rt.Status()))
	}
	if _, err := e.rt.Adopt(context.Background(), d, tierLow()); err != nil {
		t.Errorf("re-adopting a closed node role: %v", err)
	}
}

func TestAFailedSetupLeavesNothingBehind(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var c counter
	d := def("myapp.broken", &c, every(t, time.Minute))
	d.Setup = func(context.Context) error { return errors.New("cannot start") }
	torn := false
	d.Teardown = func(context.Context) error { torn = true; return nil }
	if _, err := e.rt.Adopt(context.Background(), d, tierLow()); err == nil {
		t.Fatal("a failed Setup was accepted")
	}
	if torn {
		t.Error("Teardown ran for a node role whose Setup never succeeded")
	}
	d.Setup = nil
	if _, err := e.rt.Adopt(context.Background(), d, tierLow()); err != nil {
		t.Errorf("the failed setup left the node role registered: %v", err)
	}
}

func TestAnAdoptionCanReplaceADutysSchedule(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var c counter
	d := def("myapp.tuned", &c, every(t, time.Hour))
	a := tierLow()
	a.Schedules = map[string]recurrence.Schedule{"work": every(t, time.Minute)}
	if _, err := e.rt.Adopt(context.Background(), d, a); err != nil {
		t.Fatal(err)
	}
	e.rt.Start()
	for i := 0; i < 3; i++ {
		e.tick(t)
	}
	waitFor(t, "the overridden schedule", func() bool { return c.n.Load() == 3 })

	a.Schedules = map[string]recurrence.Schedule{"nosuchduty": every(t, time.Minute)}
	if _, err := e.rt.Adopt(context.Background(), def("myapp.other", &c, every(t, time.Hour)), a); err == nil {
		t.Error("a schedule for a duty the node role does not have was accepted")
	}
}

func TestEventDutiesAndPressureWorkThroughTheRuntime(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var onStart, bg, imp counter
	if _, err := e.rt.Adopt(context.Background(), Definition{Key: "myapp.events", Tiers: tiers,
		Duties: []Duty{{Name: "boot", Events: []string{scheduler.EventStart}, Run: onStart.run}}}, tierLow()); err != nil {
		t.Fatal(err)
	}
	bgDef := def("myapp.background", &bg, every(t, time.Minute))
	bgDef.Priority = governor.Background
	impDef := def("myapp.important", &imp, every(t, time.Minute))
	impDef.Priority = governor.Important
	// Held work waits in the node role's queue, so it needs room in it.
	useDefault := Adoption{Budget: governor.Adoption{UseDefault: true}}
	for _, d := range []Definition{bgDef, impDef} {
		if _, err := e.rt.Adopt(context.Background(), d, useDefault); err != nil {
			t.Fatal(err)
		}
	}
	e.rt.Start()
	waitFor(t, "the start event duty", func() bool { return onStart.n.Load() == 1 })

	e.gov.SetPressure(governor.PressureElevated) // the application is strained
	e.clk.Advance(time.Minute)
	waitFor(t, "the important duty", func() bool { return imp.n.Load() == 1 })
	time.Sleep(60 * time.Millisecond)
	if bg.n.Load() != 0 {
		t.Fatal("a background node role ran at elevated pressure")
	}
	e.gov.SetPressure(governor.PressureNone)
	waitFor(t, "the held background duty to run once the pressure drops", func() bool { return bg.n.Load() == 1 })
}

func TestValidationAndDuplicates(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var c counter
	ctx := context.Background()
	for name, d := range map[string]Definition{
		"no key":         {Duties: []Duty{{Name: "w", Schedule: every(t, time.Minute), Run: c.run}}},
		"no duties":      {Key: "k"},
		"unnamed duty":   {Key: "k", Duties: []Duty{{Schedule: every(t, time.Minute), Run: c.run}}},
		"no run":         {Key: "k", Duties: []Duty{{Name: "w", Schedule: every(t, time.Minute)}}},
		"duplicate duty": {Key: "k", Duties: []Duty{{Name: "w", Schedule: every(t, time.Minute), Run: c.run}, {Name: "w", Schedule: every(t, time.Minute), Run: c.run}}},
	} {
		if _, err := e.rt.Adopt(ctx, d, Adoption{Budget: governor.Adoption{Override: &governor.Limits{Concurrency: 1}}}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := e.rt.Adopt(ctx, def("dup", &c, every(t, time.Minute)), tierLow()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.Adopt(ctx, def("dup", &c, every(t, time.Minute)), tierLow()); !errors.Is(err, ErrAlreadyAdopted) {
		t.Errorf("duplicate: %v", err)
	}
	e.rt.Stop(ctx)
	e.rt.Stop(ctx) // idempotent
	if _, err := e.rt.Adopt(ctx, def("late", &c, every(t, time.Minute)), tierLow()); !errors.Is(err, ErrClosed) {
		t.Errorf("Adopt after Stop: %v", err)
	}
	if _, err := New(Config{}); err == nil {
		t.Error("a runtime with no governor was accepted")
	}
}

func TestAPanickingDutyIsAFailureAndTheScheduleCarriesOn(t *testing.T) {
	e := setup(t, governor.Ceiling{Concurrency: 4})
	var n atomic.Int32
	d := Definition{Key: "myapp.flaky", Tiers: tiers, Duties: []Duty{{Name: "w", Schedule: every(t, time.Minute),
		Run: func(context.Context, scheduler.Fire) error {
			if n.Add(1) == 1 {
				panic("boom")
			}
			return nil
		}}}}
	e.rt.Adopt(context.Background(), d, tierLow())
	e.rt.Start()
	e.clk.Advance(time.Minute)
	waitFor(t, "the first run to fail", func() bool { return e.rt.Status()[0].Governor.Failed == 1 })
	e.clk.Advance(time.Minute)
	waitFor(t, "the next run", func() bool { return e.rt.Status()[0].Governor.Completed == 1 })
}
