// Package dbstore is Policy's Storage layer: PostgresReader
// (satisfying evaluation.Store plus facade's own extra reads) and
// PostgresWriter are deliberately separate types, same reasoning as
// every other base's dbstore — direct reads can stay direct while a
// future asymmetric-DB-access topology swaps in a different writer,
// without Evaluation, Facade, or any calling code noticing.
package dbstore
