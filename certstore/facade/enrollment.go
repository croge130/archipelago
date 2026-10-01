package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/certstore/evaluation"
	"github.com/croge130/archipelago/certstore/structure"
	"github.com/google/uuid"
)

// ErrNotFound is returned when an operation names an enrollment or
// cert that was never created.
var ErrNotFound = errors.New("facade: not found")

// autoApprovedBy is the ConfirmedBy sentinel for an enrollment that
// skipped the operator decision under policy — still a real,
// attributable decision in the record, per 05-pki-and-signing.md's
// "even an auto-approved one still went through CSR submission and
// purpose/policy checks" framing, not a blank confirmer.
const autoApprovedBy = "system:auto-approved"

// SubmitEnrollment records a CSR submission under the ceremony's
// list-then-confirm shape. If policy auto-approves purpose, this also
// signs immediately through ca and returns the issued Cert; otherwise
// the enrollment is recorded pending and signedCert is nil until an
// operator later calls ConfirmEnrollment.
func SubmitEnrollment(ctx context.Context, w Writer, ca *evaluation.CA, policy evaluation.Policy, purpose structure.EnrollmentPurpose, subject string, csr []byte, deviceRef string, expiresAt *time.Time) (enrollment structure.Enrollment, signedCert *structure.Cert, err error) {
	now := time.Now().Truncate(time.Microsecond)
	e := structure.Enrollment{
		EnrollmentID: uuid.New(),
		Purpose:      purpose,
		Subject:      subject,
		CSR:          csr,
		DeviceRef:    deviceRef,
		Status:       structure.EnrollmentStatusPending,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
	}

	if !policy.RequiresApproval(purpose) {
		e, err = evaluation.Confirm(e, autoApprovedBy, now)
		if err != nil {
			return structure.Enrollment{}, nil, fmt.Errorf("facade: submit enrollment: %w", err)
		}
	}

	if err := w.CreateEnrollment(ctx, e); err != nil {
		return structure.Enrollment{}, nil, fmt.Errorf("facade: submit enrollment: %w", err)
	}

	if e.Status != structure.EnrollmentStatusConfirmed {
		return e, nil, nil
	}

	cert, err := signAndRecord(ctx, w, ca, policy, e, now)
	if err != nil {
		return structure.Enrollment{}, nil, err
	}
	return e, &cert, nil
}

// ConfirmEnrollment is the operator's half of the ceremony for an
// enrollment that required approval: it transitions the enrollment to
// confirmed and signs it through ca in the same call.
func ConfirmEnrollment(ctx context.Context, r Reader, w Writer, ca *evaluation.CA, policy evaluation.Policy, enrollmentID uuid.UUID, confirmedBy string) (structure.Enrollment, structure.Cert, error) {
	e, found, err := r.GetEnrollment(ctx, enrollmentID)
	if err != nil {
		return structure.Enrollment{}, structure.Cert{}, fmt.Errorf("facade: confirm enrollment: %w", err)
	}
	if !found {
		return structure.Enrollment{}, structure.Cert{}, ErrNotFound
	}

	now := time.Now().Truncate(time.Microsecond)
	e, err = evaluation.Confirm(e, confirmedBy, now)
	if err != nil {
		return structure.Enrollment{}, structure.Cert{}, fmt.Errorf("facade: confirm enrollment: %w", err)
	}
	if err := w.UpdateEnrollment(ctx, e); err != nil {
		return structure.Enrollment{}, structure.Cert{}, fmt.Errorf("facade: confirm enrollment: %w", err)
	}

	cert, err := signAndRecord(ctx, w, ca, policy, e, now)
	if err != nil {
		return structure.Enrollment{}, structure.Cert{}, err
	}
	return e, cert, nil
}

// RejectEnrollment is the operator's explicit "kill it now" path,
// alongside expiry — 05-pki-and-signing.md calls for this so a
// suspicious pending request doesn't just sit until it times out.
func RejectEnrollment(ctx context.Context, r Reader, w Writer, enrollmentID uuid.UUID) (structure.Enrollment, error) {
	e, found, err := r.GetEnrollment(ctx, enrollmentID)
	if err != nil {
		return structure.Enrollment{}, fmt.Errorf("facade: reject enrollment: %w", err)
	}
	if !found {
		return structure.Enrollment{}, ErrNotFound
	}

	e, err = evaluation.Reject(e, time.Now().Truncate(time.Microsecond))
	if err != nil {
		return structure.Enrollment{}, fmt.Errorf("facade: reject enrollment: %w", err)
	}
	if err := w.UpdateEnrollment(ctx, e); err != nil {
		return structure.Enrollment{}, fmt.Errorf("facade: reject enrollment: %w", err)
	}
	return e, nil
}

// signAndRecord parses a confirmed enrollment's CSR, signs it through
// ca for the duration policy assigns its purpose, converts the result
// into a Cert record, and persists it. Shared by the auto-approve path
// in SubmitEnrollment and the operator path in ConfirmEnrollment so the
// two can never diverge in how a confirmed enrollment becomes a cert.
func signAndRecord(ctx context.Context, w Writer, ca *evaluation.CA, policy evaluation.Policy, e structure.Enrollment, issuedAt time.Time) (structure.Cert, error) {
	csr, err := evaluation.ParseCSR(e.CSR)
	if err != nil {
		return structure.Cert{}, fmt.Errorf("facade: sign enrollment: %w", err)
	}

	validity := policy.ValidityFor(e.Purpose)
	chain, err := ca.Sign(ctx, csr, issuedAt, issuedAt.Add(validity))
	if err != nil {
		return structure.Cert{}, fmt.Errorf("facade: sign enrollment: %w", err)
	}
	if len(chain) == 0 {
		return structure.Cert{}, fmt.Errorf("facade: sign enrollment: CA returned an empty chain")
	}

	cert := evaluation.NewCert(chain[0], e.EnrollmentID, e.Purpose, e.Subject, issuedAt)
	if err := w.CreateCert(ctx, cert); err != nil {
		return structure.Cert{}, fmt.Errorf("facade: sign enrollment: %w", err)
	}
	return cert, nil
}
