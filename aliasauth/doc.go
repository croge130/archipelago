// Package aliasauth is the Alias + Gatehouse-core integration named in
// 01-build-order.md's Layer 2 table: "who's allowed to create, resolve,
// or release a given alias entry — Alias itself resolves a name with no
// opinion on this." Alias's own evaluation.Resolve and facade.Ensure/
// ReleaseAlias stay exactly as built: pure structure and evaluation over
// storage, with zero authorization logic of their own. This package is
// what a caller reaches for the moment it needs one, composing Alias's
// operations with a Gatehouse-core permission check in front of each —
// never the other way around, and never folded into either base.
//
// The three operations get their own permission keys (PermissionCreate,
// PermissionResolve, PermissionRelease), scoped per-table rather than
// per-entry: a table ("docs.address_aliases", say) is the natural
// ownership boundary — many names live under one table, and that's the
// granularity an admin actually wants to grant at, not one grant per
// alias. ContextTypeTable + a table name is a Gatehouse-core Context
// like any other; a global grant for the same permission key still
// covers every table, same as everywhere else Context scoping is used.
//
// "alias" is already a reserved permission-key namespace in
// gatehouse-core/facade's own defaults, specifically so these three
// built-in keys can't be accidentally shadowed by an app registering its
// own "alias.*" key. RegisterPermissions claims that namespace on
// purpose via AllowReservedNamespace — call it once during setup, before
// any Create/Resolve/ReleaseAlias check can succeed; Gatehouse-core's own
// Evaluate denies unconditionally against a permission key nothing ever
// registered.
package aliasauth
