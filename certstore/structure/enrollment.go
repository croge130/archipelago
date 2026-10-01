package structure

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EnrollmentPurpose is deliberately open-ended, not a closed enum — the
// three named constants are what 05-pki-and-signing.md's enrollment
// ceremony names today (service mTLS peers, dedicated signing keys,
// admin/destructive-action keys), but the doc is explicit that purposes
// are an extensible vocabulary, the same shape as
// gatehouse-core's AuthenticationMethod. Which purposes are actually
// wired up, and what policy tier each gets, is an Evaluation-layer
// concern, not a fixed set checked here.
type EnrollmentPurpose string

const (
	EnrollmentPurposeMTLSPeer   EnrollmentPurpose = "mtls_peer"
	EnrollmentPurposeSigningKey EnrollmentPurpose = "signing_key"
	EnrollmentPurposeAdminKey   EnrollmentPurpose = "admin_key"
)

// EnrollmentStatus, unlike Purpose, is a closed, exhaustive set — the
// ceremony's own lifecycle, not an open vocabulary.
type EnrollmentStatus string

const (
	EnrollmentStatusPending   EnrollmentStatus = "pending"
	EnrollmentStatusConfirmed EnrollmentStatus = "confirmed"
	EnrollmentStatusRejected  EnrollmentStatus = "rejected"
	EnrollmentStatusExpired   EnrollmentStatus = "expired"
)

func (s EnrollmentStatus) Valid() bool {
	switch s {
	case EnrollmentStatusPending, EnrollmentStatusConfirmed, EnrollmentStatusRejected, EnrollmentStatusExpired:
		return true
	default:
		return false
	}
}

// Enrollment is the list-then-confirm ceremony's own record: a CSR
// submitted for a stated purpose, awaiting (or having received) a
// decision made through a genuinely separate channel — never gated by
// the same signed-request mechanism as an app-level dangerous action,
// per the doc's own reasoning (machine-level access to the backend
// already implies broader trust than a signature check would add).
//
// DeviceRef is correlation only, the same "devices are correlation
// context, not principals" rule gatehouse-core already settled —
// it's whatever the requester wants logged for the operator reviewing
// `list --pending` to see, not a reference to anything certstore
// itself models.
type Enrollment struct {
	EnrollmentID uuid.UUID
	Purpose      EnrollmentPurpose
	Subject      string // what the issued cert will claim to be
	CSR          []byte // the raw, submitted CSR (PEM or DER)
	DeviceRef    string
	Status       EnrollmentStatus
	CreatedAt    time.Time
	ExpiresAt    *time.Time
	ConfirmedAt  *time.Time
	ConfirmedBy  string // an operator identifier, never a gatehouse-core PrincipalID — certstore doesn't depend on that base
	RejectedAt   *time.Time
}

func (e Enrollment) Validate() error {
	if e.EnrollmentID == uuid.Nil {
		return fmt.Errorf("structure: enrollment: EnrollmentID is required")
	}
	if e.Purpose == "" {
		return fmt.Errorf("structure: enrollment: Purpose is required")
	}
	if e.Subject == "" {
		return fmt.Errorf("structure: enrollment: Subject is required")
	}
	if len(e.CSR) == 0 {
		return fmt.Errorf("structure: enrollment: CSR is required")
	}
	if !e.Status.Valid() {
		return fmt.Errorf("structure: enrollment: invalid Status %q", e.Status)
	}

	haveConfirmed := e.ConfirmedAt != nil
	haveRejected := e.RejectedAt != nil
	switch e.Status {
	case EnrollmentStatusPending, EnrollmentStatusExpired:
		if haveConfirmed || haveRejected {
			return fmt.Errorf("structure: enrollment: %s must not carry ConfirmedAt or RejectedAt", e.Status)
		}
	case EnrollmentStatusConfirmed:
		if !haveConfirmed || e.ConfirmedBy == "" {
			return fmt.Errorf("structure: enrollment: confirmed requires ConfirmedAt and ConfirmedBy")
		}
		if haveRejected {
			return fmt.Errorf("structure: enrollment: confirmed must not carry RejectedAt")
		}
	case EnrollmentStatusRejected:
		if !haveRejected {
			return fmt.Errorf("structure: enrollment: rejected requires RejectedAt")
		}
		if haveConfirmed {
			return fmt.Errorf("structure: enrollment: rejected must not carry ConfirmedAt")
		}
	}
	return nil
}

// IsDecided reports whether the ceremony has reached a terminal state
// — confirmed or rejected — as distinct from pending (awaiting a
// decision) or expired (timed out without one).
func (e Enrollment) IsDecided() bool {
	return e.Status == EnrollmentStatusConfirmed || e.Status == EnrollmentStatusRejected
}
