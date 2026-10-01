// Package structure is Policy's Structure layer: PolicyDefinition,
// PolicyInstance, PolicyContext, and the Ref primitive, per
// docs/architecture/10-typedvalue-and-policy-model.md. Policy depends
// on typedvalue (what a value means) and typeconstraints (what rules
// apply to it) but on no other base — not Gatehouse-core, not any
// identity model — so Ref is deliberately opaque: Policy stores and
// compares (RefKind, RefKey) pairs, never interprets them.
package structure
