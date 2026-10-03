// Package sso is the Layer 3 compound feature named in 01-build-order.md:
// SSO ticket issuance and verification, combining Gatehouse-core
// (principal existence), Cert-store (the dedicated ticket-signing key —
// never the mTLS key) and Transit (delivery, used but not owned here).
// See docs/architecture/12-sso-tickets-model.md for the full design:
// field shape, why there's no Issuer/payload field, the ECDSA P-256
// scope decision, and what's deliberately left out (replay protection
// beyond short expiry, revocation, multi-key support).
//
// Issue and Verify stop at identity resolution on purpose — a verified
// ticket's resolved principal still goes through Gatehouse-core's own
// evaluation.RequirePermission/RequireContextPermission exactly like
// any other resolved identity in this design (peerauth.Require is the
// closest existing shape); folding a permission check into Verify
// itself would turn "identity-only" into a name, not an invariant.
package sso
