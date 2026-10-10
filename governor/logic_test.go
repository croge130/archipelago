package governor

import (
	"errors"
	"testing"
	"time"
)

func TestPriorityOrderAndMin(t *testing.T) {
	order := []Priority{Background, Normal, Important, Critical}
	for i := 1; i < len(order); i++ {
		if order[i].Rank() <= order[i-1].Rank() {
			t.Errorf("%s should outrank %s", order[i], order[i-1])
		}
	}
	if Min(Critical, Normal) != Normal || Min(Background, Important) != Background || Min(Normal, Normal) != Normal {
		t.Error("Min must return the lower priority: a remote hint can never exceed the node's cap")
	}
	if Priority("urgent!!").Rank() != Normal.Rank() || Priority("urgent!!").Valid() {
		t.Error("an unknown priority must rank as Normal and be invalid, so a typo cannot jump the queue")
	}
}

func TestPressureLowersAdmissionOnly(t *testing.T) {
	cases := []struct {
		p    Pressure
		prio Priority
		ok   bool
	}{
		{PressureNone, Background, true},
		{PressureElevated, Background, false}, {PressureElevated, Normal, true},
		{PressureHigh, Normal, false}, {PressureHigh, Important, true}, {PressureHigh, Critical, true},
	}
	for _, c := range cases {
		if got := c.p.admits(c.prio); got != c.ok {
			t.Errorf("pressure %s admits %s = %v, want %v", c.p, c.prio, got, c.ok)
		}
	}
}

// Aging: one level per step waited, capped at Important so aged background
// work never displaces critical, and work already at the cap does not age.
func TestAgingRisesOneLevelPerStepAndStopsBelowTheTop(t *testing.T) {
	step := 10 * time.Second
	cases := []struct {
		base   Priority
		waited time.Duration
		want   Priority
	}{
		{Background, 0, Background}, {Background, 9 * time.Second, Background},
		{Background, 10 * time.Second, Normal}, {Background, 20 * time.Second, Important},
		{Background, time.Hour, Important}, {Normal, time.Hour, Important},
		{Important, time.Hour, Important}, {Critical, time.Hour, Critical},
	}
	for _, c := range cases {
		if got := effectivePriority(c.base, c.waited, step); got != c.want.Rank() {
			t.Errorf("%s waited %v: effective rank %d, want %d (%s)", c.base, c.waited, got, c.want.Rank(), c.want)
		}
	}
	if effectivePriority(Background, time.Hour, 0) != Background.Rank() {
		t.Error("a zero age step must disable aging, not divide by zero")
	}
}

func TestClampToCapsEachDimensionAndReportsIt(t *testing.T) {
	req := Limits{Concurrency: 8, QueueDepth: 50, Rate: Rate{Starts: 100, Per: time.Second}, RunTimeout: time.Hour}
	ceil := Ceiling{Concurrency: 2, MaxQueueDepth: 10, MaxRate: Rate{Starts: 5, Per: time.Second}, MaxRunTimeout: time.Minute}
	got, clamps := ClampTo(req, ceil, "node")
	if got.Concurrency != 2 || got.QueueDepth != 10 || got.Rate != (Rate{5, time.Second}) || got.RunTimeout != time.Minute {
		t.Errorf("clamped to %+v", got)
	}
	if len(clamps) != 4 {
		t.Errorf("%d clamps reported, want one per dimension: %v", len(clamps), clamps)
	}
	for _, c := range clamps {
		if c.By != "node" || c.Requested == c.Effective {
			t.Errorf("bad clamp record %+v", c)
		}
	}

	// A request already inside the ceiling is untouched and reports nothing.
	small := Limits{Concurrency: 1, QueueDepth: 1, Rate: Rate{1, time.Second}, RunTimeout: time.Second}
	if got, clamps := ClampTo(small, ceil, "node"); got != small || len(clamps) != 0 {
		t.Errorf("a request within the ceiling changed: %+v %v", got, clamps)
	}

	// Unbounded in a dimension the ceiling bounds: the ceiling's value applies.
	got, clamps = ClampTo(Limits{Concurrency: 1}, ceil, "domain")
	if got.Rate != ceil.MaxRate || got.RunTimeout != ceil.MaxRunTimeout || len(clamps) != 2 {
		t.Errorf("unbounded request under a bounding ceiling: %+v %v", got, clamps)
	}
	// No ceiling in a dimension means no cap there.
	got, _ = ClampTo(req, Ceiling{Concurrency: 100}, "node")
	if got != req {
		t.Errorf("a ceiling with only Concurrency changed other dimensions: %+v", got)
	}
}

func TestResolveBudgetNeverInventsADefault(t *testing.T) {
	tiers := TierTable{
		TierLow:  {Concurrency: 1, QueueDepth: 2},
		TierHigh: {Concurrency: 8, QueueDepth: 100, RunTimeout: time.Minute},
	}
	def := &Limits{Concurrency: 2, QueueDepth: 4}

	if _, err := ResolveBudget(Adoption{}, tiers, def); !errors.Is(err, ErrNoBudgetStated) {
		t.Errorf("an adoption stating nothing: %v, want ErrNoBudgetStated", err)
	}
	if _, err := ResolveBudget(Adoption{Tier: TierLow, UseDefault: true}, tiers, def); !errors.Is(err, ErrAmbiguousBudget) {
		t.Errorf("a tier and the default together: %v", err)
	}
	if _, err := ResolveBudget(Adoption{Tier: TierMedium}, tiers, def); !errors.Is(err, ErrUnknownTier) {
		t.Errorf("a tier the role never defined: %v", err)
	}
	if _, err := ResolveBudget(Adoption{UseDefault: true}, tiers, nil); err == nil {
		t.Error("UseDefault with no declared default was accepted")
	}

	if got, err := ResolveBudget(Adoption{Tier: TierHigh}, tiers, def); err != nil || got != tiers[TierHigh] {
		t.Errorf("tier high: %+v %v", got, err)
	}
	if got, err := ResolveBudget(Adoption{UseDefault: true}, tiers, def); err != nil || got != *def {
		t.Errorf("default: %+v %v", got, err)
	}
	// Explicit numbers override individual dimensions of a tier.
	got, err := ResolveBudget(Adoption{Tier: TierHigh, Override: &Limits{Concurrency: 3}}, tiers, def)
	if err != nil || got.Concurrency != 3 || got.QueueDepth != 100 || got.RunTimeout != time.Minute {
		t.Errorf("tier with override: %+v %v", got, err)
	}
	// Or stand alone if complete.
	if got, err := ResolveBudget(Adoption{Override: &Limits{Concurrency: 2}}, tiers, nil); err != nil || got.Concurrency != 2 {
		t.Errorf("explicit limits alone: %+v %v", got, err)
	}
	if _, err := ResolveBudget(Adoption{Override: &Limits{QueueDepth: 5}}, tiers, nil); err == nil {
		t.Error("explicit limits without a concurrency were accepted")
	}
}

func TestLimitsAndCeilingValidation(t *testing.T) {
	bad := []Limits{
		{Concurrency: 0}, {Concurrency: 1, QueueDepth: -1}, {Concurrency: 1, RunTimeout: -1},
		{Concurrency: 1, Rate: Rate{Starts: 5}}, {Concurrency: 1, Rate: Rate{Per: time.Second}},
	}
	for _, l := range bad {
		if l.Validate() == nil {
			t.Errorf("%+v was accepted", l)
		}
	}
	if (Limits{Concurrency: 1, Rate: Rate{1, time.Second}}).Validate() != nil {
		t.Error("a valid limits was rejected")
	}
	if (Ceiling{}).Validate() == nil || (Ceiling{Concurrency: 1, MaxRate: Rate{Starts: 1}}).Validate() == nil {
		t.Error("an invalid ceiling was accepted")
	}
}

func TestRateWindowSlides(t *testing.T) {
	r := &Role{limits: Limits{Concurrency: 1, Rate: Rate{Starts: 2, Per: 100 * time.Millisecond}}}
	t0 := time.Now()
	if _, ok := r.nextStart(t0); !ok {
		t.Fatal("blocked with no starts")
	}
	r.starts = []time.Time{t0, t0.Add(10 * time.Millisecond)}
	at, ok := r.nextStart(t0.Add(20 * time.Millisecond))
	if ok || !at.Equal(t0.Add(100*time.Millisecond)) {
		t.Errorf("a full window: ok=%v next=%v, want blocked until the oldest start leaves the window", ok, at.Sub(t0))
	}
	if _, ok := r.nextStart(t0.Add(101 * time.Millisecond)); !ok {
		t.Error("still blocked after the oldest start left the window")
	}
}
