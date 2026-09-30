package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config is what's needed to open a Pool. Pooling knobs beyond the DSN
// (max connections, lifetimes, …) are already well covered by pgxpool's
// own DSN query parameters and Config type; this stays deliberately
// narrow rather than re-exposing all of it.
type Config struct {
	// DSN is a standard PostgreSQL connection string
	// (postgres://user:pass@host:port/dbname?...).
	DSN string
}

// Pool wraps a pgxpool.Pool. All SQL run through it — here and in every
// base's storage implementation — must be parameterized (pgx
// placeholders); this wrapper doesn't change that discipline, only
// where connections come from.
type Pool struct {
	pgx *pgxpool.Pool
}

// Open parses cfg.DSN and establishes a connection pool, verifying
// connectivity with a ping before returning.
func Open(ctx context.Context, cfg Config) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("db: parse DSN: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &Pool{pgx: pool}, nil
}

// Close releases all pooled connections.
func (p *Pool) Close() {
	p.pgx.Close()
}

// Pgx is the escape hatch: a base's own storage implementation runs its
// own parameterized queries directly against the underlying
// *pgxpool.Pool, rather than this package growing query methods of its
// own for every base's schema.
func (p *Pool) Pgx() *pgxpool.Pool {
	return p.pgx
}
