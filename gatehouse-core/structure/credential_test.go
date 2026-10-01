package structure

import (
	"testing"

	"github.com/google/uuid"
)

func validCredential(kind CredentialKind) Credential {
	return Credential{
		CredentialID: uuid.New(),
		PrincipalID:  uuid.New(),
		Kind:         kind,
		Status:       CredentialStatusActive,
	}
}

func TestCredentialValidateOK(t *testing.T) {
	if err := validCredential(CredentialKindPassword).Validate(); err != nil {
		t.Fatalf("expected a valid credential to validate, got: %v", err)
	}
}

func TestCredentialValidateRejectsInvalidKind(t *testing.T) {
	c := validCredential(CredentialKind("carrier_pigeon"))
	if err := c.Validate(); err == nil {
		t.Fatal("expected an invalid Kind to be rejected")
	}
}

func TestCredentialValidateRejectsInvalidStatus(t *testing.T) {
	c := validCredential(CredentialKindPassword)
	c.Status = CredentialStatus("pending")
	if err := c.Validate(); err == nil {
		t.Fatal("expected an invalid Status to be rejected")
	}
}

func TestPasswordCredentialDetailValidate(t *testing.T) {
	d := PasswordCredentialDetail{
		CredentialID:  uuid.New(),
		HashAlgorithm: "argon2id",
		Hash:          []byte("not-a-real-hash"),
		Params:        PasswordHashParams{MemoryKiB: 65536, Iterations: 3, Parallelism: 4},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid password detail to validate, got: %v", err)
	}
	d.Hash = nil
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a missing Hash")
	}
}

func TestTokenCredentialDetailValidate(t *testing.T) {
	d := TokenCredentialDetail{
		CredentialID: uuid.New(),
		Hash:         []byte("fast-hash"),
		Purpose:      "session",
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid token detail to validate, got: %v", err)
	}
	d.Purpose = ""
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a missing Purpose")
	}
}

func TestTOTPCredentialDetailValidate(t *testing.T) {
	d := TOTPCredentialDetail{
		CredentialID:    uuid.New(),
		EncryptedSecret: []byte("ciphertext"),
		KeyVersion:      1,
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid totp detail to validate, got: %v", err)
	}
	d.KeyVersion = 0
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a non-positive KeyVersion")
	}
}

func TestPasskeyCredentialDetailValidate(t *testing.T) {
	d := PasskeyCredentialDetail{
		CredentialID: uuid.New(),
		PublicKey:    []byte("public-key-bytes"),
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid passkey detail to validate, got: %v", err)
	}
	d.PublicKey = nil
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a missing PublicKey")
	}
}

func TestMTLSCertificateCredentialDetailValidate(t *testing.T) {
	d := MTLSCertificateCredentialDetail{
		CredentialID:    uuid.New(),
		CertFingerprint: "sha256:abcd",
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid cert detail to validate, got: %v", err)
	}
	d.CertFingerprint = ""
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a missing CertFingerprint — no secret material should ever be required or stored here")
	}
}
