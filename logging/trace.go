package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
)

// TraceID and SpanID use the W3C Trace Context wire format: a 128-bit
// trace ID and a 64-bit span ID, both fixed-width hex. Adopting the
// standard format (rather than a free-form string) is what makes a
// trace emitted here mergeable with traces from anything else already
// speaking it — Jaeger, Tempo, Zipkin, any OTel-compatible backend.
type TraceID [16]byte
type SpanID [8]byte

var (
	zeroTraceID TraceID
	zeroSpanID  SpanID
)

// IsZero reports whether this is the zero value — never a real,
// generated ID.
func (t TraceID) IsZero() bool { return t == zeroTraceID }
func (s SpanID) IsZero() bool  { return s == zeroSpanID }

func (t TraceID) String() string { return hex.EncodeToString(t[:]) }
func (s SpanID) String() string  { return hex.EncodeToString(s[:]) }

// NewTraceID and NewSpanID generate random IDs using crypto/rand, as the
// W3C spec requires (IDs must be unpredictable, not just unique).
func NewTraceID() TraceID {
	var id TraceID
	// crypto/rand.Read on the standard reader never returns an error
	// in practice; a zero-value ID on the extremely unlikely failure
	// path is a safe degradation (IsZero catches it) rather than a
	// panic in logging code.
	_, _ = rand.Read(id[:])
	return id
}

func NewSpanID() SpanID {
	var id SpanID
	_, _ = rand.Read(id[:])
	return id
}

// ParseTraceID and ParseSpanID decode the hex wire format back into an
// ID, for reading trace context out of an incoming Transit envelope.
func ParseTraceID(s string) (TraceID, error) {
	var id TraceID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, err
	}
	if len(b) != len(id) {
		return id, errors.New("logging: trace ID must be 16 bytes (32 hex chars)")
	}
	copy(id[:], b)
	return id, nil
}

func ParseSpanID(s string) (SpanID, error) {
	var id SpanID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, err
	}
	if len(b) != len(id) {
		return id, errors.New("logging: span ID must be 8 bytes (16 hex chars)")
	}
	copy(id[:], b)
	return id, nil
}

// SpanContext identifies which logical operation a log line belongs to
// (the trace) and which unit of work within it (the span) — the
// standard-terminology replacement for Lighthouse's own "narrative" and
// "parent ID" vocabulary.
type SpanContext struct {
	TraceID      TraceID
	SpanID       SpanID
	ParentSpanID SpanID // zero value means this is the trace's root span
}

// NewRootSpan starts a new trace with a new root span.
func NewRootSpan() SpanContext {
	return SpanContext{TraceID: NewTraceID(), SpanID: NewSpanID()}
}

// NewChildSpan starts a new span within the same trace as parent, with
// parent's span recorded as its parent span.
func NewChildSpan(parent SpanContext) SpanContext {
	return SpanContext{
		TraceID:      parent.TraceID,
		SpanID:       NewSpanID(),
		ParentSpanID: parent.SpanID,
	}
}

// Attrs renders the SpanContext as slog attributes using OpenTelemetry's
// conventional attribute keys.
func (c SpanContext) Attrs() []slog.Attr {
	attrs := []slog.Attr{
		slog.String("trace_id", c.TraceID.String()),
		slog.String("span_id", c.SpanID.String()),
	}
	if !c.ParentSpanID.IsZero() {
		attrs = append(attrs, slog.String("parent_span_id", c.ParentSpanID.String()))
	}
	return attrs
}

// SpanLink records a causal relationship to a span that isn't a
// parent/child of the current one — the standard OTel term for what
// Lighthouse called a "related ID" (batch fan-out, queue handoffs,
// anything that crosses a trace boundary).
type SpanLink struct {
	TraceID TraceID
	SpanID  SpanID
}

// SpanLinksAttr renders a set of links as a single slog attribute.
func SpanLinksAttr(links []SpanLink) slog.Attr {
	values := make([]string, len(links))
	for i, l := range links {
		values[i] = l.TraceID.String() + ":" + l.SpanID.String()
	}
	return slog.Any("span_links", values)
}

type spanContextKey struct{}

// ContextWithSpan attaches a SpanContext to ctx, for propagation down a
// call chain and across a Transit envelope's trace-context field.
func ContextWithSpan(ctx context.Context, sc SpanContext) context.Context {
	return context.WithValue(ctx, spanContextKey{}, sc)
}

// SpanFromContext retrieves the SpanContext attached by ContextWithSpan,
// if any.
func SpanFromContext(ctx context.Context) (SpanContext, bool) {
	sc, ok := ctx.Value(spanContextKey{}).(SpanContext)
	return sc, ok
}
