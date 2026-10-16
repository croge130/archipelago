package noderolespolicy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/policy/evaluation"
	"github.com/croge130/archipelago/recurrence"
)

// Report says what one Apply did.
type Report struct {
	Role string
	// Changed is false when policy already matched what was running and
	// nothing was touched.
	Changed bool
	// Budget is set when the governor budget was reconfigured.
	Budget *governor.Limits
	// Clamps are the ceilings' (node and domain) further reductions.
	Clamps []governor.Clamp
	// Capped are policy requests above what the node adopted.
	Capped []Cap
	// Rescheduled lists duties given a policy interval; Restored lists duties
	// returned to their adopted schedule.
	Rescheduled []string
	Restored    []string
	// Floored lists duties whose requested interval was raised to their
	// MinInterval.
	Floored []string
	// Disabled and Enabled list duties policy switched off and back on.
	Disabled []string
	Enabled  []string
	// Notes explain policy values that could not apply.
	Notes []string
}

// BindingConfig configures a Binding.
type BindingConfig struct {
	Store evaluation.Store
	// Domain is the domain whose policy applies; empty applies only global and
	// per-role policy.
	Domain string
	// Clock anchors new interval schedules (default time.Now).
	Clock  func() time.Time
	Logger *slog.Logger
}

// Binding applies policy to adopted node roles and remembers what it applied.
type Binding struct {
	res   *Resolver
	clock func() time.Time
	log   *slog.Logger

	mu    sync.Mutex
	state map[string]*roleState

	notesMu sync.Mutex
	notes   map[string][]string
}

type roleState struct {
	adopted *noderoles.Adopted
	budget  governor.Limits
	duties  map[string]*dutyState
}

type dutyState struct {
	interval     time.Duration
	disabledByUs bool
}

// NewBinding returns a Binding.
func NewBinding(cfg BindingConfig) (*Binding, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("noderolespolicy: a Binding needs a policy Store")
	}
	b := &Binding{res: NewResolver(cfg.Store, cfg.Domain), clock: cfg.Clock, log: cfg.Logger, state: map[string]*roleState{}, notes: map[string][]string{}}
	if b.clock == nil {
		b.clock = time.Now
	}
	if b.log == nil {
		b.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return b, nil
}

// Apply resolves policy for a node role and brings the running node role in
// line with it. If resolving fails nothing is applied and the node role keeps
// running as it was.
func (b *Binding) Apply(ctx context.Context, a *noderoles.Adopted) (Report, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := a.Key()
	if key == RefreshKey {
		// Policy never configures the node role that applies policy: one
		// disabled by policy could never be re-enabled by it.
		return Report{Role: key}, nil
	}
	resolved, err := b.res.Resolve(ctx, a)
	if err != nil {
		b.log.Warn("node role policy could not be resolved; the node role keeps its current settings",
			"role", key, "error", err)
		b.setNotes(key, []string{"policy could not be read, so the node role keeps its current settings: " + err.Error()})
		return Report{Role: key}, err
	}

	st := b.state[key]
	if st == nil || st.adopted != a {
		// First sight of this adoption (or a fresh one): what is running is
		// what the node adopted.
		st = &roleState{adopted: a, budget: a.Base(), duties: map[string]*dutyState{}}
		b.state[key] = st
	}

	rep := Report{Role: key, Capped: resolved.Capped}
	var u noderoles.Update
	if resolved.Budget != st.budget {
		bl := resolved.Budget
		u.Budget = &bl
	}

	type pending struct {
		name  string
		state dutyState
	}
	var next []pending
	for _, d := range a.Definition().Duties {
		c := resolved.Duties[d.Name]
		ds := st.duties[d.Name]
		if ds == nil {
			ds = &dutyState{}
		}
		nd := *ds
		if c.Note != "" {
			rep.Notes = append(rep.Notes, c.Note)
		}
		if c.Interval != ds.interval {
			if c.Interval > 0 {
				s, err := recurrence.Every(c.Interval, b.clock())
				if err != nil {
					return rep, fmt.Errorf("noderolespolicy: %s/%s: %w", key, d.Name, err)
				}
				if u.Schedules == nil {
					u.Schedules = map[string]recurrence.Schedule{}
				}
				u.Schedules[d.Name] = s
				rep.Rescheduled = append(rep.Rescheduled, d.Name)
				if c.Floored {
					rep.Floored = append(rep.Floored, d.Name)
					rep.Notes = append(rep.Notes, fmt.Sprintf("duty %q: policy asked for every %s, raised to its floor of %s", d.Name, c.Requested, c.Interval))
				}
			} else {
				if u.Schedules == nil {
					u.Schedules = map[string]recurrence.Schedule{}
				}
				u.Schedules[d.Name] = a.DefaultSchedule(d.Name)
				rep.Restored = append(rep.Restored, d.Name)
			}
			nd.interval = c.Interval
		}
		wantOff := c.Enabled != nil && !*c.Enabled
		if wantOff && !ds.disabledByUs {
			if u.Enabled == nil {
				u.Enabled = map[string]bool{}
			}
			u.Enabled[d.Name] = false
			rep.Disabled = append(rep.Disabled, d.Name)
			nd.disabledByUs = true
		} else if !wantOff && ds.disabledByUs {
			if u.Enabled == nil {
				u.Enabled = map[string]bool{}
			}
			u.Enabled[d.Name] = true
			rep.Enabled = append(rep.Enabled, d.Name)
			nd.disabledByUs = false
		}
		next = append(next, pending{d.Name, nd})
	}

	b.setNotes(key, append(capNotes(resolved.Capped), rep.Notes...))
	for _, c := range resolved.Capped {
		b.log.Warn("policy asked for more than the node adopted; capped",
			"role", key, "dimension", c.Dimension, "asked", c.Asked, "allowed", c.Allowed)
	}
	for _, n := range rep.Notes {
		b.log.Warn("node role policy note", "role", key, "note", n)
	}

	if u.Budget == nil && len(u.Schedules) == 0 && len(u.Enabled) == 0 {
		b.log.Debug("node role policy unchanged", "role", key)
		return rep, nil
	}

	clamps, err := a.Reconfigure(u)
	if err != nil {
		b.log.Error("applying node role policy failed", "role", key, "error", err)
		return rep, fmt.Errorf("noderolespolicy: applying policy to %q: %w", key, err)
	}
	rep.Changed, rep.Clamps = true, clamps
	rep.Budget = u.Budget
	if u.Budget != nil {
		st.budget = *u.Budget
	}
	for _, p := range next {
		ds := p.state
		st.duties[p.name] = &ds
	}
	for _, c := range clamps {
		b.log.Warn("a ceiling reduced the policy budget", "role", key, "clamp", c.String())
	}
	b.log.Info("node role policy applied", "role", key,
		"budget_changed", u.Budget != nil, "rescheduled", rep.Rescheduled, "restored", rep.Restored,
		"disabled", rep.Disabled, "enabled", rep.Enabled, "floored", rep.Floored)
	return rep, nil
}

// Forget drops what the Binding remembers of a node role (after it is closed).
func (b *Binding) Forget(role string) {
	b.mu.Lock()
	delete(b.state, role)
	b.mu.Unlock()
	b.setNotes(role, nil)
}

// Notes says what policy could not apply as asked for a node role at its last
// Apply: a request capped at the adoption, an interval raised to a duty's
// floor, a value that does not fit the duty, or policy being unreadable. It is
// empty when policy was applied as asked, and does not wait for an Apply in
// progress.
func (b *Binding) Notes(role string) []string {
	b.notesMu.Lock()
	defer b.notesMu.Unlock()
	return append([]string(nil), b.notes[role]...)
}

func (b *Binding) setNotes(role string, notes []string) {
	b.notesMu.Lock()
	defer b.notesMu.Unlock()
	if len(notes) == 0 {
		delete(b.notes, role)
		return
	}
	b.notes[role] = notes
}

func capNotes(caps []Cap) []string {
	var out []string
	for _, c := range caps {
		out = append(out, c.String())
	}
	return out
}
