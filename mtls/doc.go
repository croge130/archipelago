// Package mtls is the Transit + Cert-store integration named in
// 01-build-order.md's Layer 2 table: "real cert-backed PeerIdentity(),
// not a throwaway test cert." It is not "transit, with certstore
// baked in" — a separate package that imports both, per
// 02-package-boundaries.md's own rule, so a system that only wants
// raw peer identity never has to pull in certstore's dependency graph
// just because a convenience wrapper happened to live inside
// transit.
//
// What it adds: tls.Config construction from certstore-issued
// certificate material, and the wiring that gets a real,
// CA-verified peer identity all the way from a certstore enrollment
// through a TLS handshake into transit.PeerIdentity.
package mtls
