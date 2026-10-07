// Package sdkdb is the DB-backed provider of sdk.Stores: the one storage
// implementation that exists today. It is a separate module from sdk on
// purpose (docs/architecture/17-sdk-model.md) — sdk itself must stay
// usable by a node with no direct database access, or only read-only,
// scoped or conditional access, so nothing in sdk may depend on this
// module. Such a node builds its sdk.Stores some other way, or builds
// them from OpenDB's result and narrows them (drop a Writer, drop a
// base); sdk.New and App.Seed are written to accept exactly that.
package sdkdb
