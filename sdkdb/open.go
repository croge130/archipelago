package sdkdb

import (
	"context"
	"fmt"

	aliasDB "github.com/croge130/archipelago/alias/storage/dbstore"
	certstoreDB "github.com/croge130/archipelago/certstore/storage/dbstore"
	archidb "github.com/croge130/archipelago/db"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	jobsDB "github.com/croge130/archipelago/jobs/storage/dbstore"
	policyDB "github.com/croge130/archipelago/policy/storage/dbstore"
	"github.com/croge130/archipelago/sdk"
	vitalsDB "github.com/croge130/archipelago/vitals/storage/dbstore"
)

// OpenDB opens a connection pool, provisions the schema for each base
// modes enables (and only those — a mode left off creates no tables and
// leaves its Stores entry empty), and returns read/write Stores over it.
// Read/write is what a DSN with full privileges gives; a caller whose
// access is narrower zeroes the Writer fields (or whole entries) of the
// returned Stores before handing them to sdk.New.
//
// The caller owns the returned pool and must Close it.
func OpenDB(ctx context.Context, cfg archidb.Config, modes sdk.Modes) (sdk.Stores, *archidb.Pool, error) {
	pool, err := archidb.Open(ctx, cfg)
	if err != nil {
		return sdk.Stores{}, nil, fmt.Errorf("sdkdb: %w", err)
	}

	var stores sdk.Stores
	provision := func(name string, migrate func() ([]archidb.Migration, error)) error {
		m, err := migrate()
		if err != nil {
			return fmt.Errorf("sdkdb: %s migrations: %w", name, err)
		}
		if err := pool.ProvisionSchemas(ctx, m); err != nil {
			return fmt.Errorf("sdkdb: provision %s: %w", name, err)
		}
		return nil
	}

	steps := []struct {
		enabled bool
		name    string
		migrate func() ([]archidb.Migration, error)
		wire    func()
	}{
		{modes.Gatehouse, "gatehouse-core", gatehouseDB.Migrations, func() {
			stores.Gatehouse = sdk.GatehouseStores{Reader: gatehouseDB.NewPostgresReader(pool.Pgx()), Writer: gatehouseDB.NewPostgresWriter(pool.Pgx())}
		}},
		{modes.Policy, "policy", policyDB.Migrations, func() {
			stores.Policy = sdk.PolicyStores{Reader: policyDB.NewPostgresReader(pool.Pgx()), Writer: policyDB.NewPostgresWriter(pool.Pgx())}
		}},
		{modes.Alias, "alias", aliasDB.Migrations, func() {
			stores.Alias = sdk.AliasStores{Reader: aliasDB.NewPostgresReader(pool.Pgx()), Writer: aliasDB.NewPostgresWriter(pool.Pgx())}
		}},
		{modes.Certs, "certstore", certstoreDB.Migrations, func() {
			stores.Certs = sdk.CertStores{Reader: certstoreDB.NewPostgresReader(pool.Pgx()), Writer: certstoreDB.NewPostgresWriter(pool.Pgx())}
		}},
		{modes.Jobs, "jobs", jobsDB.Migrations, func() {
			stores.Jobs = sdk.JobsStores{Reader: jobsDB.NewPostgresReader(pool.Pgx()), Writer: jobsDB.NewPostgresWriter(pool.Pgx())}
		}},
		{modes.Vitals, "vitals", vitalsDB.Migrations, func() {
			stores.Vitals = sdk.VitalsStores{Reader: vitalsDB.NewPostgresReader(pool.Pgx()), Writer: vitalsDB.NewPostgresWriter(pool.Pgx())}
		}},
	}
	for _, s := range steps {
		if !s.enabled {
			continue
		}
		if err := provision(s.name, s.migrate); err != nil {
			pool.Close()
			return sdk.Stores{}, nil, err
		}
		s.wire()
	}
	return stores, pool, nil
}
