// Package typeconstraints holds rules about values within an
// already-complete type — min/max, length, pattern — as opposed to
// typedvalue, which describes what a value means. The dividing line
// (from docs/architecture/10-typedvalue-and-policy-model.md, itself
// ported from the Lighthouse typedvalue generalization design doc):
// a constraint that *completes* a type stays in typedvalue
// (allowed_values, since an enum without its member set isn't a
// complete description of what the value is); a constraint that
// *restricts* values within an already-complete type lives here.
//
// merge_mode also lives here rather than in typedvalue or Policy
// itself, because checking it — minimum/maximum require an ordered
// type, boolean_and/boolean_or require bool, object_merge requires a
// JSON-shaped value — needs both a type and a rule, exactly this
// package's job, and because the merge operation itself (combining
// several applicable values into one) is the same type-dependent work
// as any other constraint check.
package typeconstraints
