// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. They exercise the facade
// through dbstore's real PostgresReader/PostgresWriter, proving
// facade.Reader and facade.Writer are actually satisfied by the
// concrete implementation.
package facade

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/croge130/archipelago/alias/evaluation"
	"github.com/croge130/archipelago/alias/storage/dbstore"
	archidb "github.com/croge130/archipelago/db"
)

func setupFacadeTest(t *testing.T) (Reader, Writer) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping facade integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := dbstore.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("ProvisionSchemas: %v", err)
	}
	if _, err := pool.Pgx().Exec(context.Background(), `TRUNCATE alias_aliases`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx())
}

func TestEnsureAliasIdempotent(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	first, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-42")
	if err != nil {
		t.Fatalf("EnsureAlias (first): %v", err)
	}
	second, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-42")
	if err != nil {
		t.Fatalf("EnsureAlias (second): %v", err)
	}
	if first.Target != second.Target || first.CreatedAt != second.CreatedAt {
		t.Fatalf("expected the second EnsureAlias call to return the same alias, got %+v and %+v", first, second)
	}
}

func TestEnsureAliasConflict(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	if _, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-42"); err != nil {
		t.Fatalf("EnsureAlias (first): %v", err)
	}
	_, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-99")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for a different target on an active alias, got: %v", err)
	}
}

func TestReleaseAliasThenResolveNotFound(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	if _, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-42"); err != nil {
		t.Fatalf("EnsureAlias: %v", err)
	}
	if err := ReleaseAlias(ctx, reader, writer, "documents", "readme"); err != nil {
		t.Fatalf("ReleaseAlias: %v", err)
	}

	_, found, err := evaluation.Resolve(ctx, reader, "documents", "readme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if found {
		t.Fatal("expected a released alias to resolve as not found")
	}
}

func TestReleaseAliasIdempotent(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	if _, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-42"); err != nil {
		t.Fatalf("EnsureAlias: %v", err)
	}
	if err := ReleaseAlias(ctx, reader, writer, "documents", "readme"); err != nil {
		t.Fatalf("ReleaseAlias (first): %v", err)
	}
	if err := ReleaseAlias(ctx, reader, writer, "documents", "readme"); err != nil {
		t.Fatalf("expected a second release to be a no-op, got: %v", err)
	}
}

func TestReleaseAliasNotFound(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	err := ReleaseAlias(context.Background(), reader, writer, "documents", "never-created")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestEnsureAliasReusesReleasedSlot(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	if _, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-42"); err != nil {
		t.Fatalf("EnsureAlias (first): %v", err)
	}
	if err := ReleaseAlias(ctx, reader, writer, "documents", "readme"); err != nil {
		t.Fatalf("ReleaseAlias: %v", err)
	}

	// A different target is fine now — the slot was released, so this
	// isn't a conflict with a still-active alias.
	a, err := EnsureAlias(ctx, reader, writer, "documents", "readme", "doc-99")
	if err != nil {
		t.Fatalf("EnsureAlias (reuse after release): %v", err)
	}
	if a.Target != "doc-99" || a.Lifecycle != "active" {
		t.Fatalf("expected the reactivated alias to carry the new target and be active, got %+v", a)
	}

	target, found, err := evaluation.Resolve(ctx, reader, "documents", "readme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || target != "doc-99" {
		t.Fatalf("Resolve = (%q, %v), want (doc-99, true)", target, found)
	}
}
