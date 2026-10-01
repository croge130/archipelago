// Package facade is the ergonomic composition layer that ties
// Evaluation and Storage together — the thing neither should do
// internally. Evaluation only ever depends on its own Store interface;
// Storage only ever implements interfaces declared by whoever consumes
// it. Composing both into "ensure this principal exists," "register
// this permission," "grant this" is a genuinely separate concern from
// either, which is why it's its own package rather than bolted onto
// one of them.
//
// Reader and Writer are declared here, not re-exported from
// storage/dbstore — the same reasoning as evaluation.Store's own
// placement: the interface lives with its consumer, so a future
// alternative implementation (a remote-forwarding Writer for an
// asymmetric topology, per docs/architecture/03-multi-instance-and-
// suites.md) only has to satisfy this package's interfaces, never
// import dbstore just to reference a type.
//
// Per docs/architecture/04-facades-and-ergonomics.md: this is a thin
// wrapper over the same real Evaluate/Require and the same real
// Writer, never a second implementation that usually agrees with them.
package facade
