package structure

import (
	"fmt"
	"time"
)

// State is where a job is in its life. Transition legality lives in the
// evaluation package, not here.
type State string

const (
	StatePending   State = "pending"
	StateClaimed   State = "claimed"
	StateSucceeded State = "succeeded"
	StateDead      State = "dead"
	StateCancelled State = "cancelled"
)

func (s State) Valid() bool {
	switch s {
	case StatePending, StateClaimed, StateSucceeded, StateDead, StateCancelled:
		return true
	default:
		return false
	}
}

// Terminal reports whether no further transition is possible.
func (s State) Terminal() bool {
	return s == StateSucceeded || s == StateDead || s == StateCancelled
}

// Priority is a small named, ordered set rather than a number, in the
// same coarse spirit as the budget tiers in 19. A job's priority is a
// hint: an executor applies its own cap, and a remote party cannot
// force a node to give it standing the node did not agree to.
type Priority string

const (
	PriorityBackground Priority = "background"
	PriorityNormal     Priority = "normal"
	PriorityImportant  Priority = "important"
	PriorityCritical   Priority = "critical"
)

func (p Priority) Valid() bool {
	_, ok := priorityRank[p]
	return ok
}

var priorityRank = map[Priority]int{
	PriorityBackground: 0,
	PriorityNormal:     1,
	PriorityImportant:  2,
	PriorityCritical:   3,
}

// Rank orders priorities, higher is more urgent. An invalid priority
// ranks below background.
func (p Priority) Rank() int {
	r, ok := priorityRank[p]
	if !ok {
		return -1
	}
	return r
}

// PriorityFromRank is Rank's inverse, used by storage.
func PriorityFromRank(rank int) (Priority, error) {
	for p, r := range priorityRank {
		if r == rank {
			return p, nil
		}
	}
	return "", fmt.Errorf("structure: no priority has rank %d", rank)
}

// MinPriority returns the less urgent of a and b.
func MinPriority(a, b Priority) Priority {
	if a.Rank() <= b.Rank() {
		return a
	}
	return b
}

// AuthorityMode selects whose permissions a job's operations are
// evaluated against, per 09-gatehouse-core-model.md's actor / requester
// / effective-principal model.
type AuthorityMode string

const (
	// AuthorityOwner: the effective principal is the requester, narrowed
	// to the task kind's declared scope.
	AuthorityOwner AuthorityMode = "owner"
	// AuthorityService: the effective principal is the executor itself,
	// under its own standing authority.
	AuthorityService AuthorityMode = "service"
	// AuthorityAssumed: a third principal, via an assumed session; needs
	// both consent edges.
	AuthorityAssumed AuthorityMode = "assumed"
)

func (m AuthorityMode) Valid() bool {
	switch m {
	case AuthorityOwner, AuthorityService, AuthorityAssumed:
		return true
	default:
		return false
	}
}

// BackoffKind is how the delay between attempts grows.
type BackoffKind string

const (
	BackoffFixed       BackoffKind = "fixed"
	BackoffExponential BackoffKind = "exponential"
)

// BackoffPolicy is the delay before a retry. The zero value is invalid;
// a policy always states its kind and base.
type BackoffPolicy struct {
	Kind BackoffKind
	Base time.Duration
	// Max caps an exponential delay. Zero means uncapped, which Validate
	// rejects for exponential: an unbounded retry delay is a bug, not a
	// policy.
	Max time.Duration
}

func (b BackoffPolicy) Validate() error {
	switch b.Kind {
	case BackoffFixed:
		if b.Base <= 0 {
			return fmt.Errorf("structure: backoff: fixed Base must be positive")
		}
	case BackoffExponential:
		if b.Base <= 0 {
			return fmt.Errorf("structure: backoff: exponential Base must be positive")
		}
		if b.Max < b.Base {
			return fmt.Errorf("structure: backoff: exponential Max must be at least Base")
		}
	default:
		return fmt.Errorf("structure: backoff: invalid Kind %q", b.Kind)
	}
	return nil
}

// DefaultBackoff is used when a task definition states none.
var DefaultBackoff = BackoffPolicy{Kind: BackoffExponential, Base: 5 * time.Second, Max: 5 * time.Minute}
