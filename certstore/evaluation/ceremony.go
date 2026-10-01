package evaluation

import (
	"fmt"
	"time"

	"github.com/croge130/archipelago/certstore/structure"
)

// Policy says which purposes may auto-approve without an operator
// decision — 05-pki-and-signing.md's "confirmation policy per purpose"
// knob. The real purpose-to-tier mapping belongs to the Policy base
// once it exists; until then a caller builds its own Policy value and
// passes it in, rather than certstore hardcoding a mapping it has no
// business owning.
type Policy struct {
	AutoApprove map[structure.EnrollmentPurpose]bool
}

// RequiresApproval reports whether purpose needs an operator decision
// before signing — true for any purpose not explicitly listed, so an
// unrecognized or newly introduced purpose defaults to the cautious
// path rather than silently auto-approving.
func (p Policy) RequiresApproval(purpose structure.EnrollmentPurpose) bool {
	return !p.AutoApprove[purpose]
}

// Confirm transitions a pending Enrollment to confirmed. confirmedBy is
// an operator identifier — certstore has no principal model of its own
// to attribute this to.
func Confirm(e structure.Enrollment, confirmedBy string, now time.Time) (structure.Enrollment, error) {
	if e.Status != structure.EnrollmentStatusPending {
		return structure.Enrollment{}, fmt.Errorf("evaluation: confirm: enrollment %s is %s, not pending", e.EnrollmentID, e.Status)
	}
	e.Status = structure.EnrollmentStatusConfirmed
	e.ConfirmedAt = &now
	e.ConfirmedBy = confirmedBy
	if err := e.Validate(); err != nil {
		return structure.Enrollment{}, fmt.Errorf("evaluation: confirm: %w", err)
	}
	return e, nil
}

// Reject transitions a pending Enrollment to rejected — the explicit
// "kill it now" path the doc calls for alongside expiry, so a
// suspicious pending request doesn't just sit until it times out.
func Reject(e structure.Enrollment, now time.Time) (structure.Enrollment, error) {
	if e.Status != structure.EnrollmentStatusPending {
		return structure.Enrollment{}, fmt.Errorf("evaluation: reject: enrollment %s is %s, not pending", e.EnrollmentID, e.Status)
	}
	e.Status = structure.EnrollmentStatusRejected
	e.RejectedAt = &now
	if err := e.Validate(); err != nil {
		return structure.Enrollment{}, fmt.Errorf("evaluation: reject: %w", err)
	}
	return e, nil
}

// Expire transitions a pending Enrollment that has passed its
// ExpiresAt to expired — a timeout, distinct from Reject's explicit
// operator decision.
func Expire(e structure.Enrollment, now time.Time) (structure.Enrollment, error) {
	if e.Status != structure.EnrollmentStatusPending {
		return structure.Enrollment{}, fmt.Errorf("evaluation: expire: enrollment %s is %s, not pending", e.EnrollmentID, e.Status)
	}
	if e.ExpiresAt == nil || !now.After(*e.ExpiresAt) {
		return structure.Enrollment{}, fmt.Errorf("evaluation: expire: enrollment %s has not passed its ExpiresAt", e.EnrollmentID)
	}
	e.Status = structure.EnrollmentStatusExpired
	if err := e.Validate(); err != nil {
		return structure.Enrollment{}, fmt.Errorf("evaluation: expire: %w", err)
	}
	return e, nil
}

// Revoke transitions an active Cert to revoked.
func Revoke(c structure.Cert, reason string, now time.Time) (structure.Cert, error) {
	if c.Status != structure.CertStatusActive {
		return structure.Cert{}, fmt.Errorf("evaluation: revoke: cert %s is %s, not active", c.SerialNumber, c.Status)
	}
	c.Status = structure.CertStatusRevoked
	c.RevokedAt = &now
	c.RevocationReason = reason
	if err := c.Validate(); err != nil {
		return structure.Cert{}, fmt.Errorf("evaluation: revoke: %w", err)
	}
	return c, nil
}
