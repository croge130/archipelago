// Package typedvalue describes what a value means: its dimension (a
// sparse map of base-dimension exponents, not Lighthouse's original
// []UnitTerm-only model, which had no canonical form and compared
// unreliably), the unit it's expressed in, and — for the cases
// dimensions provably can't separate (duration vs. timestamp, torque
// vs. energy) — a SemanticType naming the distinction by hand.
//
// It does not describe whether a value is required, nor enforce rules
// about which values are permitted beyond completing the type itself
// (an enum's allowed_values). Those are typeconstraints' job, a
// one-way dependency on this package — see
// docs/architecture/10-typedvalue-and-policy-model.md for the full
// model this ports.
package typedvalue
