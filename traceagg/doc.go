// Package traceagg is the Transit + registry integration named in
// docs/architecture/16-trace-log-aggregation-model.md: shipping a
// reporting instance's captured logging.Entry records to a peer, and
// collecting them into a trace-ID-indexed store a coordinator/Viewer
// can query to stitch together one causal story across instances.
//
// This package does not import registry. Finding which peer to push
// to — ListPeers, ListInstancesByGroup — is the caller's own job, the
// same "already built, just pointed at a new audience" reasoning
// 13-registry-and-leases-model.md's Broadcast section already applied
// to Status aggregation: traceagg only owns what to send and what to
// do with what arrives, never how a Session was found or opened.
//
// It also does not own how a message arrives at all. Collector.Ingest
// takes an already-received wire.Message, the same way sso.Verify
// takes an already-received Ticket — no Router/dispatch layer exists
// in this codebase yet (11-transit-model.md's own deferred list), and
// this package doesn't build one just for its own convenience.
package traceagg
