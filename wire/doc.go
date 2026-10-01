// Package wire is the shared envelope model for Transit — Message,
// the Kind and DeliveryClass vocabularies, initiator-scoped channel-
// id helpers, and Encode/Decode. No sockets, no QUIC: anything that
// needs to speak the envelope (an SDK, a future app) depends on this
// alone, never on the goroutine/ctx/backpressure machinery transit
// itself needs. See docs/architecture/11-transit-model.md for the
// full model this ports from Lighthouse's own transport layer design.
package wire
