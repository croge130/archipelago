package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// ErrConflict is returned by EnsureMTLSCredential when fingerprint is
// already bound to a different principal — "ensure" means idempotent
// create-if-absent, never silently rebinding an existing credential
// to someone else, the same rule EnsureAlias and
// policy/facade.EnsurePolicyDefinition already follow for their own
// targets.
var ErrConflict = errors.New("facade: credential exists for a different principal")

// EnsureMTLSCredential binds a verified mTLS certificate fingerprint
// to principalID, idempotently: a second call with the same
// fingerprint for the same principal is a no-op; for a different
// principal it's ErrConflict. No secret storage at all — fingerprint
// is a reference into certstore's own records, per the model doc.
func EnsureMTLSCredential(ctx context.Context, reader Reader, writer Writer, principalID uuid.UUID, fingerprint string) (structure.Credential, error) {
	existing, _, found, err := reader.GetCredentialByMTLSFingerprint(ctx, fingerprint)
	if err != nil {
		return structure.Credential{}, fmt.Errorf("facade: ensure mtls credential: %w", err)
	}
	if found {
		if existing.PrincipalID != principalID {
			return structure.Credential{}, ErrConflict
		}
		return existing, nil
	}

	now := time.Now().Truncate(time.Microsecond)
	cred := structure.Credential{
		CredentialID: uuid.New(),
		PrincipalID:  principalID,
		Kind:         structure.CredentialKindMTLSCertificate,
		Status:       structure.CredentialStatusActive,
		CreatedAt:    now,
	}
	detail := structure.MTLSCertCredDetail{
		CredentialID:    cred.CredentialID,
		CertFingerprint: fingerprint,
	}
	if err := writer.CreateMTLSCredential(ctx, cred, detail); err != nil {
		return structure.Credential{}, fmt.Errorf("facade: ensure mtls credential: %w", err)
	}
	return cred, nil
}
