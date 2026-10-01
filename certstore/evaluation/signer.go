package evaluation

import (
	"crypto"
	"crypto/x509"
	"io"
)

// Signer is the swappable signing-hardware abstraction 05-pki-and-signing.md
// calls for: a software key today, a PKCS#11-backed HSM, a TPM-sealed
// key, or a YubiKey/PIV key later, without the CA that uses it needing
// to change. It's exactly what smallstep's authority.WithX509Signer
// needs — a crypto.Signer plus the signer's own certificate — named at
// certstore's own boundary rather than importing smallstep's types into
// callers that only want to provide key material.
type Signer interface {
	crypto.Signer
	Certificate() *x509.Certificate
}

// softwareSigner is today's only Signer: an in-process private key.
// HSM/TPM/YubiKey implementations are future Signer values behind this
// same interface, per the doc — acknowledged, not built.
type softwareSigner struct {
	cert *x509.Certificate
	key  crypto.Signer
}

// NewSoftwareSigner wraps an in-process certificate and private key as
// a Signer.
func NewSoftwareSigner(cert *x509.Certificate, key crypto.Signer) Signer {
	return softwareSigner{cert: cert, key: key}
}

func (s softwareSigner) Certificate() *x509.Certificate { return s.cert }
func (s softwareSigner) Public() crypto.PublicKey       { return s.key.Public() }
func (s softwareSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.key.Sign(rand, digest, opts)
}
