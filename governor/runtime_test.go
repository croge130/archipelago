package governor

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newGov(t *testing.T, cfg Config) *Governor {
	t.Helper()
	g, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

func role(t *testing.T, g *Governor, key, domain string, l Limits, p Priority) *Role {
	t.Helper()
	r, err := g.Register(RoleSpec{Key: key, Domain: domain, Requested: l, Priority: p})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// gate is work that reports it started and then holds its slot until released.
type gate struct {
	started chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{started: make(chan struct{}, 64), release: make(chan struct{})} }

func (g *gate) run(ctx context.Context) error {
	g.started <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (g *gate) waitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-g.started:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d of %d started", i, n)
		}
	}
}

func mustSubmit(t *testing.T, r *Role, w Work) *Ticket {
	t.Helper()
	tk, err := r.Submit(w)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tk
}

func wait(t *testing.T, tk *Ticket) Result {
	t.Helper()
	res, err := tk.Wait(ctxT(t))
	if err != nil {
		t.Fatalf("ticket never resolved: %v", err)
	}
	return res
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

// track records the most that were ever running at once.
type track struct{ cur, max atomic.Int32 }

func (k *track) enter() {
	n := k.cur.Add(1)
	for {
		m := k.max.Load()
		if n <= m || k.max.CompareAndSwap(m, n) {
			return
		}
	}
}
func (k *track) leave() { k.cur.Add(-1) }

func TestARoleNeverRunsMoreThanItsConcurrencyAndTheRestQueue(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}})
	r := role(t, g, "sweeper", "d", Limits{Concurrency: 2, QueueDepth: 10}, Normal)
	var k track
	var tickets []*Ticket
	for i := 0; i < 8; i++ {
		tickets = append(tickets, mustSubmit(t, r, Work{Duty: "sweep", Run: func(context.Context) error {
			k.enter()
			defer k.leave()
			time.Sleep(15 * time.Millisecond)
			return nil
		}}))
	}
	for _, tk := range tickets {
		if res := wait(t, tk); res.State != Completed {
			t.Fatalf("result %+v", res)
		}
	}
	if k.max.Load() != 2 {
		t.Errorf("at most %d ran at once, want exactly the concurrency of 2", k.max.Load())
	}
}

func TestTheNodeCeilingBindsAllRolesTogether(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 3}})
	var k track
	run := func(context.Context) error { k.enter(); defer k.leave(); time.Sleep(20 * time.Millisecond); return nil }
	var tickets []*Ticket
	for _, name := range []string{"a", "b", "c"} {
		r := role(t, g, name, "d", Limits{Concurrency: 3, QueueDepth: 10}, Normal)
		for i := 0; i < 6; i++ {
			tickets = append(tickets, mustSubmit(t, r, Work{Duty: name, Run: run}))
		}
	}
	for _, tk := range tickets {
		wait(t, tk)
	}
	if got := k.max.Load(); got != 3 {
		t.Errorf("%d ran at once across three roles each allowed 3; the node ceiling is 3", got)
	}
}

func TestADomainShareBindsItsRolesAndLeavesTheRestForOthers(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 6}, Domains: map[string]Ceiling{"small": {Concurrency: 1}}})
	small := role(t, g, "r", "small", Limits{Concurrency: 5, QueueDepth: 10}, Normal)
	big := role(t, g, "r", "big", Limits{Concurrency: 5, QueueDepth: 10}, Normal)
	if small.Limits().Concurrency != 1 || len(small.Clamps()) != 1 || small.Clamps()[0].By != "domain" {
		t.Fatalf("domain clamp: %+v %v", small.Limits(), small.Clamps())
	}
	var ks, kb track
	run := func(k *track) func(context.Context) error {
		return func(context.Context) error { k.enter(); defer k.leave(); time.Sleep(20 * time.Millisecond); return nil }
	}
	var tickets []*Ticket
	for i := 0; i < 5; i++ {
		tickets = append(tickets, mustSubmit(t, small, Work{Duty: "s", Run: run(&ks)}), mustSubmit(t, big, Work{Duty: "b", Run: run(&kb)}))
	}
	for _, tk := range tickets {
		wait(t, tk)
	}
	if ks.max.Load() != 1 {
		t.Errorf("the small domain ran %d at once, its share is 1", ks.max.Load())
	}
	if kb.max.Load() < 2 {
		t.Errorf("the big domain only reached %d: the small one's share must not hold it back", kb.max.Load())
	}
}

func TestRegisterReportsEveryCapAndRefusesDuplicates(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 2, MaxRunTimeout: time.Minute}, Domains: map[string]Ceiling{"d": {Concurrency: 4}}})
	r := role(t, g, "x", "d", Limits{Concurrency: 9, QueueDepth: 1}, Normal)
	if r.Limits().Concurrency != 2 || r.Limits().RunTimeout != time.Minute {
		t.Errorf("limits = %+v", r.Limits())
	}
	dims := map[string]string{}
	for _, c := range r.Clamps() {
		dims[c.Dimension] = c.By
	}
	if dims["concurrency"] != "node" || dims["run timeout"] != "node" {
		t.Errorf("clamps = %v", r.Clamps())
	}
	if _, err := g.Register(RoleSpec{Key: "x", Domain: "d", Requested: Limits{Concurrency: 1}}); !errors.Is(err, ErrDuplicateRole) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := g.Register(RoleSpec{Key: "", Requested: Limits{Concurrency: 1}}); err == nil {
		t.Error("a role with no key was accepted")
	}
	if _, err := g.Register(RoleSpec{Key: "y", Requested: Limits{}}); err == nil {
		t.Error("a role with invalid limits was accepted")
	}
	if _, err := g.Register(RoleSpec{Key: "z", Requested: Limits{Concurrency: 1}, Priority: "loud"}); err == nil {
		t.Error("a role with an unknown priority was accepted")
	}
}

func TestOverflowDropDeferAndCoalesce(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}})
	r := role(t, g, "r", "d", Limits{Concurrency: 1, QueueDepth: 1}, Normal)
	hold := newGate()
	running := mustSubmit(t, r, Work{Duty: "hold", Run: hold.run})
	hold.waitStarted(t, 1)

	queued := mustSubmit(t, r, Work{Duty: "queued", Run: func(context.Context) error { return nil }})
	select {
	case <-queued.Done():
		t.Fatal("the queued work resolved while its role was full")
	default:
	}

	// The queue (depth 1) is now full.
	dropped := mustSubmit(t, r, Work{Duty: "x", Run: func(context.Context) error { return nil }})
	deferred := mustSubmit(t, r, Work{Duty: "y", Overflow: OverflowDefer, Run: func(context.Context) error { return nil }})
	if res := wait(t, dropped); res.State != Shed || res.Reason != ShedQueueFull {
		t.Errorf("drop: %+v", res)
	}
	if res := wait(t, deferred); res.State != Shed || res.Reason != ShedDeferred {
		t.Errorf("defer: %+v", res)
	}

	// Coalescing merges into an identical waiting piece and returns its ticket.
	again := mustSubmit(t, r, Work{Duty: "queued", Overflow: OverflowCoalesce, Run: func(context.Context) error { t.Error("a coalesced run executed"); return nil }})
	if again != queued {
		t.Error("coalescing into a waiting piece must hand back that piece's ticket")
	}
	// With nothing to merge into and no room, coalesce drops.
	other := mustSubmit(t, r, Work{Duty: "other", Overflow: OverflowCoalesce, Run: func(context.Context) error { return nil }})
	if res := wait(t, other); res.State != Shed || res.Reason != ShedQueueFull {
		t.Errorf("coalesce without a match: %+v", res)
	}

	close(hold.release)
	wait(t, running)
	if res := wait(t, queued); res.State != Completed {
		t.Errorf("queued: %+v", res)
	}
	st := g.Stats().Roles[0]
	if st.ShedQueueFull != 2 || st.ShedDeferred != 1 || st.Coalesced != 1 {
		t.Errorf("counters %+v", st.Counters)
	}
}

func TestADepthOfZeroMeansStartAtOnceOrNeverQueue(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}})
	r := role(t, g, "r", "d", Limits{Concurrency: 1, QueueDepth: 0}, Normal)
	hold := newGate()
	first := mustSubmit(t, r, Work{Duty: "a", Run: hold.run})
	hold.waitStarted(t, 1)
	second := mustSubmit(t, r, Work{Duty: "b", Run: func(context.Context) error { return nil }})
	if res := wait(t, second); res.State != Shed {
		t.Errorf("with no queue and no room: %+v", res)
	}
	close(hold.release)
	wait(t, first)
	third := mustSubmit(t, r, Work{Duty: "c", Run: func(context.Context) error { return nil }})
	if res := wait(t, third); res.State != Completed {
		t.Errorf("once there is room: %+v", res)
	}
}

func TestWaitingWorkIsAdmittedMostUrgentFirstThenOldestFirst(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}, AgeStep: time.Hour})
	r := role(t, g, "r", "d", Limits{Concurrency: 1, QueueDepth: 20}, Normal)
	hold := newGate()
	blocker := mustSubmit(t, r, Work{Duty: "hold", Run: hold.run})
	hold.waitStarted(t, 1)

	var mu sync.Mutex
	var order []string
	mk := func(name string, p Priority) *Ticket {
		return mustSubmit(t, r, Work{Duty: name, Priority: p, Run: func(context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}})
	}
	ts := []*Ticket{mk("bg", Background), mk("n1", Normal), mk("crit", Critical), mk("imp", Important), mk("n2", Normal)}
	close(hold.release)
	wait(t, blocker)
	for _, tk := range ts {
		wait(t, tk)
	}
	if got := fmt.Sprint(order); got != "[crit imp n1 n2 bg]" {
		t.Errorf("admission order = %s, want [crit imp n1 n2 bg]", got)
	}
}

func TestWaitingWorkAgesButNeverPastImportant(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}, AgeStep: 40 * time.Millisecond})
	r := role(t, g, "r", "d", Limits{Concurrency: 1, QueueDepth: 20}, Normal)
	hold := newGate()
	blocker := mustSubmit(t, r, Work{Duty: "hold", Run: hold.run})
	hold.waitStarted(t, 1)

	var mu sync.Mutex
	var order []string
	mk := func(name string, p Priority) *Ticket {
		return mustSubmit(t, r, Work{Duty: name, Priority: p, Run: func(context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}})
	}
	old := mk("old-bg", Background)
	time.Sleep(110 * time.Millisecond) // two steps: background has aged to important
	fresh := []*Ticket{mk("fresh-normal", Normal), mk("fresh-crit", Critical)}
	close(hold.release)
	wait(t, blocker)
	wait(t, old)
	for _, tk := range fresh {
		wait(t, tk)
	}
	// Aged background outranks a fresh normal, but critical still goes first.
	if got := fmt.Sprint(order); got != "[fresh-crit old-bg fresh-normal]" {
		t.Errorf("order = %s, want [fresh-crit old-bg fresh-normal]", got)
	}
}

func TestPressureHoldsLowPriorityWorkWithoutKillingAnything(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}, AgeStep: 10 * time.Millisecond})
	r := role(t, g, "r", "d", Limits{Concurrency: 4, QueueDepth: 20}, Normal)

	// Already running when the pressure rises: it must finish untouched.
	hold := newGate()
	runningBG := mustSubmit(t, r, Work{Duty: "running-bg", Priority: Background, Run: hold.run})
	hold.waitStarted(t, 1)

	g.SetPressure(PressureElevated)
	ran := func(name string) (*Ticket, *atomic.Bool) {
		var b atomic.Bool
		return mustSubmit(t, r, Work{Duty: name, Priority: map[string]Priority{"bg": Background, "n": Normal, "imp": Important}[name], Run: func(context.Context) error { b.Store(true); return nil }}), &b
	}
	bgT, bgRan := ran("bg")
	nT, nRan := ran("n")
	impT, impRan := ran("imp")
	wait(t, nT)
	wait(t, impT)
	if !nRan.Load() || !impRan.Load() {
		t.Error("normal and important work should run at elevated pressure")
	}
	// Background is held, and waiting does not defeat the cutoff: let it age
	// well past several steps and it must still be held.
	time.Sleep(80 * time.Millisecond)
	// Aging is only looked at when something is dispatched, so cause a
	// dispatch now that the held work has aged, and check it still stays held.
	poke, _ := ran("imp")
	wait(t, poke)
	select {
	case <-bgT.Done():
		t.Fatal("background work ran at elevated pressure (aging must not lift the cutoff)")
	case <-time.After(100 * time.Millisecond):
	}

	g.SetPressure(PressureHigh)
	nT2, n2Ran := ran("n")
	time.Sleep(50 * time.Millisecond)
	if n2Ran.Load() {
		t.Error("normal work ran at high pressure")
	}
	impT2, _ := ran("imp")
	wait(t, impT2)

	select {
	case <-runningBG.Done():
		t.Fatal("raising the pressure ended work that was already running")
	default:
	}

	g.SetPressure(PressureNone)
	wait(t, bgT)
	wait(t, nT2)
	if !bgRan.Load() || !n2Ran.Load() {
		t.Error("held work did not run once the pressure dropped")
	}
	close(hold.release)
	wait(t, runningBG)
}

func TestARateLimitSpacesStartsAndNeverBlocksOtherRoles(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 10}})
	limited := role(t, g, "limited", "d", Limits{Concurrency: 5, QueueDepth: 10, Rate: Rate{Starts: 2, Per: 200 * time.Millisecond}}, Normal)
	free := role(t, g, "free", "d", Limits{Concurrency: 5, QueueDepth: 10}, Normal)

	var mu sync.Mutex
	var starts []time.Time
	var tickets []*Ticket
	t0 := time.Now()
	for i := 0; i < 4; i++ {
		tickets = append(tickets, mustSubmit(t, limited, Work{Duty: "l", Run: func(context.Context) error {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			return nil
		}}))
	}
	// A role with no rate limit is not held up behind the limited one's queue.
	if res := wait(t, mustSubmit(t, free, Work{Duty: "f", Run: func(context.Context) error { return nil }})); res.State != Completed || time.Since(t0) > 150*time.Millisecond {
		t.Errorf("an unlimited role was held behind a rate-limited one: %+v after %v", res, time.Since(t0))
	}
	for _, tk := range tickets {
		wait(t, tk)
	}
	if len(starts) != 4 {
		t.Fatalf("%d started", len(starts))
	}
	if gap := starts[2].Sub(starts[0]); gap < 190*time.Millisecond {
		t.Errorf("third start came %v after the first; 2 per 200ms allows it no sooner than 200ms", gap)
	}
	if starts[1].Sub(starts[0]) > 100*time.Millisecond {
		t.Errorf("the second start waited %v though the rate allowed it", starts[1].Sub(starts[0]))
	}
}

func TestARunTimeoutIsEnforcedByContext(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 2}})
	r := role(t, g, "r", "d", Limits{Concurrency: 1, RunTimeout: 60 * time.Millisecond}, Normal)
	res := wait(t, mustSubmit(t, r, Work{Duty: "slow", Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}))
	if res.State != Failed || !errors.Is(res.Err, context.DeadlineExceeded) || res.Ran > time.Second {
		t.Errorf("result %+v", res)
	}
	// A node-level MaxRunTimeout reaches a role that asked for none.
	g2 := newGov(t, Config{Node: Ceiling{Concurrency: 2, MaxRunTimeout: 60 * time.Millisecond}})
	r2 := role(t, g2, "r", "d", Limits{Concurrency: 1}, Normal)
	if res := wait(t, mustSubmit(t, r2, Work{Duty: "slow", Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }})); res.State != Failed {
		t.Errorf("the node's timeout cap did not apply: %+v", res)
	}
}

func TestAPanickingDutyIsAFailureNotACrash(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 2}})
	r := role(t, g, "r", "d", Limits{Concurrency: 1}, Normal)
	res := wait(t, mustSubmit(t, r, Work{Duty: "boom", Run: func(context.Context) error { panic("oops") }}))
	if res.State != Failed || res.Err == nil {
		t.Errorf("result %+v", res)
	}
	// And its slot was released.
	if res := wait(t, mustSubmit(t, r, Work{Duty: "next", Run: func(context.Context) error { return nil }})); res.State != Completed {
		t.Errorf("the role is wedged after a panic: %+v", res)
	}
}

func TestCloseShedsTheQueueCancelsTheRunningAndRefusesMore(t *testing.T) {
	g, err := New(Config{Node: Ceiling{Concurrency: 5}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := g.Register(RoleSpec{Key: "r", Requested: Limits{Concurrency: 1, QueueDepth: 5}})
	hold := newGate()
	running := mustSubmit(t, r, Work{Duty: "run", Run: hold.run})
	hold.waitStarted(t, 1)
	queued := mustSubmit(t, r, Work{Duty: "queued", Run: func(context.Context) error { return nil }})

	g.Close()
	g.Close() // idempotent
	if res := wait(t, queued); res.State != Shed || res.Reason != ShedClosed {
		t.Errorf("queued at close: %+v", res)
	}
	if res := wait(t, running); res.State != Cancelled {
		t.Errorf("running at close: %+v", res)
	}
	if _, err := r.Submit(Work{Duty: "late", Run: func(context.Context) error { return nil }}); !errors.Is(err, ErrClosed) {
		t.Errorf("Submit after Close: %v", err)
	}
	if _, err := g.Register(RoleSpec{Key: "new", Requested: Limits{Concurrency: 1}}); !errors.Is(err, ErrClosed) {
		t.Errorf("Register after Close: %v", err)
	}
}

func TestUnregisterShedsOnlyThatRolesWaitingWork(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 1}})
	a := role(t, g, "a", "d", Limits{Concurrency: 1, QueueDepth: 5}, Normal)
	b := role(t, g, "b", "d", Limits{Concurrency: 1, QueueDepth: 5}, Normal)
	hold := newGate()
	running := mustSubmit(t, a, Work{Duty: "run", Run: hold.run})
	hold.waitStarted(t, 1)
	aWaiting := mustSubmit(t, a, Work{Duty: "aw", Run: func(context.Context) error { return nil }})
	bWaiting := mustSubmit(t, b, Work{Duty: "bw", Run: func(context.Context) error { return nil }})

	a.Unregister()
	if res := wait(t, aWaiting); res.State != Shed || res.Reason != ShedRemoved {
		t.Errorf("removed role's waiting work: %+v", res)
	}
	close(hold.release)
	wait(t, running)
	if res := wait(t, bWaiting); res.State != Completed {
		t.Errorf("another role's waiting work: %+v", res)
	}
	if _, err := a.Submit(Work{Duty: "x", Run: func(context.Context) error { return nil }}); err == nil {
		t.Error("a removed role accepted work")
	}
}

func TestSubmitRejectsMisuse(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 1}})
	r := role(t, g, "r", "d", Limits{Concurrency: 1}, Normal)
	if _, err := r.Submit(Work{Duty: "no run"}); err == nil {
		t.Error("work with no Run was accepted")
	}
	if _, err := r.Submit(Work{Duty: "x", Priority: "loud", Run: func(context.Context) error { return nil }}); err == nil {
		t.Error("work with an unknown priority was accepted")
	}
}

// Randomized pressure on every invariant at once, under the race detector:
// no level is ever over its cap, every ticket resolves, and the counters
// account for every submission.
func TestStressNeverExceedsAnyLevelAndAccountsForEverything(t *testing.T) {
	g := newGov(t, Config{Node: Ceiling{Concurrency: 4}, Domains: map[string]Ceiling{"d1": {Concurrency: 2}}, AgeStep: 5 * time.Millisecond})
	type rr struct {
		r   *Role
		trk *track
		cap int32
	}
	var roles []rr
	var node, d1 track
	for i, spec := range []struct {
		domain string
		l      Limits
	}{
		{"d1", Limits{Concurrency: 2, QueueDepth: 5}}, {"d1", Limits{Concurrency: 1, QueueDepth: 0}},
		{"d2", Limits{Concurrency: 3, QueueDepth: 8, Rate: Rate{Starts: 20, Per: 50 * time.Millisecond}}}, {"d2", Limits{Concurrency: 1, QueueDepth: 3}},
	} {
		r := role(t, g, fmt.Sprintf("r%d", i), spec.domain, spec.l, Priority([]Priority{Background, Normal, Important, Critical}[i]))
		roles = append(roles, rr{r: r, trk: &track{}, cap: int32(r.Limits().Concurrency)})
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var tickets []*Ticket
	var submitted atomic.Int64
	stop := time.Now().Add(600 * time.Millisecond)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for time.Now().Before(stop) {
				x := roles[rng.Intn(len(roles))]
				isD1 := x.r.spec.Domain == "d1"
				ov := Overflow(rng.Intn(3))
				tk, err := x.r.Submit(Work{Duty: fmt.Sprint("d", rng.Intn(3)), Overflow: ov, Priority: []Priority{"", Background, Normal, Important, Critical}[rng.Intn(5)],
					Run: func(ctx context.Context) error {
						x.trk.enter()
						node.enter()
						if isD1 {
							d1.enter()
						}
						defer func() {
							x.trk.leave()
							node.leave()
							if isD1 {
								d1.leave()
							}
						}()
						select {
						case <-time.After(time.Duration(rand.Intn(4)) * time.Millisecond):
						case <-ctx.Done():
						}
						if rand.Intn(10) == 0 {
							return errors.New("sometimes fails")
						}
						return nil
					}})
				if err != nil {
					t.Errorf("Submit: %v", err)
					return
				}
				submitted.Add(1)
				mu.Lock()
				tickets = append(tickets, tk)
				mu.Unlock()
				if rng.Intn(50) == 0 {
					g.SetPressure(Pressure(rng.Intn(3)))
				}
				time.Sleep(time.Duration(rng.Intn(300)) * time.Microsecond)
			}
		}(int64(w))
	}
	wg.Wait()
	g.SetPressure(PressureNone) // let anything held drain

	ctx := ctxT(t)
	for _, tk := range tickets {
		if _, err := tk.Wait(ctx); err != nil {
			t.Fatalf("a ticket never resolved: %v (stats %+v)", err, g.Stats())
		}
	}
	if node.max.Load() > 4 {
		t.Errorf("node ran %d at once, ceiling 4", node.max.Load())
	}
	if d1.max.Load() > 2 {
		t.Errorf("domain d1 ran %d at once, share 2", d1.max.Load())
	}
	for _, x := range roles {
		if x.trk.max.Load() > x.cap {
			t.Errorf("role %s ran %d at once, limit %d", x.r.spec.Key, x.trk.max.Load(), x.cap)
		}
	}
	// Every submission is accounted for: started, shed, or merged into another.
	var started, shed, merged, sub uint64
	for _, rs := range g.Stats().Roles {
		started += rs.Started
		shed += rs.ShedQueueFull + rs.ShedDeferred + rs.ShedOther
		merged += rs.Coalesced
		sub += rs.Submitted
		if rs.Running != 0 || rs.Waiting != 0 {
			t.Errorf("%s still has running=%d waiting=%d after everything resolved", rs.Key, rs.Running, rs.Waiting)
		}
		if rs.Completed+rs.Failed+rs.Cancelled != rs.Started {
			t.Errorf("%s: finished %d != started %d", rs.Key, rs.Completed+rs.Failed+rs.Cancelled, rs.Started)
		}
	}
	if sub != uint64(submitted.Load()) || started+shed+merged != sub {
		t.Errorf("accounting: submitted %d (counted %d), started %d + shed %d + coalesced %d", sub, submitted.Load(), started, shed, merged)
	}
	if started == 0 {
		t.Error("the stress run started nothing")
	}
}
