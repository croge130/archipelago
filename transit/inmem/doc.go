// Package inmem is a loopback implementation of transit.Session and
// transit.Channel over in-process Go channels — no sockets. It exists
// to prove the interfaces are actually implementable and testable
// before the real WebSocket backend adds socket/handshake concerns on
// top, and to give anything built on transit.Session a fast, real
// (not mocked) pair of peers to test against.
//
// It deliberately skips the Router/dispatch layer a real backend
// sits behind — Next and AcceptChannel hand a caller exactly what a
// Router would otherwise dispatch to a handler. That layer is out of
// scope for this pass (see docs/architecture/11-transit-model.md's
// deferred list), so this package exposes the raw delivery directly
// rather than simulating one.
package inmem
