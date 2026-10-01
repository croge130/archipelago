package facade

import (
	"context"
	"crypto/x509"
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
// signs immediately through ca and returns the issued Cert plus the
// real signed chain (leaf first); otherwise the enrollment is
// recorded pending and signedCert/chain are nil until an operator
// later calls ConfirmEnrollment.
//
// The chain is returned, never persisted: the same "stored hashed,
// returned raw exactly once at mint" convention AGENTS.md already
// states for token/ticket material, applied here because the only
// party that needs the certificate bytes long-term is whoever holds
// the matching private key — which was never certstore's to keep,
// since the caller submitted a CSR, not a key.
func SubmitEnrollment(ctx context.Context, w Writer, ca *evaluation.CA, policy evaluation.Policy, purpose structure.EnrollmentPurpose, subject string, csr []byte, deviceRef string, expiresAt *time.Time) (enrollment structure.Enrollment, signedCert *structure.Cert, chain []*x509.Certificate, err error) {
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
			return structure.Enrollment{}, nil, nil, fmt.Errorf("facade: submit enrollment: %w", err)
		}
	}

	if err := w.CreateEnrollment(ctx, e); err != nil {
		return structure.Enrollment{}, nil, nil, fmt.Errorf("facade: submit enrollment: %w", err)
	}

	if e.Status != structure.EnrollmentStatusConfirmed {
		return e, nil, nil, nil
	}

	cert, signedChain, err := signAndRecord(ctx, w, ca, policy, e, now)
	if err != nil {
		return structure.Enrollment{}, nil, nil, err
	}
	return e, &cert, signedChain, nil
}

// ConfirmEnrollment is the operator's half of the ceremony for an
// enrollment that required approval: it transitions the enrollment to
// confirmed and signs it through ca in the same call. See
// SubmitEnrollment's own doc comment for why chain is returned rather
// than persisted.
func ConfirmEnrollment(ctx context.Context, r Reader, w Writer, ca *evaluation.CA, policy evaluation.Policy, enrollmentID uuid.UUID, confirmedBy string) (enrollment structure.Enrollment, signedCert structure.Cert, chain []*x509.Certificate, err error) {
	e, found, err := r.GetEnrollment(ctx, enrollmentID)
	if err != nil {
		return structure.Enrollment{}, structure.Cert{}, nil, fmt.Errorf("facade: confirm enrollment: %w", err)
	}
	if !found {
		return structure.Enrollment{}, structure.Cert{}, nil, ErrNotFound
	}

	now := time.Now().Truncate(time.Microsecond)
	e, err = evaluation.Confirm(e, confirmedBy, now)
	if err != nil {
		return structure.Enrollment{}, structure.Cert{}, nil, fmt.Errorf("facade: confirm enrollment: %w", err)
	}
	if err := w.UpdateEnrollment(ctx, e); err != nil {
		return structure.Enrollment{}, structure.Cert{}, nil, fmt.Errorf("facade: confirm enrollment: %w", err)
	}

	cert, signedChain, err := signAndRecord(ctx, w, ca, policy, e, now)
	if err != nil {
		return structure.Enrollment{}, structure.Cert{}, nil, err
	}
	return e, cert, signedChain, nil
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
// into a Cert record, and persists it — but returns the real signed
// chain too, since the Cert record alone (serial/fingerprint/validity
// metadata) is useless to a caller that actually needs to present
// this certificate somewhere. Shared by the auto-approve path in
// SubmitEnrollment and the operator path in ConfirmEnrollment so the
// two can never diverge in how a confirmed enrollment becomes a cert.
func signAndRecord(ctx context.Context, w Writer, ca *evaluation.CA, policy evaluation.Policy, e structure.Enrollment, issuedAt time.Time) (structure.Cert, []*x509.Certificate, error) {
	csr, err := evaluation.ParseCSR(e.CSR)
	if err != nil {
		return structure.Cert{}, nil, fmt.Errorf("facade: sign enrollment: %w", err)
	}

	validity := policy.ValidityFor(e.Purpose)
	chain, err := ca.Sign(ctx, csr, issuedAt, issuedAt.Add(validity))
	if err != nil {
		return structure.Cert{}, nil, fmt.Errorf("facade: sign enrollment: %w", err)
	}
	if len(chain) == 0 {
		return structure.Cert{}, nil, fmt.Errorf("facade: sign enrollment: CA returned an empty chain")
	}

	cert := evaluation.NewCert(chain[0], e.EnrollmentID, e.Purpose, e.Subject, issuedAt)
	if err := w.CreateCert(ctx, cert); err != nil {
		return structure.Cert{}, nil, fmt.Errorf("facade: sign enrollment: %w", err)
	}
	return cert, chain, nil
}
