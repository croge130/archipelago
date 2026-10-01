// Package dbstore is certstore's Storage layer: PostgresReader
// (satisfying facade.Reader) and PostgresWriter (satisfying
// facade.Writer) are deliberately separate types, same reasoning as
// Gatehouse-core's own dbstore — direct reads can stay direct while a
// future asymmetric-DB-access topology swaps in a different writer,
// without Evaluation, Facade, or any calling code noticing. See
// gatehouse-core/storage/dbstore's own doc comment for the full
// rationale; it applies here unchanged.
package dbstore
