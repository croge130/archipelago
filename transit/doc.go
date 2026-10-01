// Package transit holds the behavior Transit's wire envelope rides
// on: Session, Channel, and Backend — interfaces only, no protocol
// import. Nothing above this package may import a concrete backend
// (transit/websocket, later transit/webtransport) directly; every
// caller depends on these interfaces, which is what makes a second
// backend additive instead of a rewrite. See
// docs/architecture/11-transit-model.md for the full model.
package transit
