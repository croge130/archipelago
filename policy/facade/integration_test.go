// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL. They exercise the facade
// through dbstore's real PostgresReader/PostgresWriter and
// evaluation.Resolve together, proving the full stack — not just
// facade.Reader/facade.Writer being satisfied by the concrete type.
package facade

import (
	"context"
	"errors"
	"os"
	"testing"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/policy/evaluation"
	"github.com/croge130/archipelago/policy/storage/dbstore"
	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
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
	if _, err := pool.Pgx().Exec(context.Background(),
		`TRUNCATE policy_instances, policy_context_members, policy_contexts, policy_definitions`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx())
}

func ensureMaxRequestsDef(t *testing.T, ctx context.Context, r Reader, w Writer, mode typeconstraints.MergeMode) structure.PolicyDefinition {
	t.Helper()
	maxVal := 10000.0
	def, err := EnsurePolicyDefinition(ctx, r, w,
		"myapp.rate_limit.max_requests",
		typedvalue.Count("request", "the rate limit ceiling"),
		typeconstraints.Set{Max: &maxVal},
		mode,
		structure.ActivationImmediate,
		structure.BindingInheritedLive,
		"operator:christian",
	)
	if err != nil {
		t.Fatalf("EnsurePolicyDefinition: %v", err)
	}
	return def
}

func TestEnsurePolicyDefinitionIdempotent(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	first := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)
	second := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)
	if first.PolicyDefinitionID != second.PolicyDefinitionID {
		t.Fatalf("expected the second EnsurePolicyDefinition call to return the same definition, got %+v and %+v", first, second)
	}
}

func TestEnsurePolicyDefinitionConflict(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)
	_, err := EnsurePolicyDefinition(ctx, reader, writer,
		"myapp.rate_limit.max_requests",
		typedvalue.Count("request", ""),
		typeconstraints.Set{},
		typeconstraints.MergeMaximum, // different mode this time
		structure.ActivationImmediate,
		structure.BindingInheritedLive,
		"operator:christian",
	)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for a different merge_mode on the same key, got: %v", err)
	}
}

func TestEnsurePolicyContextIdempotent(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()

	first, err := EnsurePolicyContext(ctx, reader, writer, "internet-facing", "services reachable from the public internet")
	if err != nil {
		t.Fatalf("EnsurePolicyContext (first): %v", err)
	}
	second, err := EnsurePolicyContext(ctx, reader, writer, "internet-facing", "a different description")
	if err != nil {
		t.Fatalf("EnsurePolicyContext (second): %v", err)
	}
	if first.PolicyContextID != second.PolicyContextID {
		t.Fatal("expected the second EnsurePolicyContext call to return the same context")
	}
}

func TestSetPolicyInstanceGlobalIsIdempotentByTarget(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)

	first, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindGlobal, nil, nil,
		1000, structure.BindingInheritedLive, "operator:christian", false)
	if err != nil {
		t.Fatalf("SetPolicyInstance (first): %v", err)
	}
	second, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindGlobal, nil, nil,
		500, structure.BindingInheritedLive, "operator:christian", false)
	if err != nil {
		t.Fatalf("SetPolicyInstance (second): %v", err)
	}
	if first.PolicyInstanceID != second.PolicyInstanceID {
		t.Fatal("expected the second SetPolicyInstance call for the same target to update the same instance")
	}
	if second.Value != int64(500) {
		t.Fatalf("second.Value = %v, want 500", second.Value)
	}
}

func TestSetPolicyInstanceRejectsOutOfRangeByDefault(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)

	_, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindGlobal, nil, nil,
		999999, structure.BindingInheritedLive, "operator:christian", false)
	if err == nil {
		t.Fatal("expected an error setting a value above the definition's Max constraint without clamp")
	}
}

func TestSetPolicyInstanceClampsWhenRequested(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)

	inst, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindGlobal, nil, nil,
		999999, structure.BindingInheritedLive, "operator:christian", true)
	if err != nil {
		t.Fatalf("SetPolicyInstance with clamp=true: %v", err)
	}
	if inst.Value != int64(10000) {
		t.Fatalf("inst.Value = %v, want 10000 (clamped to the definition's Max)", inst.Value)
	}
}

func TestSetPolicyInstanceAndResolveFullStack(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)

	if _, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindGlobal, nil, nil,
		1000, structure.BindingInheritedLive, "operator:christian", false); err != nil {
		t.Fatalf("SetPolicyInstance (global): %v", err)
	}

	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	if _, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindRef, &ref, nil,
		200, structure.BindingInheritedLive, "operator:christian", false); err != nil {
		t.Fatalf("SetPolicyInstance (ref): %v", err)
	}

	res, found, err := evaluation.Resolve(ctx, reader, def.PolicyKey, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(200) {
		t.Fatalf("Resolve = (%+v, %v), want minimum(1000, 200) = 200", res, found)
	}
}

func TestSetPolicyInstanceViaPolicyContextFullStack(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)

	policyCtx, err := EnsurePolicyContext(ctx, reader, writer, "internet-facing", "")
	if err != nil {
		t.Fatalf("EnsurePolicyContext: %v", err)
	}
	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	if err := AddPolicyContextMember(ctx, writer, policyCtx.PolicyContextID, ref); err != nil {
		t.Fatalf("AddPolicyContextMember: %v", err)
	}

	if _, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindPolicyContext, nil, &policyCtx.PolicyContextID,
		50, structure.BindingInheritedLive, "operator:christian", false); err != nil {
		t.Fatalf("SetPolicyInstance (policy context): %v", err)
	}

	res, found, err := evaluation.Resolve(ctx, reader, def.PolicyKey, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !found || res.Value != int64(50) {
		t.Fatalf("Resolve = (%+v, %v), want 50 via policy context membership", res, found)
	}
}

func TestSetPolicyInstanceEnforcesExclusivityForOverrideMode(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeOverride)

	refA := structure.Ref{Kind: "service", Key: "gamebridge"}
	if _, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindRef, &refA, nil,
		10, structure.BindingInheritedLive, "operator:christian", false); err != nil {
		t.Fatalf("SetPolicyInstance (first override): %v", err)
	}

	refB := structure.Ref{Kind: "service", Key: "torrent"}
	_, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindRef, &refB, nil,
		20, structure.BindingInheritedLive, "operator:christian", false)
	if err == nil {
		t.Fatal("expected an error creating a second active override under a non-commutative merge_mode")
	}

	// Updating the *existing* target is still fine — it's not a new
	// source, just a new value for the same one.
	if _, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindRef, &refA, nil,
		99, structure.BindingInheritedLive, "operator:christian", false); err != nil {
		t.Fatalf("expected updating the existing sole override to succeed, got: %v", err)
	}
}

func TestArchivePolicyInstanceRemovesItFromResolve(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	ctx := context.Background()
	def := ensureMaxRequestsDef(t, ctx, reader, writer, typeconstraints.MergeMinimum)

	global, err := SetPolicyInstance(ctx, reader, writer, def, structure.TargetKindGlobal, nil, nil,
		1000, structure.BindingInheritedLive, "operator:christian", false)
	if err != nil {
		t.Fatalf("SetPolicyInstance: %v", err)
	}

	if _, err := ArchivePolicyInstance(ctx, reader, writer, global.PolicyInstanceID, "operator:christian"); err != nil {
		t.Fatalf("ArchivePolicyInstance: %v", err)
	}

	_, found, err := evaluation.Resolve(ctx, reader, def.PolicyKey, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if found {
		t.Fatal("expected Resolve to no longer find an archived global instance")
	}
}

func TestArchivePolicyInstanceNotFound(t *testing.T) {
	reader, writer := setupFacadeTest(t)
	_, err := ArchivePolicyInstance(context.Background(), reader, writer, uuid.New(), "operator:christian")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
