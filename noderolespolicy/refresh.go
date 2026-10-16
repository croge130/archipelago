package noderolespolicy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
)

// RefreshKey is the node role that keeps adopted node roles in line with
// policy as it changes.
const RefreshKey = "archipelago.noderoles.policy_refresh"

// RefreshRole returns a node role that, every interval, applies policy to
// every node role the runtime has adopted (except itself). Policy has no
// change feed here, so this polling is how an edit reaches a running node
// role; a Binding that finds nothing changed touches nothing, so a short
// interval costs reads, not disturbance. Adopt it like any node role.
func RefreshRole(rt *noderoles.Runtime, b *Binding, every time.Duration, anchor time.Time) (noderoles.Definition, error) {
	sched, err := recurrence.Every(every, anchor)
	if err != nil {
		return noderoles.Definition{}, fmt.Errorf("noderolespolicy: refresh interval: %w", err)
	}
	one := governor.Limits{Concurrency: 1}
	return noderoles.Definition{
		Key:         RefreshKey,
		Description: "applies Policy to the node roles this node runs",
		Tiers:       governor.TierTable{governor.TierLow: one},
		Default:     &one,
		Duties: []noderoles.Duty{{
			Name:     "refresh",
			Schedule: sched,
			Overlap:  scheduler.OverlapSkip,
			Overflow: governor.OverflowDrop,
			Run: func(ctx context.Context, _ scheduler.Fire) error {
				return RefreshAll(ctx, rt, b)
			},
		}},
	}, nil
}

// RefreshAll applies policy to every adopted node role once. One node role
// failing does not stop the others; the errors are joined.
func RefreshAll(ctx context.Context, rt *noderoles.Runtime, b *Binding) error {
	var errs []error
	for _, a := range rt.Adopted() {
		if _, err := b.Apply(ctx, a); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
