package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validInstance() Instance {
	now := time.Now()
	return Instance{
		InstanceID:      uuid.New(),
		PrincipalID:     uuid.New(),
		Group:           "gamebridge.workers",
		RegisteredAt:    now,
		LastHeartbeatAt: now,
	}
}

func TestInstanceValidateOK(t *testing.T) {
	if err := validInstance().Validate(); err != nil {
		t.Fatalf("expected a valid instance to validate, got: %v", err)
	}
}

func TestInstanceValidateRejectsMissingInstanceID(t *testing.T) {
	i := validInstance()
	i.InstanceID = uuid.Nil
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing InstanceID")
	}
}

func TestInstanceValidateRejectsMissingPrincipalID(t *testing.T) {
	i := validInstance()
	i.PrincipalID = uuid.Nil
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing PrincipalID")
	}
}

func TestInstanceValidateRejectsMissingGroup(t *testing.T) {
	i := validInstance()
	i.Group = ""
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing Group")
	}
}

func validLease() Lease {
	now := time.Now()
	return Lease{
		Group:            "gamebridge.workers",
		Name:             "reconciler",
		HolderInstanceID: uuid.New(),
		AcquiredAt:       now,
		ExpiresAt:        now.Add(time.Minute),
	}
}

func TestLeaseValidateOK(t *testing.T) {
	if err := validLease().Validate(); err != nil {
		t.Fatalf("expected a valid lease to validate, got: %v", err)
	}
}

func TestLeaseValidateRejectsMissingGroup(t *testing.T) {
	l := validLease()
	l.Group = ""
	if err := l.Validate(); err == nil {
		t.Fatal("expected an error for a missing Group")
	}
}

func TestLeaseValidateRejectsMissingName(t *testing.T) {
	l := validLease()
	l.Name = ""
	if err := l.Validate(); err == nil {
		t.Fatal("expected an error for a missing Name")
	}
}

func TestLeaseValidateRejectsMissingHolderInstanceID(t *testing.T) {
	l := validLease()
	l.HolderInstanceID = uuid.Nil
	if err := l.Validate(); err == nil {
		t.Fatal("expected an error for a missing HolderInstanceID")
	}
}

func TestLeaseValidateRejectsExpiresAtNotAfterAcquiredAt(t *testing.T) {
	l := validLease()
	l.ExpiresAt = l.AcquiredAt
	if err := l.Validate(); err == nil {
		t.Fatal("expected an error when ExpiresAt does not come after AcquiredAt")
	}
}
