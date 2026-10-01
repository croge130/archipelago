// Package evaluation is Alias's Evaluation layer: Resolve, the one
// pure-read operation, against the Store interface it declares for
// itself — never a concrete storage implementation, same rule as
// Gatehouse-core's own evaluation package.
//
// Ensure and Release aren't here: they're read-then-write operations
// (idempotency and conflict checks before a mutation), which live in
// facade alongside the Writer interface, mirroring exactly why
// Gatehouse-core splits Evaluate (read-only) from the facade's
// EnsurePrincipal (read then write).
package evaluation
