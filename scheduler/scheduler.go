package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/croge130/archipelago/recurrence"
)

// Overlap is what to do when a duty is due while its previous run is still
// going.
type Overlap int

const (
	// OverlapSkip drops the new occurrence.
	OverlapSkip Overlap = iota
	// OverlapQueueOne remembers one occurrence (the latest) and runs it when
	// the previous run ends.
	OverlapQueueOne
	// OverlapAllow offers it regardless; the dispatcher's own concurrency
	// bound is then the limit.
	OverlapAllow
)

// CatchUp is what to do, when the scheduler starts, about occurrences that
// fell due since the duty last finished a run.
type CatchUp int

const (
	// CatchUpSkip ignores them.
	CatchUpSkip CatchUp = iota
	// CatchUpOnce runs the duty once, however many were missed.
	CatchUpOnce
	// CatchUpEach runs the oldest missed occurrences one after another, up to
	// Duty.MaxCatchUp, and counts the rest as missed.
	CatchUpEach
)

// Event names the scheduler emits itself. An integration may emit others with
// Scheduler.Emit (a lease acquired, a Policy value changed).
const EventStart = "node.start"

// Fire is one occurrence of a duty, given to its Run.
type Fire struct {
	Role, Duty string
	// Slot is the scheduled time this occurrence stands for (not when it
	// actually ran); zero for an event-triggered run.
	Slot time.Time
	// Event is the event that triggered it, empty for a scheduled run.
	Event string
	// CatchUp marks a run made up for a missed occurrence; Missed is how
	// many were missed in all.
	CatchUp bool
	Missed  int
	// Attempt counts re-offers after the dispatcher deferred it (1 = first).
	Attempt int
}

// Duty is one unit of automatic work and when it is due.
type Duty struct {
	// Role and Name identify the duty (the node role it belongs to, and its
	// name within it); together they key the catch-up record.
	Role, Name string

	// Schedule is when the duty is due, from the recurrence package; nil for
	// a duty driven only by events.
	Schedule recurrence.Schedule
	// Events also trigger the duty.
	Events []string
	// Jitter delays each scheduled run by a random amount up to this, so a
	// fleet that started together does not fire together. The slot itself,
	// and so the next occurrence, is unaffected: jitter never accumulates.
	Jitter time.Duration

	Overlap Overlap
	CatchUp CatchUp
	// MaxCatchUp bounds CatchUpEach (default 10).
	MaxCatchUp int
	// DeferRetry is how long to wait before re-offering a run the dispatcher
	// deferred (default one second). Re-offers stop when the next scheduled
	// occurrence arrives.
	DeferRetry time.Duration

	// Run does the work for one occurrence. It runs wherever the Dispatcher
	// runs it, so it must honour its context.
	Run func(ctx context.Context, f Fire) error
}

// Disposition is what the Dispatcher did with a run it was offered.
type Disposition int

const (
	// Accepted: queued or running; Done closes when it has ended.
	Accepted Disposition = iota
	// Dropped: refused; forget it.
	Dropped
	// Deferred: refused for now; offer it again later.
	Deferred
)

// Handle is the Dispatcher's answer. For anything but Accepted, Done may be
// nil.
type Handle struct {
	Disposition Disposition
	// Done is closed when an accepted run has ended, or been shed before it
	// started.
	Done <-chan struct{}
}

// Dispatcher offers one execution to whatever decides whether it may run: the
// resource governor, through an adapter. It must not block waiting for the run
// to finish.
type Dispatcher func(role, duty string, run func(ctx context.Context) error) Handle

// Config configures a Scheduler.
type Config struct {
	Dispatch Dispatcher
	// Record is the catch-up record (default in memory: no catch-up across a
	// restart).
	Record Record
	// Clock defaults to the real one.
	Clock Clock
	// Rand returns a number in [0,1) for jitter (default math/rand).
	Rand   func() float64
	Logger *slog.Logger
}

// DutyStats are cumulative counts for one duty.
type DutyStats struct {
	Role, Name string
	// Fired counts occurrences offered to the dispatcher (including re-offers
	// and catch-up runs); the rest count what happened to them.
	Fired          uint64
	Completed      uint64
	Failed         uint64
	SkippedOverlap uint64
	Queued         uint64
	Dropped        uint64
	Deferred       uint64
	// MissedSlots are occurrences never offered: missed while the node was
	// down beyond what catch-up covers, or while the process was too late.
	MissedSlots uint64
	LastSlot    time.Time
	NextSlot    time.Time
	InFlight    int
}

// Scheduler decides when duties are due.
type Scheduler struct {
	dispatch Dispatcher
	record   Record
	clock    Clock
	rand     func() float64
	log      *slog.Logger

	mu       sync.Mutex
	duties   map[[2]string]*state
	started  bool
	stopped  bool
	ctx      context.Context
	cancel   context.CancelFunc
	catchups sync.WaitGroup
}

type state struct {
	s    *Scheduler
	duty Duty

	mu       sync.Mutex
	timer    Timer
	retry    Timer
	inFlight int
	pending  *Fire
	removed  bool
	stats    DutyStats
}

var (
	// ErrDuplicateDuty: that (role, name) is already scheduled.
	ErrDuplicateDuty = errors.New("scheduler: duty already scheduled")
	// ErrStopped: the scheduler has been stopped.
	ErrStopped = errors.New("scheduler: stopped")
)

// New creates a Scheduler.
func New(cfg Config) (*Scheduler, error) {
	if cfg.Dispatch == nil {
		return nil, errors.New("scheduler: a Dispatcher is required")
	}
	s := &Scheduler{dispatch: cfg.Dispatch, record: cfg.Record, clock: cfg.Clock, rand: cfg.Rand, log: cfg.Logger, duties: map[[2]string]*state{}}
	if s.record == nil {
		s.record = NewMemoryRecord()
	}
	if s.clock == nil {
		s.clock = realClock{}
	}
	if s.rand == nil {
		s.rand = rand.Float64
	}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s, nil
}

func (d Duty) validate() error {
	switch {
	case d.Role == "" || d.Name == "":
		return errors.New("scheduler: a duty needs a Role and a Name")
	case d.Run == nil:
		return fmt.Errorf("scheduler: duty %s/%s has no Run", d.Role, d.Name)
	case d.Schedule == nil && len(d.Events) == 0:
		return fmt.Errorf("scheduler: duty %s/%s has neither a schedule nor events", d.Role, d.Name)
	case d.Jitter < 0 || d.DeferRetry < 0 || d.MaxCatchUp < 0:
		return fmt.Errorf("scheduler: duty %s/%s has a negative duration or bound", d.Role, d.Name)
	case d.Overlap < OverlapSkip || d.Overlap > OverlapAllow:
		return fmt.Errorf("scheduler: duty %s/%s has an unknown overlap policy", d.Role, d.Name)
	case d.CatchUp < CatchUpSkip || d.CatchUp > CatchUpEach:
		return fmt.Errorf("scheduler: duty %s/%s has an unknown catch-up policy", d.Role, d.Name)
	}
	return nil
}

// Add schedules a duty. Before Start it waits; after, it begins at once
// (catching up first, per its policy).
func (s *Scheduler) Add(d Duty) error {
	if err := d.validate(); err != nil {
		return err
	}
	if d.MaxCatchUp == 0 {
		d.MaxCatchUp = 10
	}
	if d.DeferRetry == 0 {
		d.DeferRetry = time.Second
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	k := [2]string{d.Role, d.Name}
	if _, dup := s.duties[k]; dup {
		s.mu.Unlock()
		return ErrDuplicateDuty
	}
	st := &state{s: s, duty: d, stats: DutyStats{Role: d.Role, Name: d.Name}}
	s.duties[k] = st
	started := s.started
	s.mu.Unlock()
	if started {
		st.begin()
	}
	return nil
}

// Remove stops scheduling a duty. A run already offered is not recalled.
func (s *Scheduler) Remove(role, name string) {
	s.mu.Lock()
	st := s.duties[[2]string{role, name}]
	delete(s.duties, [2]string{role, name})
	s.mu.Unlock()
	if st != nil {
		st.stop()
	}
}

// Start begins scheduling every duty added so far, catching up first, and
// then emits EventStart.
func (s *Scheduler) Start() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	if s.started {
		s.mu.Unlock()
		return errors.New("scheduler: already started")
	}
	s.started = true
	all := make([]*state, 0, len(s.duties))
	for _, st := range s.duties {
		all = append(all, st)
	}
	s.mu.Unlock()
	for _, st := range all {
		st.begin()
	}
	s.Emit(EventStart)
	return nil
}

// Stop cancels every pending occurrence and any catch-up in progress. Runs
// already offered are the dispatcher's to finish or cancel.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	all := make([]*state, 0, len(s.duties))
	for _, st := range s.duties {
		all = append(all, st)
	}
	s.mu.Unlock()
	s.cancel()
	for _, st := range all {
		st.stop()
	}
	s.catchups.Wait()
}

// Emit fires every duty subscribed to the event.
func (s *Scheduler) Emit(event string) {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	var hit []*state
	for _, st := range s.duties {
		for _, e := range st.duty.Events {
			if e == event {
				hit = append(hit, st)
				break
			}
		}
	}
	s.mu.Unlock()
	for _, st := range hit {
		st.fire(Fire{Role: st.duty.Role, Duty: st.duty.Name, Event: event, Attempt: 1})
	}
}

// Stats takes a snapshot of every duty.
func (s *Scheduler) Stats() []DutyStats {
	s.mu.Lock()
	all := make([]*state, 0, len(s.duties))
	for _, st := range s.duties {
		all = append(all, st)
	}
	s.mu.Unlock()
	out := make([]DutyStats, 0, len(all))
	for _, st := range all {
		st.mu.Lock()
		cp := st.stats
		cp.InFlight = st.inFlight
		st.mu.Unlock()
		out = append(out, cp)
	}
	return out
}

func (st *state) fireOf(slot time.Time, catchUp bool, missed int) Fire {
	return Fire{Role: st.duty.Role, Duty: st.duty.Name, Slot: slot, CatchUp: catchUp, Missed: missed, Attempt: 1}
}

// begin catches up on what was missed, then arms the next occurrence.
func (st *state) begin() {
	d := st.duty
	if d.Schedule == nil {
		return
	}
	s := st.s
	now := s.clock.Now()
	last, found, err := s.record.LastRun(s.ctx, d.Role, d.Name)
	if err != nil {
		s.log.Warn("could not read the catch-up record; skipping catch-up", "node_role", d.Role, "duty", d.Name, "error", err.Error())
	}
	var missed []time.Time
	if found && err == nil {
		missed = recurrence.Between(d.Schedule, last, now, 1000)
	}
	switch {
	case len(missed) == 0:
	case d.CatchUp == CatchUpSkip:
		st.countMissed(len(missed))
	case d.CatchUp == CatchUpOnce:
		st.fire(st.fireOf(missed[len(missed)-1], true, len(missed)))
		st.countMissed(len(missed) - 1)
	case d.CatchUp == CatchUpEach:
		run := missed
		if len(run) > d.MaxCatchUp {
			st.countMissed(len(run) - d.MaxCatchUp)
			run = run[:d.MaxCatchUp]
		}
		s.catchups.Add(1)
		go st.catchUpEach(run, len(missed))
	}
	st.mu.Lock()
	st.armLocked(now)
	st.mu.Unlock()
}

// catchUpEach offers the missed occurrences one at a time, each only after
// the previous has ended, whatever the duty's overlap policy.
func (st *state) catchUpEach(slots []time.Time, total int) {
	defer st.s.catchups.Done()
	for _, slot := range slots {
		h, ok := st.offer(st.fireOf(slot, true, total))
		if !ok || h.Done == nil {
			continue
		}
		select {
		case <-h.Done:
		case <-st.s.ctx.Done():
			return
		}
	}
}

func (st *state) countMissed(n int) {
	if n <= 0 {
		return
	}
	st.mu.Lock()
	st.stats.MissedSlots += uint64(n)
	st.mu.Unlock()
}

// armLocked sets the timer for the first occurrence after `from`. Occurrences
// whose (jittered) time has already passed are counted as missed rather than
// fired in a burst.
func (st *state) armLocked(from time.Time) {
	if st.removed || st.duty.Schedule == nil {
		return
	}
	s := st.s
	now := s.clock.Now()
	next, ok := st.duty.Schedule.Next(from)
	for i := 0; ok && next.Add(st.duty.Jitter).Before(now) && i < 10000; i++ {
		st.stats.MissedSlots++
		next, ok = st.duty.Schedule.Next(next)
	}
	if !ok {
		st.stats.NextSlot = time.Time{}
		return
	}
	st.stats.NextSlot = next
	delay := next.Sub(now)
	if st.duty.Jitter > 0 {
		delay += time.Duration(s.rand() * float64(st.duty.Jitter))
	}
	if delay < 0 {
		delay = 0
	}
	slot := next
	st.timer = s.clock.AfterFunc(delay, func() { st.onTimer(slot) })
}

func (st *state) onTimer(slot time.Time) {
	st.mu.Lock()
	if st.removed {
		st.mu.Unlock()
		return
	}
	if st.retry != nil { // a new occurrence supersedes a pending re-offer
		st.retry.Stop()
		st.retry = nil
	}
	st.armLocked(slot)
	st.mu.Unlock()
	st.fire(st.fireOf(slot, false, 0))
}

func (st *state) stop() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.removed = true
	if st.timer != nil {
		st.timer.Stop()
	}
	if st.retry != nil {
		st.retry.Stop()
	}
}

// fire applies the overlap policy, then offers the run.
func (st *state) fire(f Fire) {
	st.mu.Lock()
	if st.removed {
		st.mu.Unlock()
		return
	}
	if st.inFlight > 0 {
		switch st.duty.Overlap {
		case OverlapSkip:
			st.stats.SkippedOverlap++
			st.mu.Unlock()
			return
		case OverlapQueueOne:
			cp := f
			st.pending = &cp
			st.stats.Queued++
			st.mu.Unlock()
			return
		}
	}
	st.mu.Unlock()
	st.offer(f)
}

// offer gives one run to the dispatcher and acts on its answer.
func (st *state) offer(f Fire) (Handle, bool) {
	d := st.duty
	st.mu.Lock()
	if st.removed {
		st.mu.Unlock()
		return Handle{}, false
	}
	st.inFlight++
	st.stats.Fired++
	if !f.Slot.IsZero() && f.Slot.After(st.stats.LastSlot) {
		st.stats.LastSlot = f.Slot
	}
	st.mu.Unlock()

	run := func(ctx context.Context) error {
		err := d.Run(ctx, f)
		st.mu.Lock()
		if err != nil {
			st.stats.Failed++
		} else {
			st.stats.Completed++
		}
		st.mu.Unlock()
		// Recorded when the run finishes, not when it starts: a crash
		// mid-run leaves the slot unrecorded, so it is caught up, which is
		// the at-least-once the design asks of duties.
		if !f.Slot.IsZero() {
			if rerr := st.s.record.SetLastRun(context.WithoutCancel(ctx), d.Role, d.Name, f.Slot); rerr != nil {
				st.s.log.Warn("could not record a finished run", "node_role", d.Role, "duty", d.Name, "error", rerr.Error())
			}
		}
		return err
	}
	h := st.s.dispatch(d.Role, d.Name, run)

	switch h.Disposition {
	case Accepted:
		if h.Done == nil {
			st.finished()
			return h, true
		}
		// A run that has already ended must not count as in flight, or an
		// occurrence due immediately after would be mistaken for an overlap.
		select {
		case <-h.Done:
			st.finished()
		default:
			go func() {
				<-h.Done
				st.finished()
			}()
		}
	case Deferred:
		st.mu.Lock()
		st.inFlight--
		st.stats.Deferred++
		st.scheduleRetryLocked(f)
		st.mu.Unlock()
	default:
		st.mu.Lock()
		st.inFlight--
		st.stats.Dropped++
		st.mu.Unlock()
		st.drain()
	}
	return h, true
}

// scheduleRetryLocked re-offers a deferred run, unless a newer occurrence is
// already due before the retry would happen.
func (st *state) scheduleRetryLocked(f Fire) {
	if st.removed {
		return
	}
	now := st.s.clock.Now()
	if !st.stats.NextSlot.IsZero() && !now.Add(st.duty.DeferRetry).Before(st.stats.NextSlot) {
		return // the next occurrence will carry on from here
	}
	if st.retry != nil {
		st.retry.Stop()
	}
	f.Attempt++
	st.retry = st.s.clock.AfterFunc(st.duty.DeferRetry, func() {
		st.mu.Lock()
		st.retry = nil
		st.mu.Unlock()
		st.fire(f)
	})
}

func (st *state) finished() {
	st.mu.Lock()
	st.inFlight--
	st.mu.Unlock()
	st.drain()
}

// drain runs the one queued occurrence, if there is one and nothing is running.
func (st *state) drain() {
	st.mu.Lock()
	if st.inFlight > 0 || st.pending == nil || st.removed {
		st.mu.Unlock()
		return
	}
	f := *st.pending
	st.pending = nil
	st.mu.Unlock()
	st.offer(f)
}
