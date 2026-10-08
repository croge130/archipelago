package evaluation

import "github.com/croge130/archipelago/jobs/structure"

var legalTransitions = map[structure.State][]structure.State{
	structure.StatePending: {
		structure.StateClaimed,
		structure.StateDead,      // expired before any run
		structure.StateCancelled, // cancel
	},
	structure.StateClaimed: {
		structure.StateSucceeded,
		structure.StatePending,   // failed or claim expired, retries remain
		structure.StateDead,      // failed or claim expired, no retries left; or authority denied
		structure.StateCancelled, // cooperative cancel acknowledged
	},
}

// CanTransition reports whether from -> to is a legal state change.
// Terminal states have no outgoing transitions.
func CanTransition(from, to structure.State) bool {
	for _, next := range legalTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// AfterFailure decides what a failed attempt, or a claim that lapsed,
// leads to. attempt is the number of attempts already made, including
// the one that just failed. terminal marks a failure that must never be
// retried (an authority denial: retrying a permission failure never
// helps). cancelRequested means someone asked for cancellation while
// the job was claimed; a request to stop wins over a retry.
//
// The storage layer implements the identical rule in one SQL statement;
// the integration tests run both and compare.
func AfterFailure(retryable bool, attempt, maxAttempts int, terminal, cancelRequested bool) structure.State {
	switch {
	case cancelRequested:
		return structure.StateCancelled
	case terminal || !retryable || attempt >= maxAttempts:
		return structure.StateDead
	default:
		return structure.StatePending
	}
}
