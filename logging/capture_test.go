package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestRingBufferRecentReturnsOldestFirstWithinCapacity(t *testing.T) {
	buf := NewRingBuffer(10)
	buf.append(Entry{Message: "one"})
	buf.append(Entry{Message: "two"})
	buf.append(Entry{Message: "three"})

	got := buf.Recent(2)
	if len(got) != 2 || got[0].Message != "two" || got[1].Message != "three" {
		t.Fatalf("Recent(2) = %+v, want [two three]", got)
	}
}

func TestRingBufferEvictsOldestOnOverflow(t *testing.T) {
	buf := NewRingBuffer(2)
	buf.append(Entry{Message: "one"})
	buf.append(Entry{Message: "two"})
	buf.append(Entry{Message: "three"})

	got := buf.Recent(10) // asking for more than capacity still caps at what's held
	if len(got) != 2 || got[0].Message != "two" || got[1].Message != "three" {
		t.Fatalf("Recent(10) after overflow = %+v, want [two three] (oldest evicted)", got)
	}
}

func TestRingBufferByTraceIDFiltersAcrossEvictions(t *testing.T) {
	buf := NewRingBuffer(3)
	buf.append(Entry{Message: "a", TraceID: "trace-1"})
	buf.append(Entry{Message: "b", TraceID: "trace-2"})
	buf.append(Entry{Message: "c", TraceID: "trace-1"})
	buf.append(Entry{Message: "d", TraceID: "trace-1"}) // evicts "a"

	got := buf.ByTraceID("trace-1")
	if len(got) != 2 || got[0].Message != "c" || got[1].Message != "d" {
		t.Fatalf("ByTraceID(trace-1) = %+v, want [c d] (a was evicted)", got)
	}
}

func TestRingBufferHandlerCapturesTraceContextAndDelegates(t *testing.T) {
	var sink bytes.Buffer
	buf := NewRingBuffer(10)
	logger := NewJSONLoggerWithCapture(&sink, Resource{ServiceName: "svc", ServiceInstanceID: "inst-1"}, LevelInfo, buf)

	sc := NewRootSpan()
	ctx := ContextWithSpan(context.Background(), sc)
	Log(ctx, logger, LevelNotice, "something happened", slog.String("key", "value"))

	if sink.Len() == 0 {
		t.Fatal("expected the real sink to still receive the record")
	}

	entries := buf.Recent(1)
	if len(entries) != 1 {
		t.Fatalf("expected one captured entry, got %d", len(entries))
	}
	got := entries[0]
	if got.Message != "something happened" {
		t.Fatalf("Message = %q, want %q", got.Message, "something happened")
	}
	if got.Level != "NOTICE" {
		t.Fatalf("Level = %q, want %q (Archipelago's own naming, not slog's)", got.Level, "NOTICE")
	}
	if got.TraceID != sc.TraceID.String() || got.SpanID != sc.SpanID.String() {
		t.Fatalf("captured trace context = (%s, %s), want (%s, %s)", got.TraceID, got.SpanID, sc.TraceID, sc.SpanID)
	}

	var attrs map[string]any
	if err := json.Unmarshal(got.Attrs, &attrs); err != nil {
		t.Fatalf("unmarshal captured Attrs: %v", err)
	}
	if attrs["key"] != "value" {
		t.Fatalf("captured Attrs missing call-site attribute, got %v", attrs)
	}
	if attrs["service.name"] != "svc" {
		t.Fatalf("captured Attrs missing resource attribute, got %v", attrs)
	}
}

func TestRingBufferHandlerNeverChangesRealSinkOutput(t *testing.T) {
	var withCapture, without bytes.Buffer
	buf := NewRingBuffer(10)
	resource := Resource{ServiceName: "svc", ServiceInstanceID: "inst-1"}

	captured := NewJSONLoggerWithCapture(&withCapture, resource, LevelInfo, buf)
	plain := NewJSONLogger(&without, resource, LevelInfo)

	captured.Info("hello")
	plain.Info("hello")

	// Compare with the "time" field stripped — each call legitimately
	// gets its own real timestamp; everything else must be identical.
	withoutTime := func(line []byte) map[string]any {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("unmarshal sink output: %v", err)
		}
		delete(m, "time")
		return m
	}
	got, want := withoutTime(withCapture.Bytes()), withoutTime(without.Bytes())
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("capture changed the real sink's output:\nwith capture: %s\nwithout:      %s", gotJSON, wantJSON)
	}
}
