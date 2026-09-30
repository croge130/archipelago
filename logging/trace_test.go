package logging

import (
	"context"
	"testing"
)

func TestTraceIDRoundTrip(t *testing.T) {
	id := NewTraceID()
	if id.IsZero() {
		t.Fatal("NewTraceID returned the zero value")
	}
	parsed, err := ParseTraceID(id.String())
	if err != nil {
		t.Fatalf("ParseTraceID: %v", err)
	}
	if parsed != id {
		t.Errorf("round trip mismatch: got %v, want %v", parsed, id)
	}
	if len(id.String()) != 32 {
		t.Errorf("trace ID string length = %d, want 32 (128-bit hex)", len(id.String()))
	}
}

func TestSpanIDRoundTrip(t *testing.T) {
	id := NewSpanID()
	if id.IsZero() {
		t.Fatal("NewSpanID returned the zero value")
	}
	parsed, err := ParseSpanID(id.String())
	if err != nil {
		t.Fatalf("ParseSpanID: %v", err)
	}
	if parsed != id {
		t.Errorf("round trip mismatch: got %v, want %v", parsed, id)
	}
	if len(id.String()) != 16 {
		t.Errorf("span ID string length = %d, want 16 (64-bit hex)", len(id.String()))
	}
}

func TestParseTraceIDRejectsWrongLength(t *testing.T) {
	if _, err := ParseTraceID(NewSpanID().String()); err == nil {
		t.Error("ParseTraceID accepted a span-ID-length string")
	}
}

func TestParseSpanIDRejectsBadHex(t *testing.T) {
	if _, err := ParseSpanID("not-hex-at-all!"); err == nil {
		t.Error("ParseSpanID accepted invalid hex")
	}
}

func TestNewChildSpanSharesTraceNotSpan(t *testing.T) {
	root := NewRootSpan()
	if !root.ParentSpanID.IsZero() {
		t.Error("root span should have no parent")
	}
	child := NewChildSpan(root)
	if child.TraceID != root.TraceID {
		t.Error("child span should share the root's trace ID")
	}
	if child.SpanID == root.SpanID {
		t.Error("child span should have its own span ID")
	}
	if child.ParentSpanID != root.SpanID {
		t.Error("child span's parent should be the root's span ID")
	}
}

func TestSpanContextAttrsOmitsParentWhenRoot(t *testing.T) {
	root := NewRootSpan()
	attrs := root.Attrs()
	for _, a := range attrs {
		if a.Key == "parent_span_id" {
			t.Error("root span's Attrs included a parent_span_id")
		}
	}
	if len(attrs) != 2 {
		t.Errorf("root span should have exactly trace_id + span_id, got %d attrs", len(attrs))
	}
}

func TestSpanContextAttrsIncludesParentForChild(t *testing.T) {
	root := NewRootSpan()
	child := NewChildSpan(root)
	found := false
	for _, a := range child.Attrs() {
		if a.Key == "parent_span_id" {
			found = true
			if a.Value.String() != root.SpanID.String() {
				t.Errorf("parent_span_id = %s, want %s", a.Value.String(), root.SpanID.String())
			}
		}
	}
	if !found {
		t.Error("child span's Attrs missing parent_span_id")
	}
}

func TestContextSpanRoundTrip(t *testing.T) {
	sc := NewRootSpan()
	ctx := ContextWithSpan(context.Background(), sc)
	got, ok := SpanFromContext(ctx)
	if !ok {
		t.Fatal("SpanFromContext found nothing")
	}
	if got != sc {
		t.Errorf("SpanFromContext = %v, want %v", got, sc)
	}
}

func TestSpanFromContextMissing(t *testing.T) {
	if _, ok := SpanFromContext(context.Background()); ok {
		t.Error("SpanFromContext reported a span on a bare context")
	}
}

func TestSpanLinksAttr(t *testing.T) {
	a := NewTraceID()
	b := NewSpanID()
	attr := SpanLinksAttr([]SpanLink{{TraceID: a, SpanID: b}})
	if attr.Key != "span_links" {
		t.Errorf("SpanLinksAttr key = %q, want span_links", attr.Key)
	}
}
