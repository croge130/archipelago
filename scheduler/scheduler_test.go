package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/croge130/archipelago/recurrence"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// recorder is a Dispatcher for tests: it runs each offered execution at once,
// on the calling goroutine, and lets the test decide the disposition and keep
// accepted runs "in flight" until released.
type recorder struct {
	mu       sync.Mutex
	fires    []Fire
	slots    []time.Time
	disp     func(n int) Disposition // by 1-based offer count; default Accepted
	hold     bool                    // keep accepted runs in flight until release()
	pending  []chan struct{}
	offers   int
	failWith error
	started  []string
}

func (r *recorder) dispatcher(f *Fire) Dispatcher {
	return func(role, duty string, run func(ctx context.Context) error) Handle {
		r.mu.Lock()
		r.offers++
		n := r.offers
		d := Accepted
		if r.disp != nil {
			d = r.disp(n)
		}
		hold := r.hold
		r.mu.Unlock()
		if d != Accepted {
			return Handle{Disposition: d}
		}
		_ = run(context.Background())
		done := make(chan struct{})
		if hold {
			r.mu.Lock()
			r.pending = append(r.pending, done)
			r.mu.Unlock()
		} else {
			close(done)
		}
		return Handle{Disposition: Accepted, Done: done}
	}
}

func (r *recorder) release(n int) {
	r.mu.Lock()
	var rel []chan struct{}
	for i := 0; i < n && len(r.pending) > 0; i++ {
		rel = append(rel, r.pending[0])
		r.pending = r.pending[1:]
	}
	r.mu.Unlock()
	for _, c := range rel {
		close(c)
	}
	time.Sleep(20 * time.Millisecond) // let the waiter goroutines observe it
}

func (r *recorder) onFire(ctx context.Context, f Fire) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fires = append(r.fires, f)
	return r.failWith
}

func (r *recorder) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.fires) }

func (r *recorder) snapshot() []Fire {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Fire(nil), r.fires...)
}

type env struct {
	clk *ManualClock
	rec *recorder
	rcd *MemoryRecord
	s   *Scheduler
}

func setup(t *testing.T, mut ...func(*Config)) *env {
	t.Helper()
	e := &env{clk: NewManualClock(t0), rec: &recorder{}, rcd: NewMemoryRecord()}
	cfg := Config{Clock: e.clk, Record: e.rcd, Rand: func() float64 { return 0 }}
	cfg.Dispatch = e.rec.dispatcher(nil)
	for _, m := range mut {
		m(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.s = s
	t.Cleanup(s.Stop)
	return e
}

func every(t *testing.T, d time.Duration) recurrence.Schedule {
	t.Helper()
	s, err := recurrence.Every(d, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *env) add(t *testing.T, d Duty) {
	t.Helper()
	if d.Role == "" {
		d.Role = "role"
	}
	if d.Name == "" {
		d.Name = "duty"
	}
	if d.Run == nil {
		d.Run = e.rec.onFire
	}
	if err := e.s.Add(d); err != nil {
		t.Fatal(err)
	}
}

func (e *env) start(t *testing.T) {
	t.Helper()
	if err := e.s.Start(); err != nil {
		t.Fatal(err)
	}
}

func slotsOf(fs []Fire) []string {
	var out []string
	for _, f := range fs {
		if f.Event != "" {
			out = append(out, "event:"+f.Event)
			continue
		}
		tag := f.Slot.Format("15:04:05")
		if f.CatchUp {
			tag += "*"
		}
		out = append(out, tag)
	}
	return out
}

func eq(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s:\n got %v\nwant %v", label, got, want)
	}
}

func TestAScheduledDutyFiresOnEachSlotAndNothingBetween(t *testing.T) {
	e := setup(t)
	e.add(t, Duty{Schedule: every(t, time.Minute)})
	e.start(t)
	e.clk.Advance(59 * time.Second)
	if e.rec.count() != 0 {
		t.Fatal("fired before its first slot")
	}
	e.clk.Advance(2*time.Minute + time.Second)
	eq(t, "slots", slotsOf(e.rec.snapshot()), "00:01:00", "00:02:00", "00:03:00")
	if st := e.s.Stats()[0]; st.Fired != 3 || st.Completed != 3 || !st.NextSlot.Equal(t0.Add(4*time.Minute)) {
		t.Errorf("stats %+v", st)
	}
}

func TestACalendarDutyUsesTheWallClockOfItsLocation(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	cal, err := recurrence.Calendar("0 9 * * *", ny)
	if err != nil {
		t.Fatal(err)
	}
	e := setup(t) // clock starts 2026-01-01 00:00 UTC = Dec 31 19:00 New York
	e.add(t, Duty{Schedule: cal})
	e.start(t)
	e.clk.Advance(48 * time.Hour)
	var got []string
	for _, f := range e.rec.snapshot() {
		got = append(got, f.Slot.In(ny).Format("01-02 15:04"))
	}
	eq(t, "calendar slots", got, "01-01 09:00", "01-02 09:00")
}

func TestJitterDelaysTheRunButNeverShiftsTheSlotsOrAccumulates(t *testing.T) {
	e := setup(t, func(c *Config) { c.Rand = func() float64 { return 0.5 } })
	e.add(t, Duty{Schedule: every(t, time.Minute), Jitter: 20 * time.Second})
	e.start(t)
	e.clk.Advance(time.Minute + 9*time.Second)
	if e.rec.count() != 0 {
		t.Fatal("fired before slot + jitter (10s)")
	}
	e.clk.Advance(2 * time.Second)
	if e.rec.count() != 1 || !e.rec.snapshot()[0].Slot.Equal(t0.Add(time.Minute)) {
		t.Fatalf("first run: %v", slotsOf(e.rec.snapshot()))
	}
	// The second slot is a minute after the first slot, not after the first run.
	e.clk.Advance(time.Minute)
	if e.rec.count() != 2 || !e.rec.snapshot()[1].Slot.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("jitter accumulated: %v", slotsOf(e.rec.snapshot()))
	}
}

func TestOverlapSkipQueueOneAndAllow(t *testing.T) {
	cases := []struct {
		name    string
		overlap Overlap
		// after three slots with the first run held in flight, then released
		wantRunsWhileHeld  int
		wantRunsAfterFree  int
		wantSkipped, wantQ uint64
	}{
		{"skip", OverlapSkip, 1, 1, 2, 0},
		{"queue one", OverlapQueueOne, 1, 2, 0, 2},
		{"allow", OverlapAllow, 3, 3, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := setup(t)
			e.rec.hold = true
			e.add(t, Duty{Schedule: every(t, time.Minute), Overlap: c.overlap})
			e.start(t)
			e.clk.Advance(3*time.Minute + time.Second) // slots 1, 2, 3 due; slot 1 never finishes meanwhile
			if got := e.rec.count(); got != c.wantRunsWhileHeld {
				t.Errorf("runs while the first is in flight = %d, want %d", got, c.wantRunsWhileHeld)
			}
			e.rec.release(10)
			if got := e.rec.count(); got != c.wantRunsAfterFree {
				t.Errorf("runs after it ends = %d, want %d (%v)", got, c.wantRunsAfterFree, slotsOf(e.rec.snapshot()))
			}
			st := e.s.Stats()[0]
			if st.SkippedOverlap != c.wantSkipped || st.Queued != c.wantQ {
				t.Errorf("stats skipped=%d queued=%d, want %d and %d", st.SkippedOverlap, st.Queued, c.wantSkipped, c.wantQ)
			}
			if c.overlap == OverlapQueueOne {
				// The remembered occurrence is the latest one, not the first.
				if last := e.rec.snapshot()[len(e.rec.snapshot())-1]; !last.Slot.Equal(t0.Add(3 * time.Minute)) {
					t.Errorf("the queued run stood for %v, want the latest slot", last.Slot)
				}
			}
		})
	}
}

func TestCatchUpPolicies(t *testing.T) {
	missedSetup := func(t *testing.T, policy CatchUp, max int) *env {
		e := setup(t)
		// The duty last finished the 10:00 slot; the node comes back at 13:30.
		e.rcd.SetLastRun(context.Background(), "role", "duty", t0.Add(10*time.Hour))
		e.clk.Advance(13*time.Hour + 30*time.Minute)
		s, _ := recurrence.Every(time.Hour, t0)
		e.add(t, Duty{Schedule: s, CatchUp: policy, MaxCatchUp: max})
		e.start(t)
		time.Sleep(50 * time.Millisecond) // CatchUpEach runs on its own goroutine
		return e
	}
	t.Run("skip", func(t *testing.T) {
		e := missedSetup(t, CatchUpSkip, 0)
		if e.rec.count() != 0 || e.s.Stats()[0].MissedSlots != 3 {
			t.Errorf("fires %v, stats %+v; want nothing run and 3 missed (11:00 12:00 13:00)", slotsOf(e.rec.snapshot()), e.s.Stats()[0])
		}
	})
	t.Run("once", func(t *testing.T) {
		e := missedSetup(t, CatchUpOnce, 0)
		eq(t, "fires", slotsOf(e.rec.snapshot()), "13:00:00*")
		if f := e.rec.snapshot()[0]; !f.CatchUp || f.Missed != 3 {
			t.Errorf("fire %+v; want a catch-up marked with 3 missed", f)
		}
		if st := e.s.Stats()[0]; st.MissedSlots != 2 {
			t.Errorf("MissedSlots = %d, want the 2 collapsed into the one run", st.MissedSlots)
		}
	})
	t.Run("each, bounded, oldest first", func(t *testing.T) {
		e := missedSetup(t, CatchUpEach, 2)
		eq(t, "fires", slotsOf(e.rec.snapshot()), "11:00:00*", "12:00:00*")
		if st := e.s.Stats()[0]; st.MissedSlots != 1 {
			t.Errorf("MissedSlots = %d, want the 1 beyond the bound", st.MissedSlots)
		}
	})
	t.Run("each runs one after another, not together", func(t *testing.T) {
		e := setup(t)
		e.rec.hold = true
		e.rcd.SetLastRun(context.Background(), "role", "duty", t0.Add(10*time.Hour))
		e.clk.Advance(13*time.Hour + 30*time.Minute)
		e.add(t, Duty{Schedule: every(t, time.Hour), CatchUp: CatchUpEach, MaxCatchUp: 5})
		e.start(t)
		time.Sleep(50 * time.Millisecond)
		if e.rec.count() != 1 {
			t.Fatalf("%d catch-up runs started with the first still going, want 1", e.rec.count())
		}
		e.rec.release(1)
		time.Sleep(50 * time.Millisecond)
		if e.rec.count() != 2 {
			t.Errorf("%d after the first ended, want 2", e.rec.count())
		}
	})
	t.Run("a duty that has never run has nothing to catch up", func(t *testing.T) {
		e := setup(t)
		e.clk.Advance(48 * time.Hour)
		e.add(t, Duty{Schedule: every(t, time.Hour), CatchUp: CatchUpEach})
		e.start(t)
		time.Sleep(30 * time.Millisecond)
		if e.rec.count() != 0 {
			t.Errorf("a first-ever start ran %v", slotsOf(e.rec.snapshot()))
		}
	})
}

// The record is written when a run finishes, so a crash mid-run is caught up.
func TestTheLastRunIsRecordedWhenARunFinishesNotWhenItIsOffered(t *testing.T) {
	e := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	e.s.dispatch = func(role, duty string, run func(ctx context.Context) error) Handle {
		done := make(chan struct{})
		go func() { _ = run(context.Background()); close(done) }()
		return Handle{Disposition: Accepted, Done: done}
	}
	e.add(t, Duty{Schedule: every(t, time.Minute), Run: func(ctx context.Context, f Fire) error {
		close(started)
		<-release
		return nil
	}})
	e.start(t)
	e.clk.Advance(time.Minute)
	<-started
	if _, found, _ := e.rcd.LastRun(context.Background(), "role", "duty"); found {
		t.Fatal("recorded while the run was still going")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if last, found, _ := e.rcd.LastRun(context.Background(), "role", "duty"); found {
			if !last.Equal(t0.Add(time.Minute)) {
				t.Errorf("recorded %v", last)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("never recorded after the run finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRefusedAndShedRunsAreNotRecorded(t *testing.T) {
	e := setup(t)
	e.rec.disp = func(int) Disposition { return Dropped }
	e.add(t, Duty{Schedule: every(t, time.Minute)})
	e.start(t)
	e.clk.Advance(3 * time.Minute)
	if _, found, _ := e.rcd.LastRun(context.Background(), "role", "duty"); found {
		t.Error("a dropped run was recorded as done")
	}
	if st := e.s.Stats()[0]; st.Dropped != 3 || st.InFlight != 0 {
		t.Errorf("stats %+v", st)
	}
}

func TestADeferredRunIsOfferedAgainUntilTheNextSlot(t *testing.T) {
	e := setup(t)
	e.rec.disp = func(n int) Disposition {
		if n <= 2 {
			return Deferred
		}
		return Accepted
	}
	e.add(t, Duty{Schedule: every(t, time.Minute), DeferRetry: 10 * time.Second})
	e.start(t)
	e.clk.Advance(time.Minute) // slot 1: deferred
	if e.rec.count() != 0 {
		t.Fatal("ran while deferred")
	}
	e.clk.Advance(10 * time.Second) // retry 1: deferred again
	e.clk.Advance(10 * time.Second) // retry 2: accepted
	if e.rec.count() != 1 {
		t.Fatalf("runs = %d after the retries", e.rec.count())
	}
	if f := e.rec.snapshot()[0]; f.Attempt != 3 || !f.Slot.Equal(t0.Add(time.Minute)) {
		t.Errorf("the retried run: %+v, want attempt 3 for the original slot", f)
	}
	if st := e.s.Stats()[0]; st.Deferred != 2 {
		t.Errorf("Deferred = %d", st.Deferred)
	}
}

func TestADeferredRunThatWouldOutliveItsSlotIsLeftToTheNextOne(t *testing.T) {
	e := setup(t)
	e.rec.disp = func(n int) Disposition {
		if n == 1 {
			return Deferred
		}
		return Accepted
	}
	e.add(t, Duty{Schedule: every(t, 30*time.Second), DeferRetry: time.Minute}) // a retry would land after the next slot
	e.start(t)
	e.clk.Advance(30 * time.Second) // slot 1: deferred; no retry is worth scheduling
	e.clk.Advance(30 * time.Second) // slot 2 arrives
	eq(t, "runs", slotsOf(e.rec.snapshot()), "00:01:00")
}

func TestEventsFireSubscribedDutiesAndStartIsAnEvent(t *testing.T) {
	e := setup(t)
	e.add(t, Duty{Name: "on-start", Events: []string{EventStart}})
	e.add(t, Duty{Name: "on-lease", Events: []string{"lease.acquired"}})
	e.add(t, Duty{Name: "both", Schedule: every(t, time.Hour), Events: []string{"lease.acquired"}})
	e.s.Emit("lease.acquired") // before Start: nothing happens
	if e.rec.count() != 0 {
		t.Fatal("an event fired before Start")
	}
	e.start(t)
	names := func() []string {
		var out []string
		for _, f := range e.rec.snapshot() {
			out = append(out, f.Duty+":"+f.Event)
		}
		return out
	}
	eq(t, "after start", names(), "on-start:node.start")
	e.s.Emit("lease.acquired")
	e.s.Emit("unrelated")
	got := names()
	if len(got) != 3 {
		t.Errorf("fires %v, want the start duty plus the two lease subscribers", got)
	}
}

func TestOverlapAppliesToEventRunsToo(t *testing.T) {
	e := setup(t)
	e.rec.hold = true
	e.add(t, Duty{Events: []string{"x"}, Overlap: OverlapSkip})
	e.start(t)
	e.s.Emit("x")
	e.s.Emit("x")
	if e.rec.count() != 1 || e.s.Stats()[0].SkippedOverlap != 1 {
		t.Errorf("runs %d stats %+v", e.rec.count(), e.s.Stats()[0])
	}
}

// A process that was suspended collapses the slots it slept through instead
// of firing them in a burst.
func TestALateTimerFiresOnceAndCountsWhatItSleptThrough(t *testing.T) {
	e := setup(t)
	e.add(t, Duty{Schedule: every(t, time.Minute)})
	e.start(t)
	e.clk.Jump(3*time.Minute + 30*time.Second) // slots at 1, 2 and 3 minutes are now past
	e.clk.Advance(0)
	eq(t, "fires", slotsOf(e.rec.snapshot()), "00:01:00")
	st := e.s.Stats()[0]
	if st.MissedSlots != 2 || !st.NextSlot.Equal(t0.Add(4*time.Minute)) {
		t.Errorf("stats %+v, want 2 missed and the next at 4 minutes", st)
	}
	e.clk.Advance(30 * time.Second)
	eq(t, "then back on schedule", slotsOf(e.rec.snapshot()), "00:01:00", "00:04:00")
}

func TestOneShotFiresOnceThenNoMore(t *testing.T) {
	e := setup(t)
	e.add(t, Duty{Schedule: recurrence.Once(t0.Add(time.Hour))})
	e.start(t)
	e.clk.Advance(5 * time.Hour)
	eq(t, "fires", slotsOf(e.rec.snapshot()), "01:00:00")
	if e.clk.Pending() != 0 {
		t.Error("a timer is still armed for a finished one-shot")
	}
}

func TestAddRemoveStopAndValidation(t *testing.T) {
	e := setup(t)
	bad := []Duty{
		{Name: "a", Run: e.rec.onFire, Schedule: every(t, time.Hour)},                                 // no role
		{Role: "r", Run: e.rec.onFire, Schedule: every(t, time.Hour)},                                 // no name
		{Role: "r", Name: "a", Schedule: every(t, time.Hour)},                                         // no Run
		{Role: "r", Name: "a", Run: e.rec.onFire},                                                     // no trigger
		{Role: "r", Name: "a", Run: e.rec.onFire, Schedule: every(t, time.Hour), Jitter: -1},          // negative
		{Role: "r", Name: "a", Run: e.rec.onFire, Schedule: every(t, time.Hour), Overlap: Overlap(9)}, // unknown policy
		{Role: "r", Name: "a", Run: e.rec.onFire, Schedule: every(t, time.Hour), CatchUp: CatchUp(9)}, // unknown policy
	}
	for i, d := range bad {
		if err := e.s.Add(d); err == nil {
			t.Errorf("invalid duty #%d was accepted", i)
		}
	}
	e.add(t, Duty{Schedule: every(t, time.Minute)})
	if err := e.s.Add(Duty{Role: "role", Name: "duty", Run: e.rec.onFire, Schedule: every(t, time.Hour)}); !errors.Is(err, ErrDuplicateDuty) {
		t.Errorf("duplicate: %v", err)
	}
	e.start(t)
	if err := e.s.Start(); err == nil {
		t.Error("a second Start was accepted")
	}
	e.clk.Advance(time.Minute)
	if e.rec.count() != 1 {
		t.Fatal("setup")
	}

	// A duty added after Start begins at once.
	e.add(t, Duty{Name: "late", Schedule: every(t, time.Minute)})
	e.clk.Advance(time.Minute)
	if e.rec.count() != 3 {
		t.Errorf("fires %v", slotsOf(e.rec.snapshot()))
	}

	e.s.Remove("role", "duty")
	e.clk.Advance(time.Minute)
	if e.rec.count() != 4 { // only "late" fires
		t.Errorf("a removed duty kept firing: %d fires", e.rec.count())
	}

	e.s.Stop()
	e.s.Stop() // idempotent
	e.clk.Advance(time.Hour)
	if e.rec.count() != 4 {
		t.Error("fired after Stop")
	}
	if e.clk.Pending() != 0 {
		t.Errorf("%d timers left after Stop", e.clk.Pending())
	}
	if err := e.s.Add(Duty{Role: "r", Name: "z", Run: e.rec.onFire, Schedule: every(t, time.Hour)}); !errors.Is(err, ErrStopped) {
		t.Errorf("Add after Stop: %v", err)
	}
	if _, err := New(Config{}); err == nil {
		t.Error("a scheduler with no dispatcher was accepted")
	}
}

func TestFailuresAreCountedAndDoNotStopTheSchedule(t *testing.T) {
	e := setup(t)
	e.rec.failWith = errors.New("boom")
	e.add(t, Duty{Schedule: every(t, time.Minute)})
	e.start(t)
	e.clk.Advance(3 * time.Minute)
	if st := e.s.Stats()[0]; st.Failed != 3 || st.Completed != 0 {
		t.Errorf("stats %+v", st)
	}
}
