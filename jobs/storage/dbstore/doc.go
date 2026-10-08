// Package dbstore is the jobs base's Storage layer: PostgresReader and
// PostgresWriter, kept as separate types for the same asymmetric-DB-
// access-topology reason every other base documents.
//
// The Writer is deliberately made of coarse commands — enqueue, claim,
// heartbeat, complete, fail, cancel, reap, prune — each one atomic,
// because that is what lets a remote implementation of the same
// interface exist (02-package-boundaries.md): a thin network call per
// command is feasible, a fine-grained SQL surface would not be.
//
// Every state change is a single SQL statement whose WHERE clause
// carries the guard (state, and the attempt number as a claim token), so
// the database — not a read-then-write in Go — decides whether it
// applied. Every timestamp comes from the database's own clock.
package dbstore
