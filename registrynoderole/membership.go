package registrynoderole

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/google/uuid"
)

// MembershipKey is the node role's key, under the reserved archipelago namespace.
const MembershipKey = "archipelago.registry.membership"

// MembershipConfig configures the membership node role.
type MembershipConfig struct {
	Reader facade.Reader
	Writer facade.Writer

	// PrincipalID is the principal this node authenticates as in the domain;
	// the Instance belongs to it.
	PrincipalID uuid.UUID
	// Group is the instance group to join; Metadata is opaque and never
	// authorized on.
	Group    string
	Metadata json.RawMessage

	// HeartbeatEvery is how often to heartbeat (default 10s). Peers decide
	// for themselves how stale is too stale (13), so this should be well
	// inside the shortest cutoff any of them uses.
	HeartbeatEvery time.Duration

	Logger *slog.Logger
}

// Membership is the membership node role's state.
type Membership struct {
	cfg MembershipConfig
	log *slog.Logger

	mu       sync.Mutex
	instance uuid.UUID
	// reregistered counts how many times the row was lost and re-created.
	reregistered int
}

// NewMembership validates the config.
func NewMembership(cfg MembershipConfig) (*Membership, error) {
	switch {
	case cfg.Reader == nil || cfg.Writer == nil:
		return nil, errors.New("registrynoderole: a Reader and a Writer are required")
	case cfg.PrincipalID == uuid.Nil:
		return nil, errors.New("registrynoderole: a PrincipalID is required")
	case cfg.Group == "":
		return nil, errors.New("registrynoderole: a Group is required")
	case cfg.HeartbeatEvery < 0:
		return nil, errors.New("registrynoderole: HeartbeatEvery must not be negative")
	}
	if cfg.HeartbeatEvery == 0 {
		cfg.HeartbeatEvery = 10 * time.Second
	}
	m := &Membership{cfg: cfg, log: cfg.Logger}
	if m.log == nil {
		m.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return m, nil
}

// InstanceID is the node's current Instance, or the zero UUID before the node
// role has been adopted. It changes if the registration is lost and redone.
func (m *Membership) InstanceID() uuid.UUID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.instance
}

// Reregistrations is how many times the Instance row was found missing and
// created again.
func (m *Membership) Reregistrations() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reregistered
}

func (m *Membership) register(ctx context.Context) error {
	inst, err := facade.RegisterInstance(ctx, m.cfg.Writer, m.cfg.PrincipalID, m.cfg.Group, m.cfg.Metadata)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.instance = inst.InstanceID
	m.mu.Unlock()
	m.log.Info("registered in the registry", "instance_id", inst.InstanceID, "group", m.cfg.Group)
	return nil
}

func (m *Membership) heartbeat(ctx context.Context, _ scheduler.Fire) error {
	id := m.InstanceID()
	_, err := facade.Heartbeat(ctx, m.cfg.Reader, m.cfg.Writer, id)
	if errors.Is(err, facade.ErrInstanceNotFound) {
		// The row is gone (deregistered by an operator, say). The registry's
		// own contract is to register again rather than retry the old ID.
		m.log.Warn("the instance row was lost; registering again", "instance_id", id)
		if err := m.register(ctx); err != nil {
			return fmt.Errorf("re-registering: %w", err)
		}
		m.mu.Lock()
		m.reregistered++
		m.mu.Unlock()
		return nil
	}
	return err
}

// Definition is the node role a node adopts. Its budget is deliberately
// small: one heartbeat at a time, no queue (a heartbeat that cannot start now
// is superseded by the next), and a short timeout; and it is shed last, since
// a node that cannot heartbeat drops out of its peers' view.
func (m *Membership) Definition() noderoles.Definition {
	every, _ := recurrence.Every(m.cfg.HeartbeatEvery, time.Time{})
	limits := governor.Limits{Concurrency: 1, QueueDepth: 0, RunTimeout: 10 * time.Second}
	return noderoles.Definition{
		Key:         MembershipKey,
		Description: "Keep this node registered and its heartbeat fresh in the registry",
		Tiers:       governor.TierTable{governor.TierLow: limits, governor.TierMedium: limits, governor.TierHigh: limits},
		Default:     &limits,
		Priority:    governor.Important,
		Duties: []noderoles.Duty{{
			Name:     "heartbeat",
			Schedule: every,
			Jitter:   m.cfg.HeartbeatEvery / 10,
			// A late heartbeat is useless and a burst of them is worse.
			Overlap:  scheduler.OverlapSkip,
			CatchUp:  scheduler.CatchUpSkip,
			Overflow: governor.OverflowDrop,
			Run:      m.heartbeat,
		}},
		Setup: m.register,
		Teardown: func(ctx context.Context) error {
			return facade.Deregister(ctx, m.cfg.Writer, m.InstanceID())
		},
	}
}
