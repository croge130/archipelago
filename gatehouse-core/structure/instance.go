package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Instance is one running process registered into a Group, tied to the
// principal its connection authenticated as. See
// docs/architecture/13-registry-and-leases-model.md for the full
// rationale, including why InstanceID is always fresh (a restarted
// process is a new instance, not a resumed one) and why liveness is
// heartbeat-based with no background sweep.
//
// Deliberately absent: any notion of a live connection, same
// discipline Session's own doc comment states for itself — connection-
// binding is an integration's job, not a field this base record
// carries.
type Instance struct {
	InstanceID      uuid.UUID
	PrincipalID     uuid.UUID
	Group           string
	Metadata        json.RawMessage
	RegisteredAt    time.Time
	LastHeartbeatAt time.Time
}

func (i Instance) Validate() error {
	if i.InstanceID == uuid.Nil {
		return fmt.Errorf("structure: instance: InstanceID is required")
	}
	if i.PrincipalID == uuid.Nil {
		return fmt.Errorf("structure: instance: PrincipalID is required")
	}
	if i.Group == "" {
		return fmt.Errorf("structure: instance: Group is required")
	}
	if i.RegisteredAt.IsZero() {
		return fmt.Errorf("structure: instance: RegisteredAt is required")
	}
	if i.LastHeartbeatAt.IsZero() {
		return fmt.Errorf("structure: instance: LastHeartbeatAt is required")
	}
	return nil
}

// Lease is a time-limited lock on Name within Group, held by one
// Instance at a time — deliberately an Instance, not a Principal, since
// the problem a lease solves is "exactly one replica," and replicas of
// the same service share a principal.
type Lease struct {
	Group            string
	Name             string
	HolderInstanceID uuid.UUID
	AcquiredAt       time.Time
	ExpiresAt        time.Time
}

func (l Lease) Validate() error {
	if l.Group == "" {
		return fmt.Errorf("structure: lease: Group is required")
	}
	if l.Name == "" {
		return fmt.Errorf("structure: lease: Name is required")
	}
	if l.HolderInstanceID == uuid.Nil {
		return fmt.Errorf("structure: lease: HolderInstanceID is required")
	}
	if l.AcquiredAt.IsZero() {
		return fmt.Errorf("structure: lease: AcquiredAt is required")
	}
	if l.ExpiresAt.IsZero() {
		return fmt.Errorf("structure: lease: ExpiresAt is required")
	}
	if !l.ExpiresAt.After(l.AcquiredAt) {
		return fmt.Errorf("structure: lease: ExpiresAt must be after AcquiredAt")
	}
	return nil
}
