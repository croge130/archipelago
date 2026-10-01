// Package structure holds Gatehouse-core's plain data types: Principal,
// Credential, Session, Context, Grant, Role/Group/Template, and
// PermissionDefinition. No logic beyond basic shape validation, no
// storage calls, no dependency on any other Archipelago base — this is
// the Structure layer of the structure/evaluation/storage split fixed
// in docs/architecture/02-package-boundaries.md.
//
// The field shapes here follow docs/architecture/09-gatehouse-core-model.md
// directly; that doc is the source of truth for *why* a field exists or
// doesn't, this package just gives it a Go type.
package structure
