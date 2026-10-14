package registrynoderole

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/google/uuid"
)

// LeaseGate is a registry lease presented as the Gate an exclusive node role
// needs: Acquire says whether this node holds the lease now, and hands back a
// channel that closes when it stops.
//
// It is conservative on purpose. The node believes it holds the lease only
// until a deadline it computes from its own clock *before* asking, which is
// never later than when the registry will let it go; if renewals stop working
// the node stops believing before the registry would let someone else in. The
// lease is still a liveness primitive, not a fencing token (13): after expiry a
// slow previous holder can briefly overlap a new one, so the duties of an
// exclusive node role must be idempotent.
type LeaseGate struct {
	writer   facade.Writer
	group    string
	name     string
	ttl      time.Duration
	instance func() uuid.UUID
	now      func() time.Time

	mu    sync.Mutex
	held  bool
	until time.Time
	lost  chan struct{}
}

// LeaseConfig configures a LeaseGate.
type LeaseConfig struct {
	Writer facade.Writer
	// Group and Name identify the lease; TTL is how long each renewal lasts
	// (the gate renews every third of it).
	Group, Name string
	TTL         time.Duration
	// Instance is the holder, normally Membership.InstanceID.
	Instance func() uuid.UUID
	// Now is the gate's clock (default time.Now).
	Now func() time.Time
}

// NewLeaseGate validates the config.
func NewLeaseGate(cfg LeaseConfig) (*LeaseGate, error) {
	switch {
	case cfg.Writer == nil || cfg.Instance == nil:
		return nil, errors.New("registrynoderole: a lease needs a Writer and an Instance")
	case cfg.Group == "" || cfg.Name == "":
		return nil, errors.New("registrynoderole: a lease needs a Group and a Name")
	case cfg.TTL < 3*time.Millisecond:
		return nil, errors.New("registrynoderole: a lease TTL must be positive and not trivially short")
	}
	g := &LeaseGate{writer: cfg.Writer, group: cfg.Group, name: cfg.Name, ttl: cfg.TTL, instance: cfg.Instance, now: cfg.Now}
	if g.now == nil {
		g.now = time.Now
	}
	return g, nil
}

// Acquire implements noderoles.Gate.
func (g *LeaseGate) Acquire() (<-chan struct{}, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held && !g.now().Before(g.until) {
		g.dropLocked() // renewals stopped working: stop believing before the registry lets go
	}
	if !g.held {
		return nil, false
	}
	return g.lost, true
}

func (g *LeaseGate) dropLocked() {
	if g.held {
		g.held = false
		close(g.lost)
	}
}

// Renew acquires the lease or extends it. Losing it, because someone else
// holds it or the registry says no, ends the gate's hold at once; a transient
// error leaves the hold to run out on its own deadline.
func (g *LeaseGate) Renew(ctx context.Context) error {
	begin := g.now()
	_, ok, err := facade.AcquireOrRenewLease(ctx, g.writer, g.group, g.name, g.instance(), g.ttl)
	g.mu.Lock()
	defer g.mu.Unlock()
	if err != nil {
		return err
	}
	if !ok {
		g.dropLocked()
		return nil
	}
	if !g.held {
		g.held = true
		g.lost = make(chan struct{})
	}
	g.until = begin.Add(g.ttl)
	return nil
}

// Release gives the lease up (a graceful stop) and ends the gate's hold.
func (g *LeaseGate) Release(ctx context.Context) error {
	g.mu.Lock()
	g.dropLocked()
	g.mu.Unlock()
	return facade.ReleaseLease(ctx, g.writer, g.group, g.name, g.instance())
}

// Definition is the node role that keeps the lease renewed. Adopt it after
// Membership (the holder is the membership's Instance), then give the gate to
// the exclusive node role that needs it.
func (g *LeaseGate) Definition() noderoles.Definition {
	every, _ := recurrence.Every(g.ttl/3, time.Time{})
	limits := governor.Limits{Concurrency: 1, QueueDepth: 0, RunTimeout: g.ttl}
	return noderoles.Definition{
		Key:         fmt.Sprintf("archipelago.registry.lease.%s.%s", g.group, g.name),
		Description: "Hold the lease on " + g.group + "/" + g.name + " for this node",
		Tiers:       governor.TierTable{governor.TierLow: limits, governor.TierMedium: limits, governor.TierHigh: limits},
		Default:     &limits,
		Priority:    governor.Critical, // losing the lease because a renewal was shed would hand it away needlessly
		Duties: []noderoles.Duty{{
			Name: "renew", Schedule: every,
			Overlap: scheduler.OverlapSkip, CatchUp: scheduler.CatchUpSkip, Overflow: governor.OverflowDrop,
			Run: func(ctx context.Context, _ scheduler.Fire) error { return g.Renew(ctx) },
		}},
		// Try at once, so a node does not wait a third of a TTL to take a free lease.
		Setup:    func(ctx context.Context) error { return g.Renew(ctx) },
		Teardown: g.Release,
	}
}
