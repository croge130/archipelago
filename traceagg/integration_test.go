// Tests in this file need no database — traceagg has no storage of
// its own. Delivery is exercised over a real (not mocked) transit/inmem
// Session pair, the same shape sso's own integration test uses for
// ticket delivery, proving PushEntries/Ingest compose with an actual
// Transit message round-trip.
package traceagg

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func TestPushEntriesAndIngestEndToEndOverRealTransit(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()

	reporterSide, coordinatorSide := inmem.NewPipe()
	defer reporterSide.Close()
	defer coordinatorSide.Close()

	sourceInstanceID := uuid.New()
	entries := []logging.Entry{
		{Time: time.Now(), Level: "INFO", Message: "job started", TraceID: "trace-1", SpanID: "span-1"},
		{Time: time.Now(), Level: "NOTICE", Message: "job finished", TraceID: "trace-1", SpanID: "span-2"},
	}

	if err := PushEntries(ctx, reporterSide, sourceInstanceID, entries); err != nil {
		t.Fatalf("PushEntries: %v", err)
	}

	received, err := coordinatorSide.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	collector := NewCollector()
	if err := collector.Ingest(received); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	got := collector.Trace("trace-1")
	if len(got) != 2 {
		t.Fatalf("Trace(trace-1) = %+v, want 2 entries", got)
	}
	if got[0].Message != "job started" || got[1].Message != "job finished" {
		t.Fatalf("Trace(trace-1) order = [%q, %q], want [job started, job finished]", got[0].Message, got[1].Message)
	}
	if got[0].SourceInstanceID != sourceInstanceID {
		t.Fatalf("SourceInstanceID = %s, want %s", got[0].SourceInstanceID, sourceInstanceID)
	}
}

func TestIngestRejectsWrongMessageType(t *testing.T) {
	collector := NewCollector()
	err := collector.Ingest(wire.Message{Type: "something.else", Payload: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected Ingest to reject a message of the wrong type")
	}
}

func TestIngestDropsEntriesWithNoTraceID(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()

	a, b := inmem.NewPipe()
	defer a.Close()
	defer b.Close()

	entries := []logging.Entry{
		{Time: time.Now(), Message: "no trace at all"},
		{Time: time.Now(), Message: "has a trace", TraceID: "trace-1", SpanID: "span-1"},
	}
	if err := PushEntries(ctx, a, uuid.New(), entries); err != nil {
		t.Fatalf("PushEntries: %v", err)
	}
	received, err := b.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	collector := NewCollector()
	if err := collector.Ingest(received); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got := collector.Trace("trace-1"); len(got) != 1 || got[0].Message != "has a trace" {
		t.Fatalf("Trace(trace-1) = %+v, want exactly the one entry that carried a TraceID", got)
	}
	// The untraced entry must not be queryable under an empty-string
	// bucket either — there's no trace to stitch it into.
	if got := collector.Trace(""); len(got) != 0 {
		t.Fatalf("Trace(\"\") = %+v, want nothing — an untraced entry is dropped, not bucketed", got)
	}
}

func TestCollectorTraceOrdersByTimeAcrossMultipleSources(t *testing.T) {
	collector := NewCollector()
	instanceA, instanceB := uuid.New(), uuid.New()
	base := time.Now()

	// Ingest B's (later) entry first, A's (earlier) entry second — Trace
	// must still return them ordered by each entry's own recorded Time,
	// not ingestion order.
	msgB := mustEncode(t, EntryBatch{SourceInstanceID: instanceB, Entries: []logging.Entry{
		{Time: base.Add(time.Second), Message: "from B", TraceID: "trace-1"},
	}})
	msgA := mustEncode(t, EntryBatch{SourceInstanceID: instanceA, Entries: []logging.Entry{
		{Time: base, Message: "from A", TraceID: "trace-1"},
	}})

	if err := collector.Ingest(msgB); err != nil {
		t.Fatalf("Ingest (B): %v", err)
	}
	if err := collector.Ingest(msgA); err != nil {
		t.Fatalf("Ingest (A): %v", err)
	}

	got := collector.Trace("trace-1")
	if len(got) != 2 || got[0].Message != "from A" || got[1].Message != "from B" {
		t.Fatalf("Trace(trace-1) = %+v, want [from A, from B] ordered by Time regardless of ingestion order", got)
	}
	if got[0].SourceInstanceID != instanceA || got[1].SourceInstanceID != instanceB {
		t.Fatalf("Trace(trace-1) source attribution wrong: %+v", got)
	}
}

func mustEncode(t *testing.T, batch EntryBatch) wire.Message {
	t.Helper()
	payload, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal EntryBatch: %v", err)
	}
	return wire.Message{Type: MessageTypeEntries, Kind: wire.KindEvent, Payload: payload}
}
