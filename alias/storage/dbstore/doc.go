// Package dbstore is Alias's Storage layer: PostgresReader (satisfying
// evaluation.Store) and PostgresWriter are deliberately separate
// types, same reasoning as Gatehouse-core's own dbstore — direct reads
// can stay direct while a future asymmetric-DB-access topology swaps
// in a different writer, without Evaluation or any calling code
// noticing. See gatehouse-core/storage/dbstore's own doc comment for
// the full rationale; it applies here unchanged.
package dbstore
