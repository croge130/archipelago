// Package logging is Archipelago's Layer 0 logging bedrock: no
// dependencies, not even on the db module, and used directly by every
// other base and integration for its own observability.
//
// It builds on the standard library's log/slog rather than inventing a
// parallel Attr/Record/Handler system — the same "adopt the standard
// instead of rolling our own" reasoning already applied to tracing
// vocabulary (OpenTelemetry) and wire format (W3C Trace Context)
// elsewhere in this project. slog already gives structured attributes,
// a pluggable Handler interface, and arbitrary custom levels; this
// package adds exactly the three things slog doesn't: the nine-level
// severity scale carried forward from the design docs, the Resource
// concept (what produced a log line, as opposed to which operation it
// belongs to), and trace/span correlation in the W3C Trace Context
// shape.
//
// See docs/architecture/06-logging-and-observability.md in the
// project root for the design rationale.
package logging
