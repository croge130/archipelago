// Package facade composes Evaluation and Storage into Ensure/Release —
// the idempotent, read-then-write operations that are genuinely
// different from Resolve's pure read. Writer is declared here, its
// consumer, not in storage/dbstore — the same rule as Gatehouse-core's
// facade.Writer.
package facade
