// Package structure holds Alias's one plain data type. Alias is
// deliberately the simplest base in the whole system: a table+name
// pair resolving to an opaque target value, with its own lifecycle —
// never load-bearing, no realm or any other mandatory dimension
// assumed, per docs/architecture/01-build-order.md's reasoning for why
// Alias is a Layer 1 base from day one rather than retrofitted the way
// Lighthouse's own alias system was.
package structure
