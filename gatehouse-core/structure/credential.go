package structure

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CredentialKind is what's presented on the wire to authenticate a
// request, as distinct from the authentication method that first
// established it (password/TOTP/passkey are methods; session_token is
// what password or passkey login actually mints). See the model doc's
// Credential section for the full method-vs-credential reasoning.
//
// Storage is one table per kind, not grouped by shape — see the model
// doc's "Storage: one table per credential kind" section for why a
// password and a token credential don't share a table despite both
// being "hash and compare" under the hood.
type CredentialKind string

const (
	CredentialKindPassword        CredentialKind = "password"
	CredentialKindTOTP            CredentialKind = "totp"
	CredentialKindPasskey         CredentialKind = "passkey"
	CredentialKindSessionToken    CredentialKind = "session_token"
	CredentialKindMTLSCertificate CredentialKind = "mtls_certificate"
)

func (k CredentialKind) Valid() bool {
	switch k {
	case CredentialKindPassword, CredentialKindTOTP, CredentialKindPasskey,
		CredentialKindSessionToken, CredentialKindMTLSCertificate:
		return true
	default:
		return false
	}
}

type CredentialStatus string

const (
	CredentialStatusActive  CredentialStatus = "active"
	CredentialStatusRevoked CredentialStatus = "revoked"
)

func (s CredentialStatus) Valid() bool {
	return s == CredentialStatusActive || s == CredentialStatusRevoked
}

// Credential is the common record every kind shares: identity, which
// principal it belongs to, and lifecycle. Kind-specific secret/public
// material lives in the matching detail type below, never here.
type Credential struct {
	CredentialID uuid.UUID
	PrincipalID  uuid.UUID
	Kind         CredentialKind
	Status       CredentialStatus
	CreatedAt    time.Time
	RevokedAt    *time.Time
}

func (c Credential) Validate() error {
	if c.CredentialID == uuid.Nil {
		return fmt.Errorf("structure: credential: CredentialID is required")
	}
	if c.PrincipalID == uuid.Nil {
		return fmt.Errorf("structure: credential: PrincipalID is required")
	}
	if !c.Kind.Valid() {
		return fmt.Errorf("structure: credential: invalid Kind %q", c.Kind)
	}
	if !c.Status.Valid() {
		return fmt.Errorf("structure: credential: invalid Status %q", c.Status)
	}
	return nil
}

// PasswordHashParams carries a memory-hard algorithm's cost parameters.
// Kept as its own type because these are what makes password storage
// genuinely different from token storage — a token has no cost
// parameters to tune at all.
type PasswordHashParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// PasswordCredDetail carries exactly the fields the model doc's
// storage-shape section identified as password-specific: a memory-hard
// hash, its cost parameters, lockout tracking, and rehash-on-success
// bookkeeping — none of which a token credential has any use for.
type PasswordCredDetail struct {
	CredentialID   uuid.UUID
	HashAlgorithm  string // e.g. "argon2id"
	Hash           []byte
	Params         PasswordHashParams
	FailedAttempts int
	LockedUntil    *time.Time
	LastRehashedAt *time.Time
}

func (d PasswordCredDetail) Validate() error {
	if d.CredentialID == uuid.Nil {
		return fmt.Errorf("structure: password credential detail: CredentialID is required")
	}
	if d.HashAlgorithm == "" {
		return fmt.Errorf("structure: password credential detail: HashAlgorithm is required")
	}
	if len(d.Hash) == 0 {
		return fmt.Errorf("structure: password credential detail: Hash is required")
	}
	return nil
}

// TokenCredDetail: a fast hash of an already-high-entropy value.
// No cost parameters, never rehashed — deliberately the opposite shape
// from PasswordCredDetail, not a variation on it.
type TokenCredDetail struct {
	CredentialID uuid.UUID
	Hash         []byte
	Purpose      string
	Scope        string
	ExpiresAt    *time.Time
}

func (d TokenCredDetail) Validate() error {
	if d.CredentialID == uuid.Nil {
		return fmt.Errorf("structure: token credential detail: CredentialID is required")
	}
	if len(d.Hash) == 0 {
		return fmt.Errorf("structure: token credential detail: Hash is required")
	}
	if d.Purpose == "" {
		return fmt.Errorf("structure: token credential detail: Purpose is required")
	}
	return nil
}

// TOTPCredDetail: encrypted, reversible — the one credential
// shape that must be decryptable, since the code has to compute the
// current code from the secret rather than just comparing hashes.
type TOTPCredDetail struct {
	CredentialID    uuid.UUID
	EncryptedSecret []byte
	KeyVersion      int
}

func (d TOTPCredDetail) Validate() error {
	if d.CredentialID == uuid.Nil {
		return fmt.Errorf("structure: totp credential detail: CredentialID is required")
	}
	if len(d.EncryptedSecret) == 0 {
		return fmt.Errorf("structure: totp credential detail: EncryptedSecret is required")
	}
	if d.KeyVersion <= 0 {
		return fmt.Errorf("structure: totp credential detail: KeyVersion must be positive")
	}
	return nil
}

// PasskeyCredDetail: public key material only. Not a secret at
// all — never hashed, never encrypted, same as Lighthouse's own
// passkey_credentials shape.
type PasskeyCredDetail struct {
	CredentialID uuid.UUID
	PublicKey    []byte
	SignCount    uint32
	Transports   []string
	BackedUp     bool
}

func (d PasskeyCredDetail) Validate() error {
	if d.CredentialID == uuid.Nil {
		return fmt.Errorf("structure: passkey credential detail: CredentialID is required")
	}
	if len(d.PublicKey) == 0 {
		return fmt.Errorf("structure: passkey credential detail: PublicKey is required")
	}
	return nil
}

// MTLSCertCredDetail: no secret storage in gatehouse-core
// at all, per the model doc. CertFingerprint is a reference into
// certstore's own records, never a copy of the certificate or key.
type MTLSCertCredDetail struct {
	CredentialID    uuid.UUID
	CertFingerprint string
}

func (d MTLSCertCredDetail) Validate() error {
	if d.CredentialID == uuid.Nil {
		return fmt.Errorf("structure: mtls certificate credential detail: CredentialID is required")
	}
	if d.CertFingerprint == "" {
		return fmt.Errorf("structure: mtls certificate credential detail: CertFingerprint is required")
	}
	return nil
}
