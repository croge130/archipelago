// Package certcred is the Gatehouse-core + Cert-store integration
// named in 01-build-order.md's Layer 2 table: "only if the credential
// model treats a certificate as a credential type — a real coupling
// to decide on purpose, not an accident." The decision made here:
// yes, but as a read-side check only, never a write-side sync.
//
// Gatehouse-core's own mtls_certificate credential (bound once via
// facade.EnsureMTLSCredential) records the *binding* — "this
// fingerprint speaks for this principal" — and its own Status is
// about that binding's lifecycle, never about the certificate's PKI
// lifecycle. Cert-store's own Cert record is the one true source for
// whether a certificate is currently usable at all (not revoked, not
// expired) — 05-pki-and-signing.md's own "DB check first" revocation
// signal, checked here fresh on every resolution rather than cached
// or mirrored into a second copy in Gatehouse-core. Peerauth's own,
// leaner resolution (Transit + Gatehouse-core only) doesn't make this
// second check; this package exists specifically for callers that
// want the stronger guarantee, coexisting with peerauth rather than
// replacing it — 02-package-boundaries.md is explicit that neither a
// convenience integration nor a stricter one is privileged over the
// other for the same pair of bases.
package certcred
