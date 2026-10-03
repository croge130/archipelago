// Package registry is the Transit + peerauth + Gatehouse-core slice of
// "Multi-instance coordination" named in 01-build-order.md. See
// docs/architecture/13-registry-and-leases-model.md for the full
// design: Instance and Lease live in Gatehouse-core itself (same split
// Session already uses), and this package provides exactly the one
// thing neither Gatehouse-core nor peerauth already provides alone:
// registering an Instance from an authenticated Transit connection,
// resolving the connecting principal via peerauth.ResolvePrincipal
// first — an instance's identity in the registry is only ever as
// trustworthy as the mTLS-verified connection that registered it, never
// a caller-asserted PrincipalID with no transport-level backing.
//
// Heartbeat, Deregister, ListPeers, and the lease operations need
// neither Transit nor peerauth — they're already complete as
// gatehouse-core/facade functions (Heartbeat, Deregister, ListPeers,
// AcquireOrRenewLease, ReleaseLease) and are used directly from there,
// not re-wrapped here. Adding pass-through wrappers with no behavior of
// their own would be exactly the unnecessary indirection this design
// avoids elsewhere.
package registry
