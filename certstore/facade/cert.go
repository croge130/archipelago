package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/certstore/evaluation"
	"github.com/croge130/archipelago/certstore/structure"
)

// RevokeCert transitions an active cert to revoked. The DB-enrollment
// check this reflects stays the primary revocation signal, per
// 05-pki-and-signing.md — no propagation delay, no stale-cache window,
// unlike a CRL.
func RevokeCert(ctx context.Context, r Reader, w Writer, serialNumber, reason string) (structure.Cert, error) {
	c, found, err := r.GetCert(ctx, serialNumber)
	if err != nil {
		return structure.Cert{}, fmt.Errorf("facade: revoke cert: %w", err)
	}
	if !found {
		return structure.Cert{}, ErrNotFound
	}

	c, err = evaluation.Revoke(c, reason, time.Now().Truncate(time.Microsecond))
	if err != nil {
		return structure.Cert{}, fmt.Errorf("facade: revoke cert: %w", err)
	}
	if err := w.UpdateCert(ctx, c); err != nil {
		return structure.Cert{}, fmt.Errorf("facade: revoke cert: %w", err)
	}
	return c, nil
}
