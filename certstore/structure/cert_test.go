package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func activeCert(now time.Time) Cert {
	return Cert{
		SerialNumber: "0f1e2d3c4b5a",
		EnrollmentID: uuid.New(),
		Purpose:      EnrollmentPurposeMTLSPeer,
		Subject:      "service:gamebridge",
		Fingerprint:  "sha256:abcd",
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		Status:       CertStatusActive,
		IssuedAt:     now.Add(-time.Hour),
	}
}

func TestCertValidateOK(t *testing.T) {
	now := time.Now()
	if err := activeCert(now).Validate(); err != nil {
		t.Fatalf("expected a valid cert to validate, got: %v", err)
	}
}

func TestCertValidateRejectsNotAfterBeforeNotBefore(t *testing.T) {
	now := time.Now()
	c := activeCert(now)
	c.NotAfter = c.NotBefore.Add(-time.Minute)
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for NotAfter before NotBefore")
	}
}

func TestCertValidateActiveRejectsRevokedAt(t *testing.T) {
	now := time.Now()
	c := activeCert(now)
	c.RevokedAt = &now
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for an active cert carrying RevokedAt")
	}
}

func TestCertValidateRevokedRequiresRevokedAt(t *testing.T) {
	now := time.Now()
	c := activeCert(now)
	c.Status = CertStatusRevoked
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for revoked without RevokedAt")
	}
	c.RevokedAt = &now
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a properly revoked cert to validate, got: %v", err)
	}
}

func TestCertIsExpired(t *testing.T) {
	now := time.Now()
	c := activeCert(now)
	if c.IsExpired(now) {
		t.Error("a freshly issued cert should not be expired")
	}
	if !c.IsExpired(now.Add(48 * time.Hour)) {
		t.Error("a cert should be expired well past its NotAfter")
	}
}

func TestCertIsUsable(t *testing.T) {
	now := time.Now()
	c := activeCert(now)
	if !c.IsUsable(now) {
		t.Error("an active, unexpired cert should be usable")
	}
	if c.IsUsable(now.Add(48 * time.Hour)) {
		t.Error("an expired cert should not be usable even if never revoked")
	}

	c.Status = CertStatusRevoked
	c.RevokedAt = &now
	if c.IsUsable(now) {
		t.Error("a revoked cert should not be usable even before NotAfter")
	}
}
