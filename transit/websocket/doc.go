// Package websocket is Transit's WebSocket backend — the first real
// (not in-memory) implementation of transit.Session/Channel/Backend,
// built on github.com/coder/websocket (the actively maintained
// continuation of nhooyr.io/websocket, which Lighthouse's own design
// doc names but which now carries a deprecation notice pointing here
// — verified via go doc before depending on either, per the project's
// standing "verify the real library before building against it"
// discipline).
//
// One writer goroutine per connection serializes all outbound frames
// (Conn.Write is safe to call concurrently per the library's own
// docs, but every Send still funnels through one goroutine so a
// caller's per-call context never reaches the wire write directly —
// github.com/coder/websocket closes the whole connection on any
// Write error, context expiration included, so a short per-call
// timeout must bound only "how long to wait to enqueue," never the
// actual wire write). One reader goroutine demuxes incoming frames by
// Kind: stream_open/stream_data/stream_end route to the matching
// Channel; everything else lands in a generic inbox, read via Next —
// the same Router-shaped gap transit/inmem documents, for the same
// reason (the Router itself is out of scope for this pass).
package websocket
