package governor

import (
	"fmt"
	"time"
)

// Priority is "I care about this more", a small ordered set rather than a
// number. It orders admission and shedding under contention, never preempts
// and never buys extra budget.
type Priority string

const (
	Background Priority = "background"
	Normal     Priority = "normal"
	Important  Priority = "important"
	Critical   Priority = "critical"
)

var priorityRank = map[Priority]int{Background: 0, Normal: 1, Important: 2, Critical: 3}

// Valid reports whether p is one of the four.
func (p Priority) Valid() bool { _, ok := priorityRank[p]; return ok }

// Rank orders priorities: higher is more urgent. An invalid priority ranks
// as Normal, so a typo can neither jump the queue nor starve.
func (p Priority) Rank() int {
	if r, ok := priorityRank[p]; ok {
		return r
	}
	return priorityRank[Normal]
}

// Min is the lower of two priorities. A remote party's hint is bounded by the
// cap the node set for that kind of work with Min(hint, cap), so a remote
// party cannot raise its own standing (19, "Priority").
func Min(a, b Priority) Priority {
	if a.Rank() <= b.Rank() {
		return a
	}
	return b
}

func fromRank(r int) Priority {
	for p, rank := range priorityRank {
		if rank == r {
			return p
		}
	}
	return Normal
}

// Pressure is the level of strain the application reports on itself, through
// the yield hook. It lowers admission only: it never kills running work.
type Pressure int

const (
	PressureNone Pressure = iota
	PressureElevated
	PressureHigh
)

func (p Pressure) String() string {
	switch p {
	case PressureNone:
		return "none"
	case PressureElevated:
		return "elevated"
	case PressureHigh:
		return "high"
	}
	return fmt.Sprintf("pressure(%d)", int(p))
}

// admits reports whether work of the given base priority may start under
// this pressure: at elevated, background stops; at high, anything below
// important.
func (p Pressure) admits(prio Priority) bool {
	switch p {
	case PressureElevated:
		return prio.Rank() >= Normal.Rank()
	case PressureHigh:
		return prio.Rank() >= Important.Rank()
	}
	return true
}

// DefaultAgeStep is how long work waits before it is treated as one level
// more urgent.
const DefaultAgeStep = 30 * time.Second

// effectivePriority is the rank used to order waiting work: the base rank
// plus one level per ageStep waited, but never above Important, so aged
// background work eventually runs and never displaces critical. Work already
// at or above Important does not age. Only ordering uses it; whether work is
// admitted under pressure always uses the base priority, otherwise waiting
// would defeat the pressure cutoff.
func effectivePriority(base Priority, waited, ageStep time.Duration) int {
	r := base.Rank()
	ceiling := Important.Rank()
	if r >= ceiling || ageStep <= 0 || waited <= 0 {
		return r
	}
	aged := r + int(waited/ageStep)
	if aged > ceiling {
		aged = ceiling
	}
	return aged
}
