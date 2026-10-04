// Package vitalsauth is the Vitals + Gatehouse-core integration named
// in docs/architecture/14-vitals-model.md: who's allowed to read or
// write a reading, or manage a definition or group. Vitals' own
// facade has no opinion on this at all — Instance.ScopeType/ScopeID
// and Group.ScopeType/ScopeID are bare strings Vitals never
// interprets.
//
// This package is what gives that pair meaning: it's used directly as
// a Gatehouse-core Context (ScopeType as the context type, ScopeID as
// the context ID), the exact same "a bare pair IS a Context" reuse
// aliasauth's own ContextTypeTable already established for Alias's
// Table field. No new context-scoping idea, no vitalsauth-owned
// context type constant — the scope pair a caller already chose when
// creating the Instance/Group is the only context this package ever
// checks against.
//
// Definition management is checked globally, not context-scoped —
// Definition carries no scope field at all (ownership of a
// DefinitionKey is the reserved-namespace convention, per the model
// doc), so there's no (ScopeType, ScopeID) pair to scope the check to.
package vitalsauth
