// Package evaluation is Gatehouse-core's Evaluation layer: the logic
// that operates over structure's types. It depends on structure for
// its types and on the Store interface defined here for reading them —
// never on a concrete storage implementation directly, per the
// structure/evaluation/storage split fixed in
// docs/architecture/02-package-boundaries.md. A local-DB Store
// implementation (gatehouse-core/storage/dbstore) and, later, a remote-
// backed one are both just implementations of the same interface; this
// package never notices which one it's talking to.
//
// Evaluate/Require (evaluate.go) is the one chokepoint every privileged
// operation goes through, per docs/architecture/09-gatehouse-core-model.md's
// Evaluation facade section. Permission semantics — deny-always-wins,
// wildcard matching, authority-level checking, role/group expansion —
// live here and nowhere else.
package evaluation
