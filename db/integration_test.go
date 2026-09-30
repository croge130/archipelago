// Tests in this file need a real PostgreSQL instance. They skip, rather
// than fail, when ARCHIPELAGO_TEST_DATABASE_URL isn't set — the same
// DB-backed-test convention Lighthouse uses.
package db

import (
	"context"
	"os"
	"testing"
)

func openTestPool(t *testing.T) *Pool {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := Open(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestOpenAndPing(t *testing.T) {
	pool := openTestPool(t)
	if pool.Pgx() == nil {
		t.Fatal("Pgx() returned nil after a successful Open")
	}
}

func TestOpenRejectsBadDSN(t *testing.T) {
	if _, err := Open(context.Background(), Config{DSN: "not a dsn"}); err == nil {
		t.Fatal("expected an error opening an invalid DSN")
	}
}

func TestProvisionSchemasAppliesAndIsIdempotent(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	migrations := []Migration{
		{
			Module:  "test_db_pkg",
			Version: 1,
			Name:    "create widgets",
			SQL:     `CREATE TABLE IF NOT EXISTS test_db_pkg_widgets (id serial PRIMARY KEY, name text NOT NULL)`,
		},
	}
	t.Cleanup(func() {
		pool.Pgx().Exec(ctx, `DROP TABLE IF EXISTS test_db_pkg_widgets`)
		pool.Pgx().Exec(ctx, `DELETE FROM schema_migrations WHERE module = 'test_db_pkg'`)
	})

	if err := pool.ProvisionSchemas(ctx, migrations); err != nil {
		t.Fatalf("first ProvisionSchemas: %v", err)
	}

	// Running again must be a no-op, not a "table already exists" error
	// — this is the whole point of tracking what's applied.
	if err := pool.ProvisionSchemas(ctx, migrations); err != nil {
		t.Fatalf("second ProvisionSchemas (should be a no-op): %v", err)
	}

	if _, err := pool.Pgx().Exec(ctx, `INSERT INTO test_db_pkg_widgets (name) VALUES ('ok')`); err != nil {
		t.Fatalf("migrated table isn't usable: %v", err)
	}

	applied, err := pool.isApplied(ctx, "test_db_pkg", 1)
	if err != nil {
		t.Fatalf("isApplied: %v", err)
	}
	if !applied {
		t.Error("expected the migration to be recorded as applied")
	}
}

func TestProvisionSchemasRejectsDuplicateBeforeTouchingDB(t *testing.T) {
	pool := openTestPool(t)
	err := pool.ProvisionSchemas(context.Background(), []Migration{
		{Module: "test_db_pkg", Version: 1, Name: "a", SQL: "SELECT 1"},
		{Module: "test_db_pkg", Version: 1, Name: "b", SQL: "SELECT 1"},
	})
	if err == nil {
		t.Fatal("expected a validation error for duplicate (module, version)")
	}
}
