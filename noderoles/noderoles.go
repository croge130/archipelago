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
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
)

// Duty is one unit of automatic work within a node role.
type Duty struct {
	Name string

	// Schedule is the duty's default schedule; an adoption may replace it
	// (Policy resolved upstream). Nil for a duty driven only by Events.
	Schedule recurrence.Schedule
	Events   []string
	Jitter   time.Duration

	Overlap    scheduler.Overlap
	CatchUp    scheduler.CatchUp
	MaxCatchUp int
	DeferRetry time.Duration

	// Overflow is what happens to a run the governor has no room for.
	Overflow governor.Overflow
	// Priority overrides the node role's for this duty.
	Priority governor.Priority

	Run func(ctx context.Context, f scheduler.Fire) error
}

// Definition describes a node role. Core node roles live under a reserved
// namespace and app-defined ones elsewhere, the same split as everything else
// in the design (18); the runtime does not enforce the naming.
type Definition struct {
	Key         string
	Description string

	// Exclusive node roles may have one holder at a time; most should not
	// (19: prefer designs several nodes can hold). Adopting one requires a
	// Gate, and its duties must be idempotent, because a lease is a liveness
	// primitive and not a fencing token.
	Exclusive bool

	// Tiers define what low, medium and high mean for this node role's work.
	Tiers governor.TierTable
	// Default is the budget "use the default" adopts.
	Default *governor.Limits
	// Priority is the node role's standing (default Normal).
	Priority governor.Priority

	// Permissions are what the node's principal needs for the duties to
	// run, registered like any other (09). They are checked at adoption and
	// before every run.
	Permissions []string

	Duties []Duty

	// Setup runs once after the adoption is authorized and before any duty is
	// scheduled; Teardown runs when the node role is closed.
	Setup    func(ctx context.Context) error
	Teardown func(ctx context.Context) error
}

// Gate tells an exclusive node role whether this node holds it. An
// integration backs it with a registry lease.
type Gate interface {
	// Acquire reports whether the node role is held now. If so, lost is
	// closed when that stops being true, which cancels the runs in progress.
	Acquire() (lost <-chan struct{}, held bool)
}

// Adoption is the node's own decision about a node role.
type Adoption struct {
	// Budget states how much the node consents to. It must name a tier, say
	// to use the default, or give explicit limits.
	Budget governor.Adoption
	// Priority overrides the Definition's.
	Priority governor.Priority
	// Schedules replace duties' default schedules, by duty name.
	Schedules map[string]recurrence.Schedule
	// Gate is required for an exclusive node role.
	Gate Gate
	// Authorize checks the node's principal holds the permissions, and is
	// called at adoption and before each run. Nil means no check.
	Authorize func(ctx context.Context, permissions []string) error
}

var (
	// ErrNotAuthorized: the node's principal lacks the permissions the node
	// role needs. An adopted node role fails closed and says so; it does not
	// quietly skip.
	ErrNotAuthorized = errors.New("noderoles: the node is not authorized to run this node role")
	// ErrNeedsGate: an exclusive node role was adopted with no Gate.
	ErrNeedsGate = errors.New("noderoles: an exclusive node role needs a Gate")
	// ErrAlreadyAdopted: this runtime already runs that node role.
	ErrAlreadyAdopted = errors.New("noderoles: node role already adopted")
	// ErrClosed: the runtime was stopped.
	ErrClosed = errors.New("noderoles: runtime stopped")
)

// Config configures a Runtime. One Runtime per domain (per App); the
// Governor is the node's, shared by every domain on it.
type Config struct {
	Governor *governor.Governor
	// Domain names this runtime's domain to the governor, for the per-domain
	// share of the node ceiling.
	Domain string
	// Record is the scheduler's catch-up record (default in memory).
	Record scheduler.Record
	Clock  scheduler.Clock
	Rand   func() float64
	Logger *slog.Logger
}

// Runtime adopts node roles and runs their duties.
type Runtime struct {
	cfg   Config
	sched *scheduler.Scheduler
	log   *slog.Logger

	mu      sync.Mutex
	roles   map[string]*Adopted
	started bool
	stopped bool
}

// New creates a Runtime.
func New(cfg Config) (*Runtime, error) {
	if cfg.Governor == nil {
		return nil, errors.New("noderoles: a Governor is required")
	}
	rt := &Runtime{cfg: cfg, log: cfg.Logger, roles: map[string]*Adopted{}}
	if rt.log == nil {
		rt.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s, err := scheduler.New(scheduler.Config{Dispatch: rt.dispatch, Record: cfg.Record, Clock: cfg.Clock, Rand: cfg.Rand, Logger: rt.log})
	if err != nil {
		return nil, err
	}
	rt.sched = s
	return rt, nil
}

// Adopted is a node role this runtime runs.
type Adopted struct {
	rt   *Runtime
	def  Definition
	adpt Adoption
	role *governor.Role
	// Limits are what the node role really runs under, after the node's
	// ceilings; Clamps lists every dimension a ceiling reduced.
	Limits governor.Limits
	Clamps []governor.Clamp

	duties map[string]Duty

	unauthorized atomic.Uint64
	notHeld      atomic.Uint64
	closeOnce    sync.Once
}

// Adopt runs the node's side of adoption: state a budget, be authorized,
// register with the governor, set up, and schedule the duties. If any step
// fails nothing is left behind, and no other node role is affected.
func (rt *Runtime) Adopt(ctx context.Context, def Definition, a Adoption) (*Adopted, error) {
	if def.Key == "" {
		return nil, errors.New("noderoles: a node role needs a Key")
	}
	if len(def.Duties) == 0 {
		return nil, fmt.Errorf("noderoles: node role %q has no duties", def.Key)
	}
	seen := map[string]bool{}
	for _, d := range def.Duties {
		if d.Name == "" || d.Run == nil {
			return nil, fmt.Errorf("noderoles: node role %q has a duty with no name or no Run", def.Key)
		}
		if seen[d.Name] {
			return nil, fmt.Errorf("noderoles: node role %q has two duties named %q", def.Key, d.Name)
		}
		seen[d.Name] = true
	}
	if def.Exclusive && a.Gate == nil {
		return nil, fmt.Errorf("%w: %s", ErrNeedsGate, def.Key)
	}
	for name := range a.Schedules {
		if !seen[name] {
			return nil, fmt.Errorf("noderoles: node role %q has no duty %q to give a schedule to", def.Key, name)
		}
	}

	rt.mu.Lock()
	if rt.stopped {
		rt.mu.Unlock()
		return nil, ErrClosed
	}
	if _, dup := rt.roles[def.Key]; dup {
		rt.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrAlreadyAdopted, def.Key)
	}
	rt.mu.Unlock()

	requested, err := governor.ResolveBudget(a.Budget, def.Tiers, def.Default)
	if err != nil {
		return nil, fmt.Errorf("noderoles: adopting %q: %w", def.Key, err)
	}
	if a.Authorize != nil {
		if err := a.Authorize(ctx, def.Permissions); err != nil {
			return nil, fmt.Errorf("%w: %s needs %s: %v", ErrNotAuthorized, def.Key, strings.Join(def.Permissions, ", "), err)
		}
	}
	prio := a.Priority
	if prio == "" {
		prio = def.Priority
	}
	role, err := rt.cfg.Governor.Register(governor.RoleSpec{Key: def.Key, Domain: rt.cfg.Domain, Requested: requested, Priority: prio})
	if err != nil {
		return nil, fmt.Errorf("noderoles: adopting %q: %w", def.Key, err)
	}
	ad := &Adopted{rt: rt, def: def, adpt: a, role: role, Limits: role.Limits(), Clamps: role.Clamps(), duties: map[string]Duty{}}
	// Complete before anything can look it up: a started runtime may fire the
	// first duty while the later ones are still being scheduled.
	for _, d := range def.Duties {
		ad.duties[d.Name] = d
	}
	undo := func() { role.Unregister() }

	if def.Setup != nil {
		if err := def.Setup(ctx); err != nil {
			undo()
			return nil, fmt.Errorf("noderoles: setting up %q: %w", def.Key, err)
		}
	}
	undo = func() {
		role.Unregister()
		if def.Teardown != nil {
			_ = def.Teardown(context.WithoutCancel(ctx))
		}
	}

	rt.mu.Lock()
	rt.roles[def.Key] = ad
	rt.mu.Unlock()

	var added []string
	for _, d := range def.Duties {
		sched := d.Schedule
		if o, ok := a.Schedules[d.Name]; ok {
			sched = o
		}
		if err := rt.sched.Add(scheduler.Duty{
			Role: def.Key, Name: d.Name, Schedule: sched, Events: d.Events, Jitter: d.Jitter,
			Overlap: d.Overlap, CatchUp: d.CatchUp, MaxCatchUp: d.MaxCatchUp, DeferRetry: d.DeferRetry, Run: d.Run,
		}); err != nil {
			for _, n := range added {
				rt.sched.Remove(def.Key, n)
			}
			rt.mu.Lock()
			delete(rt.roles, def.Key)
			rt.mu.Unlock()
			undo()
			return nil, fmt.Errorf("noderoles: scheduling %q/%s: %w", def.Key, d.Name, err)
		}
		added = append(added, d.Name)
	}
	rt.log.Info("node role adopted", "node_role", def.Key, "domain", rt.cfg.Domain, "limits", fmt.Sprintf("%+v", ad.Limits), "clamps", len(ad.Clamps))
	return ad, nil
}

// Start begins scheduling every adopted node role's duties.
func (rt *Runtime) Start() error {
	rt.mu.Lock()
	if rt.stopped {
		rt.mu.Unlock()
		return ErrClosed
	}
	rt.started = true
	rt.mu.Unlock()
	return rt.sched.Start()
}

// Stop stops scheduling, closes every adopted node role (running Teardown),
// and returns. Runs the governor already holds are the governor's to finish
// or cancel.
func (rt *Runtime) Stop(ctx context.Context) {
	rt.mu.Lock()
	if rt.stopped {
		rt.mu.Unlock()
		return
	}
	rt.stopped = true
	all := make([]*Adopted, 0, len(rt.roles))
	for _, a := range rt.roles {
		all = append(all, a)
	}
	rt.mu.Unlock()
	rt.sched.Stop()
	for _, a := range all {
		_ = a.Close(ctx)
	}
}

// Emit raises an event for duties subscribed to it.
func (rt *Runtime) Emit(event string) { rt.sched.Emit(event) }

// Close stops this node role: its duties are no longer scheduled, its waiting
// work is shed, and Teardown runs. Idempotent.
func (a *Adopted) Close(ctx context.Context) error {
	var err error
	a.closeOnce.Do(func() {
		for name := range a.duties {
			a.rt.sched.Remove(a.def.Key, name)
		}
		a.role.Unregister()
		a.rt.mu.Lock()
		delete(a.rt.roles, a.def.Key)
		a.rt.mu.Unlock()
		if a.def.Teardown != nil {
			err = a.def.Teardown(ctx)
		}
	})
	return err
}

// dispatch is the scheduler's Dispatcher: it turns "this duty is due" into a
// piece of governed work.
func (rt *Runtime) dispatch(roleKey, duty string, run func(ctx context.Context) error) scheduler.Handle {
	rt.mu.Lock()
	a := rt.roles[roleKey]
	rt.mu.Unlock()
	if a == nil {
		return scheduler.Handle{Disposition: scheduler.Dropped}
	}
	d := a.duties[duty]
	ticket, err := a.role.Submit(governor.Work{
		Duty: duty, Priority: d.Priority, Overflow: d.Overflow, Key: duty,
		Run: func(ctx context.Context) error { return a.guarded(ctx, run) },
	})
	if err != nil {
		return scheduler.Handle{Disposition: scheduler.Dropped}
	}
	select {
	case <-ticket.Done():
		if r := ticket.Result(); r.State == governor.Shed {
			if r.Reason == governor.ShedDeferred {
				return scheduler.Handle{Disposition: scheduler.Deferred}
			}
			return scheduler.Handle{Disposition: scheduler.Dropped}
		}
	default:
	}
	return scheduler.Handle{Disposition: scheduler.Accepted, Done: ticket.Done()}
}

// guarded applies the two checks that must hold at the moment a run starts,
// not just when the node role was adopted: that an exclusive node role is
// still held, and that the node is still authorized.
func (a *Adopted) guarded(ctx context.Context, run func(context.Context) error) error {
	if g := a.adpt.Gate; g != nil {
		lost, held := g.Acquire()
		if !held {
			a.notHeld.Add(1)
			return nil // not an error: another node holds it
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			select {
			case <-lost:
				cancel()
			case <-stop:
			}
		}()
	}
	if a.adpt.Authorize != nil {
		if err := a.adpt.Authorize(ctx, a.def.Permissions); err != nil {
			a.unauthorized.Add(1)
			a.rt.log.Warn("a duty did not run: the node is no longer authorized", "node_role", a.def.Key, "error", err.Error())
			return fmt.Errorf("%w: %v", ErrNotAuthorized, err)
		}
	}
	return run(ctx)
}

// RoleStatus is a snapshot of one node role for reporting (Vitals readings,
// logs): what it really runs under, what was capped, and what has happened.
type RoleStatus struct {
	Key    string
	Limits governor.Limits
	Clamps []governor.Clamp
	// Governor is the node role's running and waiting work and shed counts.
	Governor governor.RoleStats
	Duties   []scheduler.DutyStats
	// Unauthorized counts runs refused because the node lost its
	// permissions; NotHeld counts runs skipped because an exclusive node
	// role was held elsewhere.
	Unauthorized, NotHeld uint64
}

// Status snapshots every adopted node role.
func (rt *Runtime) Status() []RoleStatus {
	rt.mu.Lock()
	roles := make([]*Adopted, 0, len(rt.roles))
	for _, a := range rt.roles {
		roles = append(roles, a)
	}
	rt.mu.Unlock()
	gov := map[string]governor.RoleStats{}
	for _, rs := range rt.cfg.Governor.Stats().Roles {
		if rs.Domain == rt.cfg.Domain {
			gov[rs.Key] = rs
		}
	}
	duties := map[string][]scheduler.DutyStats{}
	for _, ds := range rt.sched.Stats() {
		duties[ds.Role] = append(duties[ds.Role], ds)
	}
	out := make([]RoleStatus, 0, len(roles))
	for _, a := range roles {
		out = append(out, RoleStatus{
			Key: a.def.Key, Limits: a.Limits, Clamps: a.Clamps, Governor: gov[a.def.Key], Duties: duties[a.def.Key],
			Unauthorized: a.unauthorized.Load(), NotHeld: a.notHeld.Load(),
		})
	}
	return out
}
