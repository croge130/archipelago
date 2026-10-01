package structure

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CertStatus is deliberately two-valued, not three — the same reasoning
// as gatehouse-core's Session: "expired" is a computed fact (see
// IsExpired below), checked against NotAfter at read time, never a
// stored status a background sweep has to keep current. Only an
// explicit action (issuance implies active; revocation changes it)
// gets a stored status value.
type CertStatus string

const (
	CertStatusActive  CertStatus = "active"
	CertStatusRevoked CertStatus = "revoked"
)

func (s CertStatus) Valid() bool {
	return s == CertStatusActive || s == CertStatusRevoked
}

// Cert is an issued certificate's record. Every Cert traces back to an
// Enrollment — even an auto-approved one still went through CSR
// submission and purpose/policy checks, the ceremony just completed
// without a human step. Purpose and Subject are denormalized from that
// Enrollment onto the Cert itself (the same copied-at-creation
// reasoning already used for status-rollup snapshots in
// 03-multi-instance-and-suites.md), so a lookup by fingerprint or
// serial never needs a join just to know what the cert is for.
type Cert struct {
	SerialNumber     string // X.509 serials are arbitrary-precision integers; stored as hex text, not a numeric type
	EnrollmentID     uuid.UUID
	Purpose          EnrollmentPurpose
	Subject          string
	Fingerprint      string // sha256, the correlation handle used elsewhere (e.g. gatehouse-core's MTLSCertCredDetail)
	NotBefore        time.Time
	NotAfter         time.Time
	Status           CertStatus
	RevokedAt        *time.Time
	RevocationReason string
	IssuedAt         time.Time
}

func (c Cert) Validate() error {
	if c.SerialNumber == "" {
		return fmt.Errorf("structure: cert: SerialNumber is required")
	}
	if c.EnrollmentID == uuid.Nil {
		return fmt.Errorf("structure: cert: EnrollmentID is required")
	}
	if c.Purpose == "" {
		return fmt.Errorf("structure: cert: Purpose is required")
	}
	if c.Subject == "" {
		return fmt.Errorf("structure: cert: Subject is required")
	}
	if c.Fingerprint == "" {
		return fmt.Errorf("structure: cert: Fingerprint is required")
	}
	if !c.NotAfter.After(c.NotBefore) {
		return fmt.Errorf("structure: cert: NotAfter must be after NotBefore")
	}
	if !c.Status.Valid() {
		return fmt.Errorf("structure: cert: invalid Status %q", c.Status)
	}
	if c.Status == CertStatusActive && c.RevokedAt != nil {
		return fmt.Errorf("structure: cert: an active cert must not carry RevokedAt")
	}
	if c.Status == CertStatusRevoked && c.RevokedAt == nil {
		return fmt.Errorf("structure: cert: a revoked cert must carry RevokedAt")
	}
	return nil
}

// IsExpired reports whether now is past NotAfter, independent of
// Status — a cert can be expired without ever having been revoked.
func (c Cert) IsExpired(now time.Time) bool {
	return now.After(c.NotAfter)
}

// IsUsable reports whether the cert is currently valid to rely on:
// active, not revoked, and not expired. The DB-backed revocation check
// this reflects stays primary over any CRL, per the doc's own
// reasoning — no propagation delay, no stale-cache window.
func (c Cert) IsUsable(now time.Time) bool {
	return c.Status == CertStatusActive && !c.IsExpired(now)
}
