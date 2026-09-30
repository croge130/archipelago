package db

import (
	"context"
	"fmt"
	"sort"
)

// Migration is one forward-only schema change owned by a single base
// module. Version numbers are scoped per Module — gatehouse-core's
// version 1 has nothing to do with policy's version 1 — since bases
// never need to know about each other's schema history, the same way
// they never import each other's Go packages.
type Migration struct {
	Module  string
	Version int
	Name    string
	SQL     string
}

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	module     text NOT NULL,
	version    integer NOT NULL,
	name       text NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (module, version)
)`

// ProvisionSchemas applies every migration in migrations that hasn't
// already been recorded as applied, in (module, version) order.
// Migrations are applied one at a time, each in its own transaction, so
// a failure partway through leaves every prior migration committed and
// only the failing one uncommitted — safe to fix and re-run.
func (p *Pool) ProvisionSchemas(ctx context.Context, migrations []Migration) error {
	if err := validateMigrations(migrations); err != nil {
		return err
	}
	if _, err := p.pgx.Exec(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("db: ensure schema_migrations table: %w", err)
	}

	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Module != sorted[j].Module {
			return sorted[i].Module < sorted[j].Module
		}
		return sorted[i].Version < sorted[j].Version
	})

	for _, m := range sorted {
		applied, err := p.isApplied(ctx, m.Module, m.Version)
		if err != nil {
			return fmt.Errorf("db: check %s/%d (%s): %w", m.Module, m.Version, m.Name, err)
		}
		if applied {
			continue
		}
		if err := p.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("db: apply %s/%d (%s): %w", m.Module, m.Version, m.Name, err)
		}
	}
	return nil
}

// validateMigrations catches a real, concrete mistake — two migrations
// registered under the same (module, version) pair, most likely two
// bases accidentally colliding or one base double-registering — before
// any SQL runs, rather than letting it surface as a confusing partial
// apply.
func validateMigrations(migrations []Migration) error {
	seen := make(map[string]map[int]bool)
	for _, m := range migrations {
		if m.Module == "" {
			return fmt.Errorf("db: migration %q has no Module", m.Name)
		}
		if seen[m.Module] == nil {
			seen[m.Module] = make(map[int]bool)
		}
		if seen[m.Module][m.Version] {
			return fmt.Errorf("db: duplicate migration %s/%d (%s)", m.Module, m.Version, m.Name)
		}
		seen[m.Module][m.Version] = true
	}
	return nil
}

func (p *Pool) isApplied(ctx context.Context, module string, version int) (bool, error) {
	var exists bool
	err := p.pgx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE module = $1 AND version = $2)`,
		module, version,
	).Scan(&exists)
	return exists, err
}

func (p *Pool) applyMigration(ctx context.Context, m Migration) error {
	tx, err := p.pgx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed

	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("execute migration SQL: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (module, version, name) VALUES ($1, $2, $3)`,
		m.Module, m.Version, m.Name,
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return tx.Commit(ctx)
}
