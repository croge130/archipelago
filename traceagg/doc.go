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
// It does not own how a message arrives, either. Collector.Handler is
// a router.Handler to register under MessageTypeEntries, and
// PushEntries sends through anything shaped like a *router.Peer; the
// handshake, authorization (via routerauth, with whatever permission a
// coordinator requires of reporters) and in-flight limiting all belong
// to the router in front of it. Collector.Ingest remains for a caller
// that already holds a received message.
package traceagg
