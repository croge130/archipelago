// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL, same convention as every
// other module's integration tests.
package dbstore

import (
	"context"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
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

	if _, err := pool.Pgx().Exec(context.Background(),
		`TRUNCATE policy_instances, policy_context_members, policy_contexts, policy_definitions`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return NewPostgresReader(pool.Pgx()), NewPostgresWriter(pool.Pgx())
}

func testDefinition(now time.Time) structure.PolicyDefinition {
	return structure.PolicyDefinition{
		PolicyDefinitionID: uuid.New(),
		PolicyKey:          "myapp.rate_limit.max_requests",
		ValueType:          typedvalue.Count("request", "the rate limit ceiling"),
		Merge:              typeconstraints.MergeMinimum,
		Activation:         structure.ActivationImmediate,
		DefaultBinding:     structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedBy:          "operator:christian",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestDBStoreCreateAndGetPolicyDefinition(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	def := testDefinition(now)
	if err := writer.CreatePolicyDefinition(ctx, def); err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}

	got, found, err := reader.GetPolicyDefinition(ctx, def.PolicyKey)
	if err != nil {
		t.Fatalf("GetPolicyDefinition: %v", err)
	}
	if !found || got.Merge != typeconstraints.MergeMinimum || got.ValueType.Storage != typedvalue.StorageInt {
		t.Fatalf("GetPolicyDefinition = %+v, found=%v", got, found)
	}
}

func TestDBStoreGetPolicyDefinitionNotFound(t *testing.T) {
	reader, _ := setupTestStore(t)
	_, found, err := reader.GetPolicyDefinition(context.Background(), "nonexistent.key")
	if err != nil {
		t.Fatalf("GetPolicyDefinition: %v", err)
	}
	if found {
		t.Fatal("expected not found for a never-created definition")
	}
}

func TestDBStoreGlobalInstanceRoundTripsIntegerType(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	def := testDefinition(now)
	if err := writer.CreatePolicyDefinition(ctx, def); err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}

	inst := structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         structure.TargetKindGlobal,
		Value:              int64(1000),
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := writer.CreatePolicyInstance(ctx, inst); err != nil {
		t.Fatalf("CreatePolicyInstance: %v", err)
	}

	got, found, err := reader.GlobalInstance(ctx, def.PolicyDefinitionID)
	if err != nil {
		t.Fatalf("GlobalInstance: %v", err)
	}
	if !found {
		t.Fatal("expected to find the global instance")
	}
	// The exact point normalizeStoredValue exists for: jsonb round
	// trips a bare number as float64 by default, and this must come
	// back int64 to match what was written and what Merge expects.
	if v, ok := got.Value.(int64); !ok || v != 1000 {
		t.Fatalf("got.Value = %v (%T), want int64(1000)", got.Value, got.Value)
	}
}

func TestDBStoreRefInstances(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	def := testDefinition(now)
	if err := writer.CreatePolicyDefinition(ctx, def); err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}

	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	inst := structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         structure.TargetKindRef,
		Ref:                &ref,
		Value:              int64(200),
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := writer.CreatePolicyInstance(ctx, inst); err != nil {
		t.Fatalf("CreatePolicyInstance: %v", err)
	}

	got, err := reader.RefInstances(ctx, def.PolicyDefinitionID, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("RefInstances: %v", err)
	}
	if len(got) != 1 || got[0].Value != int64(200) {
		t.Fatalf("RefInstances = %+v, want exactly one instance valued 200", got)
	}

	none, err := reader.RefInstances(ctx, def.PolicyDefinitionID, []structure.Ref{{Kind: "service", Key: "other"}})
	if err != nil {
		t.Fatalf("RefInstances: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no instances for an unrelated ref, got %+v", none)
	}
}

func TestDBStorePolicyContextMembershipAndInstances(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	def := testDefinition(now)
	if err := writer.CreatePolicyDefinition(ctx, def); err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}

	policyCtx := structure.PolicyContext{PolicyContextID: uuid.New(), Key: "internet-facing", CreatedAt: now, UpdatedAt: now}
	if err := writer.CreatePolicyContext(ctx, policyCtx); err != nil {
		t.Fatalf("CreatePolicyContext: %v", err)
	}

	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	if err := writer.AddPolicyContextMember(ctx, structure.PolicyContextMember{
		PolicyContextID: policyCtx.PolicyContextID, Ref: ref, CreatedAt: now,
	}); err != nil {
		t.Fatalf("AddPolicyContextMember: %v", err)
	}
	// Idempotent: adding the same member again must not error.
	if err := writer.AddPolicyContextMember(ctx, structure.PolicyContextMember{
		PolicyContextID: policyCtx.PolicyContextID, Ref: ref, CreatedAt: now,
	}); err != nil {
		t.Fatalf("AddPolicyContextMember (second time): %v", err)
	}

	ids, err := reader.PolicyContextIDsForRefs(ctx, []structure.Ref{ref})
	if err != nil {
		t.Fatalf("PolicyContextIDsForRefs: %v", err)
	}
	if len(ids) != 1 || ids[0] != policyCtx.PolicyContextID {
		t.Fatalf("PolicyContextIDsForRefs = %v, want [%v]", ids, policyCtx.PolicyContextID)
	}

	ctxInst := structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         structure.TargetKindPolicyContext,
		PolicyContextID:    &policyCtx.PolicyContextID,
		Value:              int64(50),
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := writer.CreatePolicyInstance(ctx, ctxInst); err != nil {
		t.Fatalf("CreatePolicyInstance: %v", err)
	}

	instances, err := reader.PolicyContextInstances(ctx, def.PolicyDefinitionID, ids)
	if err != nil {
		t.Fatalf("PolicyContextInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].Value != int64(50) {
		t.Fatalf("PolicyContextInstances = %+v, want exactly one instance valued 50", instances)
	}

	members, err := reader.ListPolicyContextMembers(ctx, policyCtx.PolicyContextID)
	if err != nil {
		t.Fatalf("ListPolicyContextMembers: %v", err)
	}
	if len(members) != 1 || members[0] != ref {
		t.Fatalf("ListPolicyContextMembers = %v, want [%v]", members, ref)
	}

	if err := writer.RemovePolicyContextMember(ctx, policyCtx.PolicyContextID, ref); err != nil {
		t.Fatalf("RemovePolicyContextMember: %v", err)
	}
	remaining, err := reader.ListPolicyContextMembers(ctx, policyCtx.PolicyContextID)
	if err != nil {
		t.Fatalf("ListPolicyContextMembers: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no members after removal, got %v", remaining)
	}
}

func TestDBStoreUpdatePolicyInstanceArchives(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	def := testDefinition(now)
	if err := writer.CreatePolicyDefinition(ctx, def); err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}
	inst := structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         structure.TargetKindGlobal,
		Value:              int64(1000),
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := writer.CreatePolicyInstance(ctx, inst); err != nil {
		t.Fatalf("CreatePolicyInstance: %v", err)
	}

	inst.Lifecycle = structure.LifecycleArchived
	inst.UpdatedAt = now.Add(time.Minute)
	if err := writer.UpdatePolicyInstance(ctx, inst); err != nil {
		t.Fatalf("UpdatePolicyInstance: %v", err)
	}

	_, found, err := reader.GlobalInstance(ctx, def.PolicyDefinitionID)
	if err != nil {
		t.Fatalf("GlobalInstance: %v", err)
	}
	if found {
		t.Fatal("expected GlobalInstance to no longer find an archived instance")
	}

	got, found, err := reader.GetPolicyInstance(ctx, inst.PolicyInstanceID)
	if err != nil {
		t.Fatalf("GetPolicyInstance: %v", err)
	}
	if !found || got.Lifecycle != structure.LifecycleArchived {
		t.Fatalf("GetPolicyInstance = %+v, found=%v, want archived", got, found)
	}
}

func TestDBStoreActiveNonGlobalInstances(t *testing.T) {
	reader, writer := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Microsecond)

	def := testDefinition(now)
	if err := writer.CreatePolicyDefinition(ctx, def); err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}
	writer.CreatePolicyInstance(ctx, structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         structure.TargetKindGlobal,
		Value:              int64(1000),
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	ref := structure.Ref{Kind: "service", Key: "gamebridge"}
	if err := writer.CreatePolicyInstance(ctx, structure.PolicyInstance{
		PolicyInstanceID:   uuid.New(),
		PolicyDefinitionID: def.PolicyDefinitionID,
		TargetKind:         structure.TargetKindRef,
		Ref:                &ref,
		Value:              int64(200),
		Binding:            structure.BindingInheritedLive,
		Lifecycle:          structure.LifecycleActive,
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("CreatePolicyInstance: %v", err)
	}

	nonGlobal, err := reader.ActiveNonGlobalInstances(ctx, def.PolicyDefinitionID)
	if err != nil {
		t.Fatalf("ActiveNonGlobalInstances: %v", err)
	}
	if len(nonGlobal) != 1 || nonGlobal[0].Value != int64(200) {
		t.Fatalf("ActiveNonGlobalInstances = %+v, want exactly the ref instance", nonGlobal)
	}
}
