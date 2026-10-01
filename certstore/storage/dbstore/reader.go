package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/certstore/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresReader implements facade.Reader directly against Postgres.
type PostgresReader struct {
	pool *pgxpool.Pool
}

func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

func (r *PostgresReader) GetEnrollment(ctx context.Context, id uuid.UUID) (structure.Enrollment, bool, error) {
	var e structure.Enrollment
	var purpose, status string
	err := r.pool.QueryRow(ctx,
		`SELECT enrollment_id, purpose, subject, csr, device_ref, status,
		        created_at, expires_at, confirmed_at, confirmed_by, rejected_at
		 FROM certstore_enrollments WHERE enrollment_id = $1`,
		id,
	).Scan(&e.EnrollmentID, &purpose, &e.Subject, &e.CSR, &e.DeviceRef, &status,
		&e.CreatedAt, &e.ExpiresAt, &e.ConfirmedAt, &e.ConfirmedBy, &e.RejectedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Enrollment{}, false, nil
		}
		return structure.Enrollment{}, false, fmt.Errorf("dbstore: get enrollment: %w", err)
	}
	e.Purpose = structure.EnrollmentPurpose(purpose)
	e.Status = structure.EnrollmentStatus(status)
	return e, true, nil
}

// ListPendingEnrollments returns every enrollment still awaiting a
// decision — the `list --pending` side of the ceremony's own doc.
func (r *PostgresReader) ListPendingEnrollments(ctx context.Context) ([]structure.Enrollment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT enrollment_id, purpose, subject, csr, device_ref, status,
		        created_at, expires_at, confirmed_at, confirmed_by, rejected_at
		 FROM certstore_enrollments WHERE status = $1 ORDER BY created_at`,
		string(structure.EnrollmentStatusPending),
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: list pending enrollments: %w", err)
	}
	defer rows.Close()

	var out []structure.Enrollment
	for rows.Next() {
		var e structure.Enrollment
		var purpose, status string
		if err := rows.Scan(&e.EnrollmentID, &purpose, &e.Subject, &e.CSR, &e.DeviceRef, &status,
			&e.CreatedAt, &e.ExpiresAt, &e.ConfirmedAt, &e.ConfirmedBy, &e.RejectedAt); err != nil {
			return nil, fmt.Errorf("dbstore: list pending enrollments: scan: %w", err)
		}
		e.Purpose = structure.EnrollmentPurpose(purpose)
		e.Status = structure.EnrollmentStatus(status)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dbstore: list pending enrollments: %w", err)
	}
	return out, nil
}

func (r *PostgresReader) GetCert(ctx context.Context, serialNumber string) (structure.Cert, bool, error) {
	var c structure.Cert
	var purpose, status string
	err := r.pool.QueryRow(ctx,
		`SELECT serial_number, enrollment_id, purpose, subject, fingerprint,
		        not_before, not_after, status, revoked_at, revocation_reason, issued_at
		 FROM certstore_certs WHERE serial_number = $1`,
		serialNumber,
	).Scan(&c.SerialNumber, &c.EnrollmentID, &purpose, &c.Subject, &c.Fingerprint,
		&c.NotBefore, &c.NotAfter, &status, &c.RevokedAt, &c.RevocationReason, &c.IssuedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Cert{}, false, nil
		}
		return structure.Cert{}, false, fmt.Errorf("dbstore: get cert: %w", err)
	}
	c.Purpose = structure.EnrollmentPurpose(purpose)
	c.Status = structure.CertStatus(status)
	return c, true, nil
}

func (r *PostgresReader) GetCertByFingerprint(ctx context.Context, fingerprint string) (structure.Cert, bool, error) {
	var c structure.Cert
	var purpose, status string
	err := r.pool.QueryRow(ctx,
		`SELECT serial_number, enrollment_id, purpose, subject, fingerprint,
		        not_before, not_after, status, revoked_at, revocation_reason, issued_at
		 FROM certstore_certs WHERE fingerprint = $1`,
		fingerprint,
	).Scan(&c.SerialNumber, &c.EnrollmentID, &purpose, &c.Subject, &c.Fingerprint,
		&c.NotBefore, &c.NotAfter, &status, &c.RevokedAt, &c.RevocationReason, &c.IssuedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Cert{}, false, nil
		}
		return structure.Cert{}, false, fmt.Errorf("dbstore: get cert by fingerprint: %w", err)
	}
	c.Purpose = structure.EnrollmentPurpose(purpose)
	c.Status = structure.CertStatus(status)
	return c, true, nil
}
