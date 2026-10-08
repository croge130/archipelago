// Package router is the layer above transit.Backend.Accept that
// docs/architecture/11-transit-model.md deliberately left undesigned and
// docs/architecture/21-router-and-handshake-model.md now specifies.
//
// A Router is a registry of routes (message type -> handler) shared by
// every session it serves. For each session it runs a Peer: the receive
// loop that demultiplexes responses to waiting calls, dispatches requests
// and events to routes, accepts typed channels, enforces the handshake,
// and lets either end call the other.
//
// What it guarantees:
//
//   - A handler never runs on the read loop, so one stuck handler cannot
//     stop a session from reading a cancel frame or a heartbeat.
//   - The route is the ordering domain for non-channel messages: by
//     default a route handles one message at a time per session, in
//     arrival order, and opts into concurrency per route. A slow route
//     blocks only itself.
//   - Nothing is reachable before the handshake except the handshake.
//   - Failures cross the wire as coded errors from a small closed set; a
//     handler's internal error text is logged on the handling side and
//     never sent.
//   - Each handled message runs under a child span of the sender's trace
//     context, so a handler's own logging joins the sender's trace.
//
// What it deliberately does not know: principals, permissions, databases
// or domains. A route may carry an Authorizer; the routerauth integration
// builds one from Gatehouse-core. One Router serves one domain's listener,
// so it needs no domain concept.
package router
