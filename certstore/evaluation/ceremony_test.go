package evaluation

import (
	"testing"
	"time"

	"github.com/croge130/archipelago/certstore/structure"
	"github.com/google/uuid"
)

func pendingEnrollment() structure.Enrollment {
	return structure.Enrollment{
		EnrollmentID: uuid.New(),
		Purpose:      structure.EnrollmentPurposeMTLSPeer,
		Subject:      "service:gamebridge",
		CSR:          []byte("-----BEGIN CERTIFICATE REQUEST-----..."),
		Status:       structure.EnrollmentStatusPending,
		CreatedAt:    time.Now(),
	}
}

func TestConfirm(t *testing.T) {
	now := time.Now()
	e, err := Confirm(pendingEnrollment(), "operator:christian", now)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if e.Status != structure.EnrollmentStatusConfirmed || e.ConfirmedBy != "operator:christian" {
		t.Errorf("unexpected result: %+v", e)
	}
}

func TestConfirmRejectsNonPending(t *testing.T) {
	e := pendingEnrollment()
	e.Status = structure.EnrollmentStatusRejected
	e.RejectedAt = &e.CreatedAt
	if _, err := Confirm(e, "operator:christian", time.Now()); err == nil {
		t.Fatal("expected an error confirming an already-rejected enrollment")
	}
}

func TestReject(t *testing.T) {
	now := time.Now()
	e, err := Reject(pendingEnrollment(), now)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if e.Status != structure.EnrollmentStatusRejected {
		t.Errorf("unexpected status: %v", e.Status)
	}
}

func TestExpireRequiresPassedExpiresAt(t *testing.T) {
	e := pendingEnrollment()
	soon := e.CreatedAt.Add(time.Minute)
	e.ExpiresAt = &soon

	if _, err := Expire(e, e.CreatedAt); err == nil {
		t.Fatal("expected an error expiring before ExpiresAt has passed")
	}

	expired, err := Expire(e, soon.Add(time.Second))
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if expired.Status != structure.EnrollmentStatusExpired {
		t.Errorf("unexpected status: %v", expired.Status)
	}
}

func TestExpireRejectsMissingExpiresAt(t *testing.T) {
	if _, err := Expire(pendingEnrollment(), time.Now()); err == nil {
		t.Fatal("expected an error expiring an enrollment with no ExpiresAt")
	}
}

func activeCertForRevoke(now time.Time) structure.Cert {
	return structure.Cert{
		SerialNumber: "0f1e2d3c4b5a",
		EnrollmentID: uuid.New(),
		Purpose:      structure.EnrollmentPurposeMTLSPeer,
		Subject:      "service:gamebridge",
		Fingerprint:  "sha256:abcd",
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		Status:       structure.CertStatusActive,
		IssuedAt:     now.Add(-time.Hour),
	}
}

func TestRevoke(t *testing.T) {
	now := time.Now()
	revoked, err := Revoke(activeCertForRevoke(now), "key compromise suspected", now)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked.Status != structure.CertStatusRevoked || revoked.RevocationReason == "" {
		t.Errorf("unexpected result: %+v", revoked)
	}
}

func TestRevokeRejectsAlreadyRevoked(t *testing.T) {
	now := time.Now()
	c := activeCertForRevoke(now)
	c.Status = structure.CertStatusRevoked
	c.RevokedAt = &now
	if _, err := Revoke(c, "again", now); err == nil {
		t.Fatal("expected an error revoking an already-revoked cert")
	}
}

func TestPolicyRequiresApproval(t *testing.T) {
	p := Policy{AutoApprove: map[structure.EnrollmentPurpose]bool{
		structure.EnrollmentPurposeMTLSPeer: true,
	}}
	if p.RequiresApproval(structure.EnrollmentPurposeMTLSPeer) {
		t.Error("expected mtls_peer to be auto-approved per policy")
	}
	if !p.RequiresApproval(structure.EnrollmentPurposeAdminKey) {
		t.Error("expected admin_key, not listed in AutoApprove, to require approval")
	}
	if !p.RequiresApproval(structure.EnrollmentPurpose("some_future_purpose")) {
		t.Error("expected an unrecognized purpose to default to requiring approval")
	}
}
