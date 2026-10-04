// Tests in this file need a real PostgreSQL instance; they skip
// without ARCHIPELAGO_TEST_DATABASE_URL, same convention as every
// other module's own integration tests.
package facade

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/croge130/archipelago/vitals/storage/dbstore"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

func setupTest(t *testing.T) (*dbstore.PostgresReader, *dbstore.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping vitals integration test")
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

	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE
		vitals_group_members, vitals_groups, vitals_reading_history, vitals_current_readings,
		vitals_instances, vitals_definitions
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return dbstore.NewPostgresReader(pool.Pgx()), dbstore.NewPostgresWriter(pool.Pgx())
}

func ensureTestDefinition(t *testing.T, ctx context.Context, reader Reader, writer Writer, key string) structure.Definition {
	t.Helper()
	d, err := EnsureDefinition(ctx, reader, writer, structure.Definition{DefinitionKey: key, DefinitionVersion: 1})
	if err != nil {
		t.Fatalf("EnsureDefinition: %v", err)
	}
	return d
}

func ensureTestInstance(t *testing.T, ctx context.Context, reader Reader, writer Writer, def structure.Definition, scopeID, instanceKey string) structure.Instance {
	t.Helper()
	i, err := EnsureInstance(ctx, reader, writer, structure.Instance{
		ScopeType: "gatehouse.context", ScopeID: scopeID, InstanceKey: instanceKey, DefinitionID: def.DefinitionID,
	})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	return i
}

func TestEnsureDefinitionIdempotentAndConflict(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	first, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
		DefinitionKey: "myapp.importer.status", DefinitionVersion: 1, SchemaHash: "abc",
	})
	if err != nil {
		t.Fatalf("EnsureDefinition (first): %v", err)
	}

	second, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
		DefinitionKey: "myapp.importer.status", DefinitionVersion: 1, SchemaHash: "abc",
	})
	if err != nil {
		t.Fatalf("EnsureDefinition (same shape again): %v", err)
	}
	if second.DefinitionID != first.DefinitionID {
		t.Fatalf("expected the same DefinitionID on an idempotent re-ensure, got %s vs %s", second.DefinitionID, first.DefinitionID)
	}

	if _, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
		DefinitionKey: "myapp.importer.status", DefinitionVersion: 1, SchemaHash: "different",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for a different schema hash under the same key+version, got: %v", err)
	}

	// A new version is always a new definition, never a conflict.
	v2, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
		DefinitionKey: "myapp.importer.status", DefinitionVersion: 2, SchemaHash: "different",
	})
	if err != nil {
		t.Fatalf("EnsureDefinition (new version): %v", err)
	}
	if v2.DefinitionID == first.DefinitionID {
		t.Fatal("expected a new version to get its own DefinitionID")
	}
}

func TestSeedBuiltinDefinitionsIsIdempotent(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	if err := SeedBuiltinDefinitions(ctx, reader, writer); err != nil {
		t.Fatalf("SeedBuiltinDefinitions (first): %v", err)
	}
	if err := SeedBuiltinDefinitions(ctx, reader, writer); err != nil {
		t.Fatalf("SeedBuiltinDefinitions (second): %v", err)
	}

	d, found, err := reader.GetDefinitionByKeyVersion(ctx, structure.BuiltinServiceLiveness, 1)
	if err != nil {
		t.Fatalf("GetDefinitionByKeyVersion: %v", err)
	}
	if !found || d.DefinitionKey != structure.BuiltinServiceLiveness {
		t.Fatalf("expected %s to be seeded, found=%v", structure.BuiltinServiceLiveness, found)
	}
}

func TestEnsureInstanceIdempotent(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	first := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")
	second := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")
	if first.InstanceID != second.InstanceID {
		t.Fatalf("expected the same InstanceID on an idempotent re-ensure, got %s vs %s", first.InstanceID, second.InstanceID)
	}
}

func TestWriteReadingFirstIsAlwaysNotable(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")

	got, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK})
	if err != nil {
		t.Fatalf("WriteReading: %v", err)
	}
	if got.Revision != 1 {
		t.Fatalf("Revision = %d, want 1", got.Revision)
	}

	history, err := reader.ListHistory(ctx, inst.InstanceID, 10)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected the first reading to produce exactly one history row, got %d", len(history))
	}
}

func TestWriteReadingUnknownInstanceDenied(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	if _, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: uuid.New(), State: structure.StateOK}); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("expected ErrInstanceNotFound, got: %v", err)
	}
}

// TestWriteReadingNormalizesValueAgainstDefinitionValueMetadata proves
// the end-to-end wiring, not just evaluation.NormalizeAndValidateValue
// in isolation: a Definition that declares ValueMetadata gets that
// shape enforced on every WriteReading through this instance, and the
// stored Value is the normalized canonical form, not the caller's raw
// bytes.
func TestWriteReadingNormalizesValueAgainstDefinitionValueMetadata(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	vm := typedvalue.Count("item", "queue depth")
	def, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
		DefinitionKey: "myapp.queue.depth", DefinitionVersion: 1, ValueMetadata: &vm,
	})
	if err != nil {
		t.Fatalf("EnsureDefinition: %v", err)
	}
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.queue.depth")

	got, err := WriteReading(ctx, reader, writer, structure.Reading{
		InstanceID: inst.InstanceID, State: structure.StateOK, Value: json.RawMessage(`"42"`),
	})
	if err != nil {
		t.Fatalf("WriteReading with a value conforming to ValueMetadata: %v", err)
	}
	if string(got.Value) != "42" {
		t.Fatalf("expected the stored Value to be normalized to 42, got %s", got.Value)
	}
}

// TestWriteReadingRejectsValueNotConformingToValueMetadata is the
// other half: once a Definition declares ValueMetadata, a Value that
// doesn't fit it is rejected outright rather than stored as-is —
// enforcement is mandatory whenever ValueMetadata is set, not a
// separate opt-in.
func TestWriteReadingRejectsValueNotConformingToValueMetadata(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	vm := typedvalue.Count("item", "queue depth")
	def, err := EnsureDefinition(ctx, reader, writer, structure.Definition{
		DefinitionKey: "myapp.queue.depth.rejects", DefinitionVersion: 1, ValueMetadata: &vm,
	})
	if err != nil {
		t.Fatalf("EnsureDefinition: %v", err)
	}
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.queue.depth.rejects")

	if _, err := WriteReading(ctx, reader, writer, structure.Reading{
		InstanceID: inst.InstanceID, State: structure.StateOK, Value: json.RawMessage(`"not a number"`),
	}); err == nil {
		t.Fatal("expected WriteReading to reject a Value that doesn't conform to the Definition's ValueMetadata")
	}
}

func TestWriteReadingRepeatedSameStateDoesNotRecordHistory(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")

	if _, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK}); err != nil {
		t.Fatalf("WriteReading (1): %v", err)
	}
	got, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK})
	if err != nil {
		t.Fatalf("WriteReading (2, repeat): %v", err)
	}
	if got.Revision != 2 {
		t.Fatalf("Revision = %d, want 2 — the current row still advances even when nothing notable happened", got.Revision)
	}

	history, err := reader.ListHistory(ctx, inst.InstanceID, 10)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected the repeat same-state write to add no history row, got %d rows", len(history))
	}
}

func TestWriteReadingStateChangeRecordsHistoryAndQualityWarning(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")

	if _, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK}); err != nil {
		t.Fatalf("WriteReading (1): %v", err)
	}

	traceID := logging.NewTraceID()
	spanID := logging.NewSpanID()
	got, err := WriteReading(ctx, reader, writer, structure.Reading{
		InstanceID: inst.InstanceID, State: structure.StateOK, Impact: structure.ImpactCritical,
		TraceContext: &logging.SpanContext{TraceID: traceID, SpanID: spanID},
	})
	if err != nil {
		t.Fatalf("WriteReading (2, state unchanged but impact changed): %v", err)
	}
	// ok is the ultimate default expected state (no ExpectedStates set
	// on either the instance or its definition), so both rules
	// legitimately co-fire: ok+critical, and critical also counting as
	// high-impact on an expected state.
	hasOKWithCritical := false
	for _, w := range got.QualityWarnings {
		if w.Code == "ok_with_critical_impact" {
			hasOKWithCritical = true
		}
	}
	if !hasOKWithCritical {
		t.Fatalf("expected an ok_with_critical_impact quality warning, got %v", got.QualityWarnings)
	}
	if got.TraceContext == nil || got.TraceContext.TraceID != traceID {
		t.Fatalf("expected the trace context to round-trip, got %+v", got.TraceContext)
	}

	history, err := reader.ListHistory(ctx, inst.InstanceID, 10)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected the impact change to add a second history row, got %d", len(history))
	}
}

func TestGroupEnsureAndMembershipLifecycle(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")

	group, err := EnsureGroup(ctx, reader, writer, structure.Group{ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "default"})
	if err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	if err := SetGroupMember(ctx, writer, structure.GroupMember{
		GroupID: group.GroupID, MemberKey: "importer", MemberKind: structure.MemberKindVitalInstance, VitalInstanceID: &inst.InstanceID,
	}); err != nil {
		t.Fatalf("SetGroupMember: %v", err)
	}

	members, err := reader.ListGroupMembers(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 1 || members[0].MemberKey != "importer" {
		t.Fatalf("expected exactly one member \"importer\", got %+v", members)
	}

	if err := RemoveGroupMember(ctx, writer, group.GroupID, "importer"); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	members, err = reader.ListGroupMembers(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupMembers (after remove): %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members after removal, got %+v", members)
	}
}

func TestGroupMembershipRejectsCycle(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	parent, err := EnsureGroup(ctx, reader, writer, structure.Group{ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "parent"})
	if err != nil {
		t.Fatalf("EnsureGroup (parent): %v", err)
	}
	child, err := EnsureGroup(ctx, reader, writer, structure.Group{ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "child"})
	if err != nil {
		t.Fatalf("EnsureGroup (child): %v", err)
	}

	// parent includes child.
	if err := SetGroupMember(ctx, writer, structure.GroupMember{
		GroupID: parent.GroupID, MemberKey: "child-ref", MemberKind: structure.MemberKindVitalGroup, ChildGroupID: &child.GroupID,
	}); err != nil {
		t.Fatalf("SetGroupMember (parent includes child): %v", err)
	}

	// child including parent back would be a cycle.
	err = SetGroupMember(ctx, writer, structure.GroupMember{
		GroupID: child.GroupID, MemberKey: "parent-ref", MemberKind: structure.MemberKindVitalGroup, ChildGroupID: &parent.GroupID,
	})
	if !errors.Is(err, dbstore.ErrGroupMembershipCycle) {
		t.Fatalf("expected a cycle rejection, got: %v", err)
	}
}

// TestGroupMembershipConcurrentComplementaryEdgesNeverBothSucceed is
// the actual race UpsertGroupMember's advisory lock exists to close:
// two goroutines concurrently adding complementary edges (A includes
// B; B includes A) must never both succeed — without serialization,
// each could run its cycle check against the other's pre-commit state
// and both pass, writing a real cycle into the data despite the check.
func TestGroupMembershipConcurrentComplementaryEdgesNeverBothSucceed(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	a, err := EnsureGroup(ctx, reader, writer, structure.Group{ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "a"})
	if err != nil {
		t.Fatalf("EnsureGroup (a): %v", err)
	}
	b, err := EnsureGroup(ctx, reader, writer, structure.Group{ScopeType: "gatehouse.context", ScopeID: "myapp", GroupKey: "b"})
	if err != nil {
		t.Fatalf("EnsureGroup (b): %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = SetGroupMember(ctx, writer, structure.GroupMember{
			GroupID: a.GroupID, MemberKey: "b-ref", MemberKind: structure.MemberKindVitalGroup, ChildGroupID: &b.GroupID,
		})
	}()
	go func() {
		defer wg.Done()
		errs[1] = SetGroupMember(ctx, writer, structure.GroupMember{
			GroupID: b.GroupID, MemberKey: "a-ref", MemberKind: structure.MemberKindVitalGroup, ChildGroupID: &a.GroupID,
		})
	}()
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, dbstore.ErrGroupMembershipCycle):
			// expected for whichever of the two loses the race
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one of the two complementary edges to succeed, got %d (errs: %v)", succeeded, errs)
	}
}

func TestWriteReadingPreservesObservedAtSeparatelyFromUpdatedAt(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")

	observed := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	got, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateOK, ObservedAt: &observed})
	if err != nil {
		t.Fatalf("WriteReading: %v", err)
	}
	if got.ObservedAt == nil || !got.ObservedAt.Equal(observed) {
		t.Fatalf("expected ObservedAt to round-trip as given, got %v", got.ObservedAt)
	}
	if got.UpdatedAt.Equal(observed) || got.UpdatedAt.Before(observed) {
		t.Fatalf("expected UpdatedAt to be set to now, independent of ObservedAt, got %v (observed %v)", got.UpdatedAt, observed)
	}
}

// TestWriteReadingConcurrentWritesToSameInstanceSerializeCorrectly is
// the actual race WriteReadingAtomic's SELECT ... FOR UPDATE exists to
// close: two goroutines concurrently writing different new states for
// the *same* instance, on top of an existing baseline reading, must
// serialize into a consistent chain — revision 1 (baseline), then 2
// and 3 in some order, never two writes both landing on the same
// revision or either one silently vanishing. Without the fix, both
// goroutines could read revision 1 as "previous," each compute
// revision 2, and race on which one's write (and which one's history
// row) actually survives.
func TestWriteReadingConcurrentWritesToSameInstanceSerializeCorrectly(t *testing.T) {
	reader, writer := setupTest(t)
	ctx := context.Background()

	def := ensureTestDefinition(t, ctx, reader, writer, "myapp.importer.status")
	inst := ensureTestInstance(t, ctx, reader, writer, def, "myapp", "core.services.importer.status")

	if _, err := WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateRunning}); err != nil {
		t.Fatalf("WriteReading (baseline): %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateDegraded})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = WriteReading(ctx, reader, writer, structure.Reading{InstanceID: inst.InstanceID, State: structure.StateFailed})
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("WriteReading (concurrent %d): %v", i, err)
		}
	}

	final, found, err := reader.GetReading(ctx, inst.InstanceID)
	if err != nil {
		t.Fatalf("GetReading: %v", err)
	}
	if !found {
		t.Fatal("expected a current reading to exist")
	}
	if final.Revision != 3 {
		t.Fatalf("Revision = %d, want 3 (1 baseline + 2 correctly serialized concurrent writes)", final.Revision)
	}
	if final.State != structure.StateDegraded && final.State != structure.StateFailed {
		t.Fatalf("expected the final state to be whichever concurrent write landed last, got %q", final.State)
	}

	history, err := reader.ListHistory(ctx, inst.InstanceID, 10)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected exactly 3 history rows (baseline + 2 serialized transitions), got %d: %+v", len(history), history)
	}
}
