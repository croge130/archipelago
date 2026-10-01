// Package dbstore is Gatehouse-core's Storage layer: the first (and,
// for now, only) concrete implementation of what Structure's types
// need persisted and what Evaluation's Store interface needs read.
//
// Reads and writes are deliberately two separate types — PostgresReader
// (satisfying evaluation.Store) and PostgresWriter (satisfying Writer,
// defined in this package) — rather than one combined type, so that a
// later asymmetric-DB-access topology
// (docs/architecture/03-multi-instance-and-suites.md) can keep direct
// reads while swapping in a Writer that routes through a designated
// writer node instead. Nothing about Evaluation or any calling code
// needs to change for that swap; only which Writer gets constructed
// does.
package dbstore
