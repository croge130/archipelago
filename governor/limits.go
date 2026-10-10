package governor

import (
	"errors"
	"fmt"
	"time"
)

// Rate bounds how often work may start: at most Starts per Per. The zero
// value is no rate limit.
type Rate struct {
	Starts int
	Per    time.Duration
}

func (r Rate) set() bool { return r.Starts > 0 && r.Per > 0 }

// perSecond is the rate as a comparable number.
func (r Rate) perSecond() float64 {
	if !r.set() {
		return 0
	}
	return float64(r.Starts) / r.Per.Seconds()
}

func (r Rate) String() string {
	if !r.set() {
		return "unlimited"
	}
	return fmt.Sprintf("%d per %s", r.Starts, r.Per)
}

// Limits are one node role's budget. Every dimension is something the
// governor can enforce; memory is deliberately not among them.
type Limits struct {
	// Concurrency is how many of the role's work items run at once (at least 1).
	Concurrency int
	// QueueDepth is how many may wait. Zero means none: work starts at once or
	// is shed, which is the right shape for a trigger that is pointless late.
	QueueDepth int
	// Rate bounds how often work starts. Zero value: unlimited.
	Rate Rate
	// RunTimeout bounds a single run, by context deadline. Zero: none.
	RunTimeout time.Duration
}

// Validate reports whether the limits can be enforced.
func (l Limits) Validate() error {
	switch {
	case l.Concurrency < 1:
		return errors.New("governor: Concurrency must be at least 1")
	case l.QueueDepth < 0:
		return errors.New("governor: QueueDepth must not be negative")
	case l.RunTimeout < 0:
		return errors.New("governor: RunTimeout must not be negative")
	case (l.Rate.Starts > 0) != (l.Rate.Per > 0):
		return errors.New("governor: a Rate needs both Starts and Per")
	case l.Rate.Starts < 0 || l.Rate.Per < 0:
		return errors.New("governor: a Rate must not be negative")
	}
	return nil
}

// Ceiling caps what a wider scope may ask of a narrower one: the node as a
// whole, or one domain's share of it. For the optional dimensions a zero
// means "no cap"; to forbid queueing, set the role's own QueueDepth to zero.
type Ceiling struct {
	// Concurrency is the most that run at once across everything under this
	// ceiling. Required (at least 1).
	Concurrency int
	// MaxQueueDepth caps any one role's QueueDepth. Zero: no cap.
	MaxQueueDepth int
	// MaxRate caps any one role's start rate. Zero value: no cap.
	MaxRate Rate
	// MaxRunTimeout caps any one role's per-run timeout. Zero: no cap.
	MaxRunTimeout time.Duration
}

// Validate reports whether the ceiling is usable.
func (c Ceiling) Validate() error {
	switch {
	case c.Concurrency < 1:
		return errors.New("governor: a ceiling needs Concurrency of at least 1")
	case c.MaxQueueDepth < 0 || c.MaxRunTimeout < 0:
		return errors.New("governor: a ceiling must not be negative")
	case (c.MaxRate.Starts > 0) != (c.MaxRate.Per > 0) || c.MaxRate.Starts < 0 || c.MaxRate.Per < 0:
		return errors.New("governor: a ceiling's MaxRate needs both Starts and Per")
	}
	return nil
}

// Clamp records one dimension where a ceiling changed what was asked for.
type Clamp struct {
	Dimension string
	Requested string
	Effective string
	// By names the ceiling that applied: "node" or "domain".
	By string
}

func (c Clamp) String() string {
	return fmt.Sprintf("%s: asked %s, effective %s (capped by the %s ceiling)", c.Dimension, c.Requested, c.Effective, c.By)
}

// ClampTo applies a ceiling to requested limits, per dimension, and says
// what it changed. The result is never wider than the ceiling in any
// dimension. A dimension the role left unbounded (no rate, no timeout) takes
// the ceiling's value when the ceiling has one.
func ClampTo(requested Limits, c Ceiling, by string) (Limits, []Clamp) {
	out := requested
	var clamps []Clamp
	note := func(dim, asked, eff string) { clamps = append(clamps, Clamp{dim, asked, eff, by}) }

	if out.Concurrency > c.Concurrency {
		note("concurrency", fmt.Sprint(out.Concurrency), fmt.Sprint(c.Concurrency))
		out.Concurrency = c.Concurrency
	}
	if c.MaxQueueDepth > 0 && out.QueueDepth > c.MaxQueueDepth {
		note("queue depth", fmt.Sprint(out.QueueDepth), fmt.Sprint(c.MaxQueueDepth))
		out.QueueDepth = c.MaxQueueDepth
	}
	if c.MaxRate.set() && (!out.Rate.set() || out.Rate.perSecond() > c.MaxRate.perSecond()) {
		note("rate", out.Rate.String(), c.MaxRate.String())
		out.Rate = c.MaxRate
	}
	if c.MaxRunTimeout > 0 && (out.RunTimeout == 0 || out.RunTimeout > c.MaxRunTimeout) {
		asked := "none"
		if out.RunTimeout > 0 {
			asked = out.RunTimeout.String()
		}
		note("run timeout", asked, c.MaxRunTimeout.String())
		out.RunTimeout = c.MaxRunTimeout
	}
	return out, clamps
}

// Tier is the coarse vocabulary a node's config uses for "how much": the
// author of a node role says what each tier means for that role's work.
type Tier string

const (
	TierLow    Tier = "low"
	TierMedium Tier = "medium"
	TierHigh   Tier = "high"
)

// TierTable is a node role author's definition of its tiers.
type TierTable map[Tier]Limits

// Adoption is how a node states the budget it consents to for a node role.
// There is no silent default: it names a tier or says to use the role's
// default, and may override individual dimensions with explicit numbers.
type Adoption struct {
	Tier       Tier
	UseDefault bool
	// Override replaces any dimension it sets (non-zero) on top of the tier
	// or default. With neither Tier nor UseDefault it must be complete.
	Override *Limits
}

var (
	// ErrNoBudgetStated: adopting a node role without saying how much. It
	// fails that node role only, loudly, not the node (19, decision 6).
	ErrNoBudgetStated = errors.New("governor: adoption states no budget: name a tier, say to use the default, or give explicit limits")
	// ErrAmbiguousBudget: both a tier and UseDefault.
	ErrAmbiguousBudget = errors.New("governor: adoption names both a tier and the default")
	// ErrUnknownTier: the node role does not define the tier asked for.
	ErrUnknownTier = errors.New("governor: the node role does not define that tier")
)

// ResolveBudget turns an adoption into the limits being asked for, before
// any ceiling is applied.
func ResolveBudget(a Adoption, tiers TierTable, def *Limits) (Limits, error) {
	if a.Tier != "" && a.UseDefault {
		return Limits{}, ErrAmbiguousBudget
	}
	var base Limits
	switch {
	case a.Tier != "":
		t, ok := tiers[a.Tier]
		if !ok {
			return Limits{}, fmt.Errorf("%w: %q", ErrUnknownTier, a.Tier)
		}
		base = t
	case a.UseDefault:
		if def == nil {
			return Limits{}, errors.New("governor: the node role declares no default budget")
		}
		base = *def
	case a.Override == nil:
		return Limits{}, ErrNoBudgetStated
	}
	if o := a.Override; o != nil {
		if o.Concurrency > 0 {
			base.Concurrency = o.Concurrency
		}
		if o.QueueDepth > 0 {
			base.QueueDepth = o.QueueDepth
		}
		if o.Rate.set() {
			base.Rate = o.Rate
		}
		if o.RunTimeout > 0 {
			base.RunTimeout = o.RunTimeout
		}
	}
	if err := base.Validate(); err != nil {
		return Limits{}, err
	}
	return base, nil
}
