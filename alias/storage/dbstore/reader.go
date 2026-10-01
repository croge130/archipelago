package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/alias/structure"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresReader implements evaluation.Store directly against
// Postgres.
type PostgresReader struct {
	pool *pgxpool.Pool
}

func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

func (r *PostgresReader) GetAlias(ctx context.Context, table, name string) (structure.Alias, bool, error) {
	var a structure.Alias
	var lifecycle string
	err := r.pool.QueryRow(ctx,
		`SELECT table_name, name, target, lifecycle, created_at, updated_at, released_at
		 FROM alias_aliases WHERE table_name = $1 AND name = $2`,
		table, name,
	).Scan(&a.Table, &a.Name, &a.Target, &lifecycle, &a.CreatedAt, &a.UpdatedAt, &a.ReleasedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Alias{}, false, nil
		}
		return structure.Alias{}, false, fmt.Errorf("dbstore: get alias: %w", err)
	}
	a.Lifecycle = structure.Lifecycle(lifecycle)
	return a, true, nil
}
