// Package db is Archipelago's Layer 0 DB bedrock: a thin pgx connection
// pool wrapper plus a migration runner every base's storage layer
// depends on, and nothing else. It has no dependency on any other
// Archipelago base — logging included, matching the design docs'
// reasoning that logging failures must never loop back through
// whatever they were reporting on.
//
// Each base owns and registers its own Migrations under its own module
// name; this package only provides the shared mechanics (the pool, the
// schema_migrations tracking table, applying migrations in a
// transaction) so that mechanism exists exactly once rather than once
// per base.
//
// See docs/architecture/01-build-order.md and
// docs/architecture/08-repo-scaffolding.md in the project root.
package db
