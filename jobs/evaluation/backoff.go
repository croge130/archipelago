package evaluation

import (
	"time"

	"github.com/croge130/archipelago/jobs/structure"
)

// Delay is how long to wait before the next attempt, given attempt, the
// number of attempts already made (so the first retry follows attempt
// 1). Fixed policies always return Base. Exponential policies double
// from Base and never exceed Max; the doubling is overflow-safe, so an
// absurd attempt count cannot wrap around to a negative or tiny delay.
// An invalid policy returns zero, which a caller treats as "retry now"
// only after Validate has already passed it.
func Delay(p structure.BackoffPolicy, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	switch p.Kind {
	case structure.BackoffFixed:
		return p.Base
	case structure.BackoffExponential:
		d := p.Base
		for i := 1; i < attempt; i++ {
			if d >= p.Max || d > p.Max/2 {
				return p.Max
			}
			d *= 2
		}
		if d > p.Max {
			return p.Max
		}
		return d
	default:
		return 0
	}
}
