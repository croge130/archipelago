package dbstore

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/certstore/structure"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWriter implements facade.Writer directly against Postgres.
// The Writer interface itself is declared in facade, its consumer, not
// here — same reasoning as gatehouse-core/storage/dbstore's own
// PostgresWriter.
type PostgresWriter struct {
	pool *pgxpool.Pool
}

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{pool: pool}
}

// CreateEnrollment inserts a new enrollment row — always pending at
// creation; the ceremony's later decisions go through
// UpdateEnrollment, never a second insert.
func (w *PostgresWriter) CreateEnrollment(ctx context.Context, e structure.Enrollment) error {
	if err := e.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO certstore_enrollments
		   (enrollment_id, purpose, subject, csr, device_ref, status, created_at, expires_at, confirmed_at, confirmed_by, rejected_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		e.EnrollmentID, string(e.Purpose), e.Subject, e.CSR, e.DeviceRef, string(e.Status),
		e.CreatedAt, e.ExpiresAt, e.ConfirmedAt, e.ConfirmedBy, e.RejectedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create enrollment: %w", err)
	}
	return nil
}

// UpdateEnrollment writes back an enrollment after a ceremony decision
// (Confirm/Reject/Expire) — every field that decision could have
// changed, since evaluation already decided the full new state.
func (w *PostgresWriter) UpdateEnrollment(ctx context.Context, e structure.Enrollment) error {
	if err := e.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`UPDATE certstore_enrollments SET
		   status = $2, confirmed_at = $3, confirmed_by = $4, rejected_at = $5
		 WHERE enrollment_id = $1`,
		e.EnrollmentID, string(e.Status), e.ConfirmedAt, e.ConfirmedBy, e.RejectedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: update enrollment: %w", err)
	}
	return nil
}

// CreateCert inserts a newly issued certificate's record.
func (w *PostgresWriter) CreateCert(ctx context.Context, c structure.Cert) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO certstore_certs
		   (serial_number, enrollment_id, purpose, subject, fingerprint, not_before, not_after, status, revoked_at, revocation_reason, issued_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		c.SerialNumber, c.EnrollmentID, string(c.Purpose), c.Subject, c.Fingerprint,
		c.NotBefore, c.NotAfter, string(c.Status), c.RevokedAt, c.RevocationReason, c.IssuedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create cert: %w", err)
	}
	return nil
}

// UpdateCert writes back a cert after Revoke.
func (w *PostgresWriter) UpdateCert(ctx context.Context, c structure.Cert) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`UPDATE certstore_certs SET status = $2, revoked_at = $3, revocation_reason = $4
		 WHERE serial_number = $1`,
		c.SerialNumber, string(c.Status), c.RevokedAt, c.RevocationReason,
	)
	if err != nil {
		return fmt.Errorf("dbstore: update cert: %w", err)
	}
	return nil
}
