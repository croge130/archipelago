package facade

import (
	"context"

	"github.com/croge130/archipelago/certstore/structure"
	"github.com/google/uuid"
)

// Reader is everything the facade needs to read.
type Reader interface {
	GetEnrollment(ctx context.Context, id uuid.UUID) (structure.Enrollment, bool, error)
	ListPendingEnrollments(ctx context.Context) ([]structure.Enrollment, error)
	GetCert(ctx context.Context, serialNumber string) (structure.Cert, bool, error)
	GetCertByFingerprint(ctx context.Context, fingerprint string) (structure.Cert, bool, error)
}

// Writer is what the facade needs to persist — raw creates/updates
// with no idempotency or transition logic of its own; that logic is
// Evaluation's (Confirm/Reject/Expire/Revoke) and this package's own
// (Submit/Confirm/Reject/Revoke below), which decide the desired row
// state before calling these.
type Writer interface {
	CreateEnrollment(ctx context.Context, e structure.Enrollment) error
	UpdateEnrollment(ctx context.Context, e structure.Enrollment) error
	CreateCert(ctx context.Context, c structure.Cert) error
	UpdateCert(ctx context.Context, c structure.Cert) error
}
