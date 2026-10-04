// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. Both Policy's and Vitals'
// schemas are provisioned against the same database — their tables
// live in disjoint namespaces (policy_* vs. vitals_*), so one real
// Postgres instance serves both stores, proving the integration
// against real data in both, not fakes.
package vitalsdefaults

import (
	"context"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	policyDB "github.com/croge130/archipelago/policy/storage/dbstore"
	vitalsDB "github.com/croge130/archipelago/vitals/storage/dbstore"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

func setupTest(t *testing.T) (*policyDB.PostgresReader, *policyDB.PostgresWriter, *vitalsDB.PostgresReader, *vitalsDB.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping vitalsdefaults integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	policyMigrations, err := policyDB.Migrations()
	if err != nil {
		t.Fatalf("policy Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), policyMigrations); err != nil {
		t.Fatalf("policy ProvisionSchemas: %v", err)
	}
	vitalsMigrations, err := vitalsDB.Migrations()
	if err != nil {
		t.Fatalf("vitals Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), vitalsMigrations); err != nil {
		t.Fatalf("vitals ProvisionSchemas: %v", err)
	}

	if _, err := pool.Pgx().Exec(context.Background(),
		`TRUNCATE policy_instances, policy_context_members, policy_contexts, policy_definitions`,
	); err != nil {
		t.Fatalf("truncate policy: %v", err)
	}
	if _, err := pool.Pgx().Exec(context.Background(),
		`TRUNCATE vitals_group_members, vitals_groups, vitals_reading_history, vitals_current_readings, vitals_instances, vitals_definitions`,
	); err != nil {
		t.Fatalf("truncate vitals: %v", err)
	}

	return policyDB.NewPostgresReader(pool.Pgx()), policyDB.NewPostgresWriter(pool.Pgx()),
		vitalsDB.NewPostgresReader(pool.Pgx()), vitalsDB.NewPostgresWriter(pool.Pgx())
}

// seedGroup bypasses vitalsauth/vitals' own facade and writes a Group
// directly through the base — the resolver's job is to find it, not
// to create it.
func seedGroup(t *testing.T, ctx context.Context, vitalsWriter *vitalsDB.PostgresWriter, scopeType, scopeID, groupKey string) structure.Group {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	g := structure.Group{GroupID: uuid.New(), ScopeType: scopeType, ScopeID: scopeID, GroupKey: groupKey, CreatedAt: now, UpdatedAt: now}
	if err := vitalsWriter.CreateGroup(ctx, g); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	return g
}

func TestEnsurePolicyDefinitionIsIdempotent(t *testing.T) {
	policyReader, policyWriter, _, _ := setupTest(t)
	ctx := context.Background()

	first, err := EnsurePolicyDefinition(ctx, policyReader, policyWriter, "operator:christian")
	if err != nil {
		t.Fatalf("EnsurePolicyDefinition (first): %v", err)
	}
	second, err := EnsurePolicyDefinition(ctx, policyReader, policyWriter, "operator:christian")
	if err != nil {
		t.Fatalf("EnsurePolicyDefinition (second): %v", err)
	}
	if first.PolicyDefinitionID != second.PolicyDefinitionID {
		t.Fatalf("expected the same PolicyDefinitionID on an idempotent re-ensure, got %s vs %s", first.PolicyDefinitionID, second.PolicyDefinitionID)
	}
}

func TestResolveDefaultGroupWithNothingConfigured(t *testing.T) {
	policyReader, policyWriter, vitalsReader, _ := setupTest(t)
	ctx := context.Background()

	if _, err := EnsurePolicyDefinition(ctx, policyReader, policyWriter, "operator:christian"); err != nil {
		t.Fatalf("EnsurePolicyDefinition: %v", err)
	}

	_, found, err := ResolveDefaultGroup(ctx, policyReader, vitalsReader, "gatehouse.context", "myapp")
	if err != nil {
		t.Fatalf("ResolveDefaultGroup: %v", err)
	}
	if found {
		t.Fatal("expected no default to be configured, got found=true")
	}
}

func TestResolveDefaultGroupFallsBackToGlobal(t *testing.T) {
	policyReader, policyWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx := context.Background()

	def, err := EnsurePolicyDefinition(ctx, policyReader, policyWriter, "operator:christian")
	if err != nil {
		t.Fatalf("EnsurePolicyDefinition: %v", err)
	}
	fallback := seedGroup(t, ctx, vitalsWriter, "archipelago", "global", "overview")
	if _, err := SetGlobalDefault(ctx, policyReader, policyWriter, def, fallback.ScopeType, fallback.ScopeID, fallback.GroupKey, "operator:christian"); err != nil {
		t.Fatalf("SetGlobalDefault: %v", err)
	}

	got, found, err := ResolveDefaultGroup(ctx, policyReader, vitalsReader, "gatehouse.context", "myapp")
	if err != nil {
		t.Fatalf("ResolveDefaultGroup: %v", err)
	}
	if !found || got.GroupID != fallback.GroupID {
		t.Fatalf("expected the global fallback group, got found=%v group=%+v", found, got)
	}
}

func TestResolveDefaultGroupScopedOverrideWinsOverGlobal(t *testing.T) {
	policyReader, policyWriter, vitalsReader, vitalsWriter := setupTest(t)
	ctx := context.Background()

	def, err := EnsurePolicyDefinition(ctx, policyReader, policyWriter, "operator:christian")
	if err != nil {
		t.Fatalf("EnsurePolicyDefinition: %v", err)
	}
	fallback := seedGroup(t, ctx, vitalsWriter, "archipelago", "global", "overview")
	if _, err := SetGlobalDefault(ctx, policyReader, policyWriter, def, fallback.ScopeType, fallback.ScopeID, fallback.GroupKey, "operator:christian"); err != nil {
		t.Fatalf("SetGlobalDefault: %v", err)
	}

	scoped := seedGroup(t, ctx, vitalsWriter, "gatehouse.context", "myapp", "default")
	if _, err := SetScopedDefault(ctx, policyReader, policyWriter, def, "gatehouse.context", "myapp", scoped.ScopeType, scoped.ScopeID, scoped.GroupKey, "operator:christian"); err != nil {
		t.Fatalf("SetScopedDefault: %v", err)
	}

	// The scoped override should win for myapp specifically.
	got, found, err := ResolveDefaultGroup(ctx, policyReader, vitalsReader, "gatehouse.context", "myapp")
	if err != nil {
		t.Fatalf("ResolveDefaultGroup (myapp): %v", err)
	}
	if !found || got.GroupID != scoped.GroupID {
		t.Fatalf("expected the scoped override to win, got found=%v group=%+v", found, got)
	}

	// A different scope with no override of its own should still fall
	// back to the global default.
	got, found, err = ResolveDefaultGroup(ctx, policyReader, vitalsReader, "gatehouse.context", "other-app")
	if err != nil {
		t.Fatalf("ResolveDefaultGroup (other-app): %v", err)
	}
	if !found || got.GroupID != fallback.GroupID {
		t.Fatalf("expected the global fallback for an un-overridden scope, got found=%v group=%+v", found, got)
	}
}
