// Package evaluation holds Policy's one resolution chokepoint —
// Resolve — plus the Store interface it reads through. Resolve takes
// a set of Refs, not one: it doesn't know what a principal, group,
// role, or context is, and doesn't need to. The caller gathers every
// Ref that applies to the current evaluation (using whatever
// expansion logic it already has — Gatehouse-core's own role/group
// expansion, for instance) and Resolve treats every member of that
// set identically. See
// docs/architecture/10-typedvalue-and-policy-model.md for the full
// model.
package evaluation
