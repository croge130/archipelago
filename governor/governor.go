package governor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Overflow is what happens to a piece of work that arrives when its role has
// no room to run it and no room to queue it. It is part of a duty's
// definition, not an accident of an unbounded channel.
type Overflow int

const (
	// OverflowDrop refuses the new work; the caller should forget it.
	OverflowDrop Overflow = iota
	// OverflowDefer refuses the new work and says so: not now, ask again
	// later. A scheduler turns this into a retry at the next opportunity.
	OverflowDefer
	// OverflowCoalesce collapses the new work into an identical waiting
	// piece (same Key): repeated "run the sweep" triggers become one. With
	// nothing to merge into and no room, it drops.
	OverflowCoalesce
)

// Work is one unit of a duty: one execution.
type Work struct {
	// Duty names the unit, for stats and logs.
	Duty string
	// Priority overrides the role's own for this work (a task kind's
	// priority). Empty takes the role's. The caller is responsible for
	// bounding a remote party's hint with Min before it gets here.
	Priority Priority
	Overflow Overflow
	// Key identifies identical work for OverflowCoalesce (default Duty).
	Key string
	// Run does the work. Its context ends when the run times out or the
	// governor closes. It must return promptly once that happens.
	Run func(ctx context.Context) error
}

// State is where a piece of work ended up.
type State int

const (
	Completed State = iota + 1
	Failed
	Shed
	Cancelled
)

func (s State) String() string {
	switch s {
	case Completed:
		return "completed"
	case Failed:
		return "failed"
	case Shed:
		return "shed"
	case Cancelled:
		return "cancelled"
	}
	return "unknown"
}

// ShedReason says why work did not run.
type ShedReason string

const (
	ShedQueueFull ShedReason = "queue full"
	ShedDeferred  ShedReason = "deferred"
	ShedClosed    ShedReason = "governor closed"
	ShedRemoved   ShedReason = "node role removed"
)

// Result is the outcome of one piece of work.
type Result struct {
	State  State
	Err    error      // Failed or Cancelled: what Run returned
	Reason ShedReason // Shed only
	Waited time.Duration
	Ran    time.Duration
}

// Ticket follows one piece of work. A shed ticket is already resolved.
type Ticket struct {
	done   chan struct{}
	result Result
}

// Done is closed when the work has finished or been shed.
func (t *Ticket) Done() <-chan struct{} { return t.done }

// Result is valid once Done is closed.
func (t *Ticket) Result() Result { return t.result }

// Wait blocks until the work ends or ctx does.
func (t *Ticket) Wait(ctx context.Context) (Result, error) {
	select {
	case <-t.done:
		return t.result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (t *Ticket) resolve(r Result) {
	t.result = r
	close(t.done)
}

// Config configures a Governor. Node is required: the consent boundary.
type Config struct {
	Node Ceiling
	// Domains are optional per-domain shares of the node ceiling, keyed by
	// the name a role registers under. A domain with no entry is bound by the
	// node ceiling alone.
	Domains map[string]Ceiling
	// AgeStep is how long waiting work waits before it counts as one level
	// more urgent (default DefaultAgeStep).
	AgeStep time.Duration
	Logger  *slog.Logger
}

var (
	// ErrClosed: the governor has been closed.
	ErrClosed = errors.New("governor: closed")
	// ErrDuplicateRole: that (domain, key) is already registered.
	ErrDuplicateRole = errors.New("governor: node role already registered in that domain")
)

// Governor admits, queues or sheds work against the node's budget. One per
// node, shared by every domain on it.
type Governor struct {
	log     *slog.Logger
	ageStep time.Duration

	baseCtx    context.Context
	baseCancel context.CancelFunc
	wg         sync.WaitGroup

	mu       sync.Mutex
	node     level
	domains  map[string]*level
	ceilings map[string]Ceiling
	nodeCeil Ceiling
	roles    map[roleKey]*Role
	waiting  []*item
	seq      uint64
	pressure Pressure
	closed   bool
	timer    *time.Timer
}

type roleKey struct{ domain, key string }

// level is one nested capacity pool.
type level struct {
	cap     int
	running int
}

// New creates a Governor.
func New(cfg Config) (*Governor, error) {
	if err := cfg.Node.Validate(); err != nil {
		return nil, fmt.Errorf("node ceiling: %w", err)
	}
	g := &Governor{
		log: cfg.Logger, ageStep: cfg.AgeStep,
		node: level{cap: cfg.Node.Concurrency}, nodeCeil: cfg.Node,
		domains: map[string]*level{}, ceilings: map[string]Ceiling{}, roles: map[roleKey]*Role{},
	}
	if g.log == nil {
		g.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if g.ageStep <= 0 {
		g.ageStep = DefaultAgeStep
	}
	for name, c := range cfg.Domains {
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("domain %q ceiling: %w", name, err)
		}
		g.ceilings[name] = c
		g.domains[name] = &level{cap: min(c.Concurrency, cfg.Node.Concurrency)}
	}
	g.baseCtx, g.baseCancel = context.WithCancel(context.Background())
	return g, nil
}

// RoleSpec registers a node role's budget with the governor.
type RoleSpec struct {
	// Key names the node role; Domain the App it is adopted in.
	Key    string
	Domain string
	// Requested is the budget being asked for, normally from ResolveBudget
	// over the node's adoption, after the domain's policy has had its say.
	// The governor applies the domain share and the node ceiling to it.
	Requested Limits
	// Priority is the node role's own standing (default Normal).
	Priority Priority
}

// Role is a registered node role's handle for submitting work.
type Role struct {
	g        *Governor
	spec     RoleSpec
	limits   Limits
	clamps   []Clamp
	domain   *level
	prio     Priority
	starts   []time.Time // recent start times, for the rate limit
	running  int
	removed  bool
	counters Counters
}

// Register adds a node role. The returned role's Limits are what it will
// really run under, and Clamps lists every dimension a ceiling reduced.
// Clamping is logged at warn: a node never silently runs with a different
// number than the one asked for.
func (g *Governor) Register(spec RoleSpec) (*Role, error) {
	if spec.Key == "" {
		return nil, errors.New("governor: a node role needs a Key")
	}
	if err := spec.Requested.Validate(); err != nil {
		return nil, fmt.Errorf("node role %q: %w", spec.Key, err)
	}
	prio := spec.Priority
	if prio == "" {
		prio = Normal
	}
	if !prio.Valid() {
		return nil, fmt.Errorf("node role %q: unknown priority %q", spec.Key, prio)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrClosed
	}
	k := roleKey{spec.Domain, spec.Key}
	if _, dup := g.roles[k]; dup {
		return nil, ErrDuplicateRole
	}
	limits, clamps := spec.Requested, []Clamp(nil)
	var dom *level
	if c, ok := g.ceilings[spec.Domain]; ok {
		var cl []Clamp
		limits, cl = ClampTo(limits, c, "domain")
		clamps = append(clamps, cl...)
		dom = g.domains[spec.Domain]
	}
	var cl []Clamp
	limits, cl = ClampTo(limits, g.nodeCeil, "node")
	clamps = append(clamps, cl...)
	for _, c := range clamps {
		g.log.Warn("a node role's budget was capped", "node_role", spec.Key, "domain", spec.Domain, "clamp", c.String())
	}
	r := &Role{g: g, spec: spec, limits: limits, clamps: clamps, domain: dom, prio: prio}
	g.roles[k] = r
	return r, nil
}

// Limits are what the node role really runs under.
func (r *Role) Limits() Limits { return r.limits }

// Clamps lists the dimensions a ceiling reduced, empty if none.
func (r *Role) Clamps() []Clamp { return append([]Clamp(nil), r.clamps...) }

// Unregister removes the node role: its waiting work is shed, and its
// running work finishes.
func (r *Role) Unregister() {
	g := r.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.removed {
		return
	}
	r.removed = true
	delete(g.roles, roleKey{r.spec.Domain, r.spec.Key})
	kept := g.waiting[:0]
	for _, it := range g.waiting {
		if it.role == r {
			g.shedLocked(it, ShedRemoved)
			continue
		}
		kept = append(kept, it)
	}
	g.waiting = kept
}

type item struct {
	seq    uint64
	role   *Role
	work   Work
	prio   Priority
	queued time.Time
	ticket *Ticket
}

// Submit offers one piece of work. Misuse (no Run, a closed governor, a
// removed node role) is an error; running out of room is not, it is a
// resolved Ticket saying the work was shed and why.
func (r *Role) Submit(w Work) (*Ticket, error) {
	if w.Run == nil {
		return nil, errors.New("governor: Work needs a Run function")
	}
	prio := w.Priority
	if prio == "" {
		prio = r.prio
	}
	if !prio.Valid() {
		return nil, fmt.Errorf("governor: unknown priority %q", prio)
	}
	if w.Key == "" {
		w.Key = w.Duty
	}
	g := r.g
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrClosed
	}
	if r.removed {
		return nil, errors.New("governor: the node role was removed")
	}
	r.counters.Submitted++

	if w.Overflow == OverflowCoalesce {
		for _, it := range g.waiting {
			if it.role == r && it.work.Key == w.Key {
				if prio.Rank() > it.prio.Rank() {
					it.prio = prio // the merged work is as urgent as its most urgent trigger
				}
				r.counters.Coalesced++
				return it.ticket, nil
			}
		}
	}

	g.seq++
	it := &item{seq: g.seq, role: r, work: w, prio: prio, queued: now, ticket: &Ticket{done: make(chan struct{})}}
	g.waiting = append(g.waiting, it)
	g.dispatchLocked(now)

	// Still waiting? Then it occupies queue space, and may be over the role's depth.
	if g.isWaiting(it) && g.waitingCount(r) > r.limits.QueueDepth {
		g.removeWaiting(it)
		switch w.Overflow {
		case OverflowDefer:
			g.shedLocked(it, ShedDeferred)
		default:
			g.shedLocked(it, ShedQueueFull)
		}
	}
	return it.ticket, nil
}

func (g *Governor) isWaiting(it *item) bool {
	for _, x := range g.waiting {
		if x == it {
			return true
		}
	}
	return false
}

func (g *Governor) removeWaiting(it *item) {
	for i, x := range g.waiting {
		if x == it {
			g.waiting = append(g.waiting[:i], g.waiting[i+1:]...)
			return
		}
	}
}

func (g *Governor) waitingCount(r *Role) int {
	n := 0
	for _, it := range g.waiting {
		if it.role == r {
			n++
		}
	}
	return n
}

func (g *Governor) shedLocked(it *item, reason ShedReason) {
	switch reason {
	case ShedQueueFull:
		it.role.counters.ShedQueueFull++
	case ShedDeferred:
		it.role.counters.ShedDeferred++
	default:
		it.role.counters.ShedOther++
	}
	g.log.Debug("work shed", "node_role", it.role.spec.Key, "duty", it.work.Duty, "reason", string(reason))
	it.ticket.resolve(Result{State: Shed, Reason: reason})
}

// SetPressure is the yield hook: the application reporting how strained it
// is. Raising it stops new low-priority work from starting; lowering it lets
// held work go. Work already running is never touched.
func (g *Governor) SetPressure(p Pressure) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pressure == p {
		return
	}
	g.log.Info("pressure changed", "from", g.pressure.String(), "to", p.String())
	g.pressure = p
	g.dispatchLocked(time.Now())
}

// Pressure is the current level.
func (g *Governor) Pressure() Pressure {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pressure
}

// dispatchLocked starts as much waiting work as there is room for, best
// first: highest effective priority, then oldest. Work that is held back
// (by pressure, by a full level, or by its role's rate) does not block work
// behind it that can run. If anything is waiting only on a rate limit, a
// timer is set for the earliest moment it could start.
func (g *Governor) dispatchLocked(now time.Time) {
	for {
		var best *item
		var bestRank int
		var soonest time.Time
		for _, it := range g.waiting {
			if !g.pressure.admits(it.prio) {
				continue
			}
			r := it.role
			if g.node.running >= g.node.cap || (r.domain != nil && r.domain.running >= r.domain.cap) || r.running >= r.limits.Concurrency {
				continue
			}
			if at, ok := r.nextStart(now); !ok {
				if soonest.IsZero() || at.Before(soonest) {
					soonest = at
				}
				continue
			}
			rank := effectivePriority(it.prio, now.Sub(it.queued), g.ageStep)
			if best == nil || rank > bestRank || (rank == bestRank && it.seq < best.seq) {
				best, bestRank = it, rank
			}
		}
		if best == nil {
			g.armTimerLocked(soonest, now)
			return
		}
		g.removeWaiting(best)
		g.startLocked(best, now)
	}
}

func (g *Governor) armTimerLocked(at, now time.Time) {
	if at.IsZero() {
		return
	}
	if g.timer != nil {
		g.timer.Stop()
	}
	d := at.Sub(now)
	if d < time.Millisecond {
		d = time.Millisecond
	}
	g.timer = time.AfterFunc(d, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !g.closed {
			g.dispatchLocked(time.Now())
		}
	})
}

// nextStart reports whether the role's rate allows a start now; if not, when
// it will.
func (r *Role) nextStart(now time.Time) (time.Time, bool) {
	rate := r.limits.Rate
	if !rate.set() {
		return time.Time{}, true
	}
	cut := now.Add(-rate.Per)
	i := 0
	for i < len(r.starts) && !r.starts[i].After(cut) {
		i++
	}
	r.starts = r.starts[i:]
	if len(r.starts) < rate.Starts {
		return time.Time{}, true
	}
	return r.starts[0].Add(rate.Per), false
}

func (g *Governor) startLocked(it *item, now time.Time) {
	r := it.role
	g.node.running++
	if r.domain != nil {
		r.domain.running++
	}
	r.running++
	if r.limits.Rate.set() {
		r.starts = append(r.starts, now)
	}
	r.counters.Started++
	waited := now.Sub(it.queued)

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		ctx, cancel := g.baseCtx, context.CancelFunc(func() {})
		if r.limits.RunTimeout > 0 {
			ctx, cancel = context.WithTimeout(g.baseCtx, r.limits.RunTimeout)
		}
		began := time.Now()
		err := safely(ctx, it.work.Run)
		cancel()
		ran := time.Since(began)

		g.mu.Lock()
		defer g.mu.Unlock()
		g.node.running--
		if r.domain != nil {
			r.domain.running--
		}
		r.running--
		res := Result{Waited: waited, Ran: ran}
		switch {
		case err == nil:
			res.State = Completed
			r.counters.Completed++
		case errors.Is(err, context.Canceled) && g.baseCtx.Err() != nil:
			res.State, res.Err = Cancelled, err
			r.counters.Cancelled++
		default:
			res.State, res.Err = Failed, err
			r.counters.Failed++
		}
		it.ticket.resolve(res)
		if !g.closed {
			g.dispatchLocked(time.Now())
		}
	}()
}

// safely turns a panic in a duty into a failure: one bad duty must not take
// the node down.
func safely(ctx context.Context, run func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("governor: duty panicked: %v", r)
		}
	}()
	return run(ctx)
}

// Close stops accepting work, sheds everything waiting, cancels the context
// of everything running, and returns once it has finished.
func (g *Governor) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	if g.timer != nil {
		g.timer.Stop()
	}
	for _, it := range g.waiting {
		g.shedLocked(it, ShedClosed)
	}
	g.waiting = nil
	g.mu.Unlock()
	g.baseCancel()
	g.wg.Wait()
}
