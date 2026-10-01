package dbstore

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/alias/structure"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWriter implements facade.Writer directly against Postgres.
// The Writer interface itself is declared in facade, its consumer, not
// here — same reasoning as gatehouse-core/storage/dbstore's own
// PostgresWriter.
type PostgresWriter struct {
	pool *pgxpool.Pool
}

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{pool: pool}
}

// UpsertAlias sets the full row for (Table, Name), inserting it if
// absent. It's a raw set, not itself idempotent-with-conflict-
// detection logic — that logic lives in facade's EnsureAlias/
// ReleaseAlias, which decide what the row's new state should be before
// calling this.
func (w *PostgresWriter) UpsertAlias(ctx context.Context, a structure.Alias) error {
	if err := a.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO alias_aliases (table_name, name, target, lifecycle, created_at, updated_at, released_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (table_name, name) DO UPDATE SET
			target = EXCLUDED.target,
			lifecycle = EXCLUDED.lifecycle,
			updated_at = EXCLUDED.updated_at,
			released_at = EXCLUDED.released_at`,
		a.Table, a.Name, a.Target, string(a.Lifecycle), a.CreatedAt, a.UpdatedAt, a.ReleasedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: upsert alias: %w", err)
	}
	return nil
}
