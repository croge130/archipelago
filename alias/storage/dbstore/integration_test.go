// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL, same convention as every
// other module's integration tests.
package dbstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/croge130/archipelago/alias/structure"
	archidb "github.com/croge130/archipelago/db"
)

func setupTestStore(t *testing.T) (*PostgresReader, *PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping dbstore integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
	}

	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE alias_aliases`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return NewPostgresReader(pool.Pgx()), NewPostgresWriter(pool.Pgx())
}

func TestDBStoreUpsertAndGetAlias(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now()

	a := structure.Alias{Table: "documents", Name: "readme", Target: "doc-42", Lifecycle: structure.LifecycleActive, CreatedAt: now, UpdatedAt: now}
	if err := writer.UpsertAlias(ctx, a); err != nil {
		t.Fatalf("UpsertAlias: %v", err)
	}

	got, found, err := reader.GetAlias(ctx, "documents", "readme")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if !found || got.Target != "doc-42" || got.Lifecycle != structure.LifecycleActive {
		t.Fatalf("GetAlias = %+v, found=%v, want target doc-42 active", got, found)
	}
}

func TestDBStoreGetAliasNotFound(t *testing.T) {
	reader, _ := setupTestStore(t)
	_, found, err := reader.GetAlias(context.Background(), "documents", "nonexistent")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if found {
		t.Fatal("expected not found for a never-created alias")
	}
}

func TestDBStoreUpsertAliasOverwritesOnConflict(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := writer.UpsertAlias(ctx, structure.Alias{
		Table: "documents", Name: "readme", Target: "doc-42", Lifecycle: structure.LifecycleActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertAlias (first): %v", err)
	}

	released := now.Add(time.Minute)
	if err := writer.UpsertAlias(ctx, structure.Alias{
		Table: "documents", Name: "readme", Target: "doc-42", Lifecycle: structure.LifecycleReleased, CreatedAt: now, UpdatedAt: released, ReleasedAt: &released,
	}); err != nil {
		t.Fatalf("UpsertAlias (release): %v", err)
	}

	got, found, err := reader.GetAlias(ctx, "documents", "readme")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	if !found || got.Lifecycle != structure.LifecycleReleased || got.ReleasedAt == nil {
		t.Fatalf("GetAlias = %+v, want released with ReleasedAt set", got)
	}
}
