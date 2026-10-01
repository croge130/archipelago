// Package peerauth is the Transit + Gatehouse-core integration named
// in 01-build-order.md's Layer 2 table: "does this verified peer's
// identity resolve to a principal with this grant." A separate
// package that imports both, per 02-package-boundaries.md's rule —
// never folded into either base, so a system that wants raw peer
// identity with its own authorization logic never has to pull in
// Gatehouse-core's dependency graph just because a convenience
// wrapper happened to live inside transit.
//
// Resolution is read-only and never creates anything: mapping a
// verified mTLS fingerprint to a principal happens only through a
// credential gatehouse-core/facade.EnsureMTLSCredential already
// bound, matching the model doc's no-implicit-principal-creation
// default. The narrow mTLS auto-provisioning exception that doc
// allows is a deliberate policy decision a caller makes explicitly
// (by calling EnsureMTLSCredential itself, on its own trigger), not
// something this package does on a peer's first connection.
package peerauth
