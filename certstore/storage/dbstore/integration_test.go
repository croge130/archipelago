// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL, same convention as every
// other module's integration tests.
package dbstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/croge130/archipelago/certstore/structure"
	archidb "github.com/croge130/archipelago/db"
	"github.com/google/uuid"
)

func setupTestStore(t *testing.T) (*PostgresReader, *PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping dbstore integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
	}

	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE certstore_certs, certstore_enrollments`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return NewPostgresReader(pool.Pgx()), NewPostgresWriter(pool.Pgx())
}

func pendingEnrollment(now time.Time) structure.Enrollment {
	return structure.Enrollment{
		EnrollmentID: uuid.New(),
		Purpose:      structure.EnrollmentPurposeMTLSPeer,
		Subject:      "service:gamebridge",
		CSR:          []byte("-----BEGIN CERTIFICATE REQUEST-----..."),
		Status:       structure.EnrollmentStatusPending,
		CreatedAt:    now,
	}
}

func TestDBStoreCreateAndGetEnrollment(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	e := pendingEnrollment(now)
	if err := writer.CreateEnrollment(ctx, e); err != nil {
		t.Fatalf("CreateEnrollment: %v", err)
	}

	got, found, err := reader.GetEnrollment(ctx, e.EnrollmentID)
	if err != nil {
		t.Fatalf("GetEnrollment: %v", err)
	}
	if !found || got.Status != structure.EnrollmentStatusPending || got.Subject != e.Subject {
		t.Fatalf("GetEnrollment = %+v, found=%v", got, found)
	}
}

func TestDBStoreGetEnrollmentNotFound(t *testing.T) {
	reader, _ := setupTestStore(t)
	_, found, err := reader.GetEnrollment(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetEnrollment: %v", err)
	}
	if found {
		t.Fatal("expected not found for a never-created enrollment")
	}
}

func TestDBStoreUpdateEnrollmentConfirms(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	e := pendingEnrollment(now)
	if err := writer.CreateEnrollment(ctx, e); err != nil {
		t.Fatalf("CreateEnrollment: %v", err)
	}

	e.Status = structure.EnrollmentStatusConfirmed
	e.ConfirmedAt = &now
	e.ConfirmedBy = "operator:christian"
	if err := writer.UpdateEnrollment(ctx, e); err != nil {
		t.Fatalf("UpdateEnrollment: %v", err)
	}

	got, found, err := reader.GetEnrollment(ctx, e.EnrollmentID)
	if err != nil {
		t.Fatalf("GetEnrollment: %v", err)
	}
	if !found || got.Status != structure.EnrollmentStatusConfirmed || got.ConfirmedBy != "operator:christian" {
		t.Fatalf("GetEnrollment after confirm = %+v, found=%v", got, found)
	}
}

func TestDBStoreListPendingEnrollments(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	pending := pendingEnrollment(now)
	if err := writer.CreateEnrollment(ctx, pending); err != nil {
		t.Fatalf("CreateEnrollment (pending): %v", err)
	}

	confirmed := pendingEnrollment(now)
	if err := writer.CreateEnrollment(ctx, confirmed); err != nil {
		t.Fatalf("CreateEnrollment (to confirm): %v", err)
	}
	confirmed.Status = structure.EnrollmentStatusConfirmed
	confirmed.ConfirmedAt = &now
	confirmed.ConfirmedBy = "operator:christian"
	if err := writer.UpdateEnrollment(ctx, confirmed); err != nil {
		t.Fatalf("UpdateEnrollment: %v", err)
	}

	list, err := reader.ListPendingEnrollments(ctx)
	if err != nil {
		t.Fatalf("ListPendingEnrollments: %v", err)
	}
	if len(list) != 1 || list[0].EnrollmentID != pending.EnrollmentID {
		t.Fatalf("ListPendingEnrollments = %+v, want exactly the still-pending enrollment", list)
	}
}

func activeCert(enrollmentID uuid.UUID, now time.Time) structure.Cert {
	return structure.Cert{
		SerialNumber: "0f1e2d3c4b5a",
		EnrollmentID: enrollmentID,
		Purpose:      structure.EnrollmentPurposeMTLSPeer,
		Subject:      "service:gamebridge",
		Fingerprint:  "sha256:abcd",
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		Status:       structure.CertStatusActive,
		IssuedAt:     now,
	}
}

func TestDBStoreCreateAndGetCert(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	e := pendingEnrollment(now)
	if err := writer.CreateEnrollment(ctx, e); err != nil {
		t.Fatalf("CreateEnrollment: %v", err)
	}

	c := activeCert(e.EnrollmentID, now)
	if err := writer.CreateCert(ctx, c); err != nil {
		t.Fatalf("CreateCert: %v", err)
	}

	got, found, err := reader.GetCert(ctx, c.SerialNumber)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if !found || got.Status != structure.CertStatusActive || got.Fingerprint != c.Fingerprint {
		t.Fatalf("GetCert = %+v, found=%v", got, found)
	}

	byFingerprint, found, err := reader.GetCertByFingerprint(ctx, c.Fingerprint)
	if err != nil {
		t.Fatalf("GetCertByFingerprint: %v", err)
	}
	if !found || byFingerprint.SerialNumber != c.SerialNumber {
		t.Fatalf("GetCertByFingerprint = %+v, found=%v", byFingerprint, found)
	}
}

func TestDBStoreUpdateCertRevokes(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	e := pendingEnrollment(now)
	if err := writer.CreateEnrollment(ctx, e); err != nil {
		t.Fatalf("CreateEnrollment: %v", err)
	}
	c := activeCert(e.EnrollmentID, now)
	if err := writer.CreateCert(ctx, c); err != nil {
		t.Fatalf("CreateCert: %v", err)
	}

	c.Status = structure.CertStatusRevoked
	c.RevokedAt = &now
	c.RevocationReason = "key compromise suspected"
	if err := writer.UpdateCert(ctx, c); err != nil {
		t.Fatalf("UpdateCert: %v", err)
	}

	got, found, err := reader.GetCert(ctx, c.SerialNumber)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if !found || got.Status != structure.CertStatusRevoked || got.RevocationReason == "" {
		t.Fatalf("GetCert after revoke = %+v, found=%v", got, found)
	}
}
