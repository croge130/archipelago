// Package sdk is the storage-agnostic composition root described in
// docs/architecture/17-sdk-model.md: it takes already-built stores, checks
// them against the modes a caller enabled, and exposes them as an App —
// and that is all. It deliberately does not re-wrap any base or
// integration function (registry/doc.go made the same decision for the
// same reason), and it never touches a database: this package has no
// db/pgx dependency at all, so a node with no direct database access, or
// only read-only, scoped or conditional access, can use it unchanged.
// The DB-backed way of producing Stores lives in the separate sdkdb
// module for exactly that reason.
//
// Two rules the whole package is built around:
//
//   - New performs no storage I/O. It validates, nothing else, so a node
//     whose store access is conditional can still construct an App and
//     find out about availability when it actually uses a store.
//   - Every Reader and every Writer, for every base, is independently
//     optional. "Read-only" is "this base's Writer is nil", not a global
//     flag, and Seed skips (and reports) any step whose Writer is absent
//     rather than failing the whole startup.
package sdk
