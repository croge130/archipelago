// Package sessions is the Gatehouse-core + Transit integration named
// in 01-build-order.md's Layer 2 table: "AuthoritySession tied to a
// principal, optionally to a live connection." A standalone package
// importing both, never folded into either — Gatehouse-core's own
// Session record deliberately carries no connection reference (see
// structure.Session's own doc comment), so the live-connection half
// of "optionally" lives entirely here.
//
// This package doesn't know how a peer was authenticated — that's
// peerauth's and mtls's job — it only ties an already-resolved
// principal's Session to the lifetime of an already-connected
// transit.Session: when the connection ends, the gatehouse-core
// Session is revoked. A Session created this way is otherwise an
// ordinary Session everywhere else in Gatehouse-core; nothing about
// resolution or evaluation needs to know it came from a live
// connection.
package sessions
