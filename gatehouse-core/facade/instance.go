package facade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// ErrInstanceNotFound is returned by Heartbeat when instanceID names no
// registered instance at all — distinct from a stale-but-still-present
// row, which ListInstancesByGroup's own activeSince cutoff handles.
var ErrInstanceNotFound = errors.New("facade: instance not found")

// RegisterInstance registers a brand-new Instance for principalID —
// always a fresh InstanceID, never idempotent-by-anything, per
// 13-registry-and-leases-model.md: a restarted process is a new
// instance, not a resumed one.
func RegisterInstance(ctx context.Context, writer Writer, principalID uuid.UUID, group string, metadata json.RawMessage) (structure.Instance, error) {
	now := time.Now().Truncate(time.Microsecond)
	i := structure.Instance{
		InstanceID:      uuid.New(),
		PrincipalID:     principalID,
		Group:           group,
		Metadata:        metadata,
		RegisteredAt:    now,
		LastHeartbeatAt: now,
	}
	if err := i.Validate(); err != nil {
		return structure.Instance{}, fmt.Errorf("facade: register instance: %w", err)
	}
	if err := writer.UpsertInstance(ctx, i); err != nil {
		return structure.Instance{}, fmt.Errorf("facade: register instance: %w", err)
	}
	return i, nil
}

// Heartbeat bumps instanceID's LastHeartbeatAt to now, keeping every
// other field as already registered. ErrInstanceNotFound if the
// instance was deregistered (or never existed) — a caller that gets
// this back has lost its registration and needs to call
// RegisterInstance again, not retry the same ID.
func Heartbeat(ctx context.Context, reader Reader, writer Writer, instanceID uuid.UUID) (structure.Instance, error) {
	existing, found, err := reader.GetInstance(ctx, instanceID)
	if err != nil {
		return structure.Instance{}, fmt.Errorf("facade: heartbeat: %w", err)
	}
	if !found {
		return structure.Instance{}, ErrInstanceNotFound
	}
	existing.LastHeartbeatAt = time.Now().Truncate(time.Microsecond)
	if err := writer.UpsertInstance(ctx, existing); err != nil {
		return structure.Instance{}, fmt.Errorf("facade: heartbeat: %w", err)
	}
	return existing, nil
}

// Deregister removes instanceID's row outright — the graceful-shutdown
// path; a crashed instance ages out of ListPeers's own staleness cutoff
// instead of needing this called on its behalf.
func Deregister(ctx context.Context, writer Writer, instanceID uuid.UUID) error {
	if err := writer.DeleteInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("facade: deregister: %w", err)
	}
	return nil
}

// ListPeers returns every instance in group whose heartbeat is no
// older than activeWithin — the caller's own definition of "still
// alive," per 13-registry-and-leases-model.md.
func ListPeers(ctx context.Context, reader Reader, group string, activeWithin time.Duration) ([]structure.Instance, error) {
	instances, err := reader.ListInstancesByGroup(ctx, group, time.Now().Add(-activeWithin))
	if err != nil {
		return nil, fmt.Errorf("facade: list peers: %w", err)
	}
	return instances, nil
}

// AcquireOrRenewLease claims the lease on (group, name) for
// holderInstanceID, valid for ttl from now — the same call whether
// holderInstanceID is claiming it fresh or renewing one it already
// holds, since the underlying CAS already covers both: ok is false,
// with no error, when someone else holds an unexpired lease.
func AcquireOrRenewLease(ctx context.Context, writer Writer, group, name string, holderInstanceID uuid.UUID, ttl time.Duration) (structure.Lease, bool, error) {
	now := time.Now().Truncate(time.Microsecond)
	lease, ok, err := writer.AcquireOrRenewLease(ctx, group, name, holderInstanceID, now, now.Add(ttl))
	if err != nil {
		return structure.Lease{}, false, fmt.Errorf("facade: acquire or renew lease: %w", err)
	}
	return lease, ok, nil
}

// ReleaseLease releases the lease on (group, name) if holderInstanceID
// currently holds it — unconditionally successful either way, per
// 13-registry-and-leases-model.md.
func ReleaseLease(ctx context.Context, writer Writer, group, name string, holderInstanceID uuid.UUID) error {
	if err := writer.ReleaseLease(ctx, group, name, holderInstanceID); err != nil {
		return fmt.Errorf("facade: release lease: %w", err)
	}
	return nil
}
