package evaluation

import "github.com/croge130/archipelago/vitals/structure"

// IsNotableTransition reports whether current is worth a history row
// given previous (hadPrevious is false for an instance's first-ever
// reading, itself always notable). Narrowed from Lighthouse's own
// trigger set to match this port's trimmed Reading shape — no
// LogEntryID/NarrativeID fields exist here to change. Repeated
// same-state updates refresh the current reading only.
func IsNotableTransition(previous, current structure.Reading, hadPrevious bool) bool {
	if !hadPrevious {
		return true
	}
	return previous.State != current.State ||
		previous.Impact != current.Impact ||
		!impactScoresEqual(previous.ImpactScore, current.ImpactScore) ||
		previous.Summary != current.Summary ||
		previous.ReasonCode != current.ReasonCode
}

func impactScoresEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
