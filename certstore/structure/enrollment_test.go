package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func pendingEnrollment() Enrollment {
	return Enrollment{
		EnrollmentID: uuid.New(),
		Purpose:      EnrollmentPurposeMTLSPeer,
		Subject:      "service:gamebridge",
		CSR:          []byte("-----BEGIN CERTIFICATE REQUEST-----..."),
		Status:       EnrollmentStatusPending,
		CreatedAt:    time.Now(),
	}
}

func TestEnrollmentValidatePendingOK(t *testing.T) {
	if err := pendingEnrollment().Validate(); err != nil {
		t.Fatalf("expected a valid pending enrollment to validate, got: %v", err)
	}
}

func TestEnrollmentValidateRejectsMissingCSR(t *testing.T) {
	e := pendingEnrollment()
	e.CSR = nil
	if err := e.Validate(); err == nil {
		t.Fatal("expected an error for a missing CSR")
	}
}

func TestEnrollmentValidateRejectsMissingSubject(t *testing.T) {
	e := pendingEnrollment()
	e.Subject = ""
	if err := e.Validate(); err == nil {
		t.Fatal("expected an error for a missing Subject")
	}
}

func TestEnrollmentValidateConfirmedRequiresConfirmedByAndAt(t *testing.T) {
	e := pendingEnrollment()
	e.Status = EnrollmentStatusConfirmed
	if err := e.Validate(); err == nil {
		t.Fatal("expected an error for confirmed without ConfirmedAt/ConfirmedBy")
	}
	now := time.Now()
	e.ConfirmedAt = &now
	e.ConfirmedBy = "operator:christian"
	if err := e.Validate(); err != nil {
		t.Fatalf("expected a properly confirmed enrollment to validate, got: %v", err)
	}
}

func TestEnrollmentValidateRejectedRequiresRejectedAt(t *testing.T) {
	e := pendingEnrollment()
	e.Status = EnrollmentStatusRejected
	if err := e.Validate(); err == nil {
		t.Fatal("expected an error for rejected without RejectedAt")
	}
	now := time.Now()
	e.RejectedAt = &now
	if err := e.Validate(); err != nil {
		t.Fatalf("expected a properly rejected enrollment to validate, got: %v", err)
	}
}

func TestEnrollmentValidatePendingRejectsConfirmedAt(t *testing.T) {
	e := pendingEnrollment()
	now := time.Now()
	e.ConfirmedAt = &now
	if err := e.Validate(); err == nil {
		t.Fatal("expected an error for a pending enrollment carrying ConfirmedAt")
	}
}

func TestEnrollmentIsDecided(t *testing.T) {
	e := pendingEnrollment()
	if e.IsDecided() {
		t.Error("a pending enrollment should not report as decided")
	}
	e.Status = EnrollmentStatusExpired
	if e.IsDecided() {
		t.Error("an expired enrollment should not report as decided — it timed out, nobody decided")
	}
	e.Status = EnrollmentStatusConfirmed
	if !e.IsDecided() {
		t.Error("a confirmed enrollment should report as decided")
	}
}

func TestEnrollmentPurposeIsOpenEnded(t *testing.T) {
	// Deliberately no Valid() method on EnrollmentPurpose to test
	// against — an arbitrary purpose string must be constructible and
	// pass Enrollment.Validate, since restricting the vocabulary is an
	// Evaluation-layer policy concern, not a Structure-layer one.
	e := pendingEnrollment()
	e.Purpose = EnrollmentPurpose("some_future_app_defined_purpose")
	if err := e.Validate(); err != nil {
		t.Fatalf("expected an arbitrary EnrollmentPurpose to be accepted at the structure layer, got: %v", err)
	}
}
