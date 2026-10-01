package evaluation

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/croge130/archipelago/certstore/structure"
	"github.com/google/uuid"
	"github.com/smallstep/certificates/authority"
	"github.com/smallstep/certificates/authority/provisioner"
)

// ParseCSR parses an Enrollment's stored CSR bytes, accepting either
// PEM (what a CLI/API submission typically carries) or raw DER.
func ParseCSR(raw []byte) (*x509.CertificateRequest, error) {
	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		der = block.Bytes
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, fmt.Errorf("evaluation: parse csr: %w", err)
	}
	return csr, nil
}

// CA is the embedded certificate authority: smallstep/certificates'
// provisioner-based CSR/signing logic running as a library inside our
// own process, per 05-pki-and-signing.md — no separate service to
// network-secure. Its own signing key is a Signer, so swapping in HSM-
// or TPM-backed hardware later means constructing CA with a different
// Signer, not changing anything here.
type CA struct {
	authority *authority.Authority
}

// NewCA constructs the embedded authority from a root CA bundle (PEM)
// and the Signer that holds the issuing intermediate's key.
func NewCA(rootPEM []byte, signer Signer) (*CA, error) {
	a, err := authority.NewEmbedded(
		authority.WithX509RootBundle(rootPEM),
		authority.WithX509Signer(signer.Certificate(), signer),
	)
	if err != nil {
		return nil, fmt.Errorf("evaluation: new embedded authority: %w", err)
	}
	return &CA{authority: a}, nil
}

// validityWindow is a provisioner.CertificateModifier. It exists
// because provisioner.SignOptions.NotBefore/NotAfter are not
// self-applying — step-ca only honors them through a
// CertificateModifier it constructs internally (profileDefaultDuration,
// unexported), so callers outside the module have to supply their own
// to control the issued certificate's validity window at all. Verified
// empirically against smallstep/certificates v0.30.2 before relying on
// it here.
type validityWindow struct {
	notBefore time.Time
	notAfter  time.Time
}

func (v validityWindow) Modify(cert *x509.Certificate, _ provisioner.SignOptions) error {
	cert.NotBefore = v.notBefore
	cert.NotAfter = v.notAfter
	return nil
}

// Sign issues a certificate for csr, valid over exactly [notBefore,
// notAfter] — the caller (facade, informed by the enrollment's purpose
// and policy) decides that window; CA has no validity-duration
// policy of its own. The full chain is returned leaf-first.
func (ca *CA) Sign(ctx context.Context, csr *x509.CertificateRequest, notBefore, notAfter time.Time) ([]*x509.Certificate, error) {
	chain, err := ca.authority.SignWithContext(ctx, csr, provisioner.SignOptions{}, validityWindow{notBefore: notBefore, notAfter: notAfter})
	if err != nil {
		return nil, fmt.Errorf("evaluation: sign: %w", err)
	}
	return chain, nil
}

// NewCert converts a signed leaf certificate into the structure.Cert
// record it represents — the "copied-at-creation" denormalization of
// Purpose/Subject from the Enrollment it traces back to, same as
// Cert's own doc comment describes. issuedAt is passed in rather than
// read from the clock here, so callers stay in control of the single
// timestamp used across a signing operation's own records.
func NewCert(leaf *x509.Certificate, enrollmentID uuid.UUID, purpose structure.EnrollmentPurpose, subject string, issuedAt time.Time) structure.Cert {
	sum := sha256.Sum256(leaf.Raw)
	return structure.Cert{
		SerialNumber: leaf.SerialNumber.Text(16),
		EnrollmentID: enrollmentID,
		Purpose:      purpose,
		Subject:      subject,
		Fingerprint:  "sha256:" + hex.EncodeToString(sum[:]),
		NotBefore:    leaf.NotBefore,
		NotAfter:     leaf.NotAfter,
		Status:       structure.CertStatusActive,
		IssuedAt:     issuedAt,
	}
}
