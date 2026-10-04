package logging

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Entry is one captured record, exported shape for shipping elsewhere
// — see docs/architecture/16-trace-log-aggregation-model.md. Level is
// rendered via LevelString (not slog.Level's own JSON form), so an
// exported Entry always shows the same name the real sink already
// printed for it ("NOTICE", not slog.Level's own "INFO+2"-style
// fallback). Attrs flattens every attribute the record carried —
// Resource, any With()-chained attrs, and call-site attrs — into one
// JSON object, in that order, later attrs winning on key collision
// (the same last-write-wins a caller would see in a real text/json
// line's repeated keys).
type Entry struct {
	Time         time.Time
	Level        string
	Message      string
	TraceID      string // "" if the record carried none
	SpanID       string
	ParentSpanID string
	Attrs        json.RawMessage
}

// RingBuffer holds up to capacity Entries, oldest evicted first —
// bounded and lossy by design. It exists to answer "what just
// happened, recently" for export, never as a backup of everything
// ever logged; a caller that wants durability already has the real
// io.Writer sink sitting beside it.
type RingBuffer struct {
	mu       sync.Mutex
	entries  []Entry
	capacity int
	start    int // index of the oldest entry, once entries is full
	size     int
}

// NewRingBuffer returns a RingBuffer holding at most capacity
// Entries. capacity <= 0 is treated as 1 — a buffer that captures
// nothing isn't a valid "capture" buffer at all, and panicking over a
// caller's off-by-one is worse than silently rounding up here.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &RingBuffer{entries: make([]Entry, capacity), capacity: capacity}
}

func (b *RingBuffer) append(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size < b.capacity {
		b.entries[b.size] = e
		b.size++
		return
	}
	b.entries[b.start] = e
	b.start = (b.start + 1) % b.capacity
}

// Recent returns the most recent n Entries (or fewer, if the buffer
// doesn't hold that many yet), oldest first.
func (b *RingBuffer) Recent(n int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.size {
		n = b.size
	}
	if n <= 0 {
		return nil
	}
	out := make([]Entry, n)
	// The n most recent entries are the last n in logical (oldest-
	// first) order — start at size-n positions back from the newest.
	for i := 0; i < n; i++ {
		idx := (b.start + b.size - n + i) % b.capacity
		out[i] = b.entries[idx]
	}
	return out
}

// ByTraceID returns every currently-buffered Entry for traceID,
// oldest first. A trace whose earliest entries already rotated out
// of the buffer is only partially represented — the same bounded-
// lossy trade-off Recent has, not a bug specific to this lookup.
func (b *RingBuffer) ByTraceID(traceID string) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Entry
	for i := 0; i < b.size; i++ {
		idx := (b.start + i) % b.capacity // oldest-first order, same indexing Recent uses
		if b.entries[idx].TraceID == traceID {
			out = append(out, b.entries[idx])
		}
	}
	return out
}

// RingBufferHandler wraps a real slog.Handler, appending an Entry to
// Buffer for every record and then unconditionally delegating to
// Handler — the same wrapping shape FallbackHandler already uses.
// Capture never changes what reaches the real sink; it only also
// keeps a copy.
//
// Known limitation: WithGroup delegates to the inner handler for real
// output, but captured Entry.Attrs doesn't reflect group namespacing
// — nothing in this codebase groups attributes yet, so there's
// nothing to get wrong today, but a future caller that does should
// know the capture path doesn't model it.
type RingBufferHandler struct {
	Handler slog.Handler
	Buffer  *RingBuffer
	attrs   []slog.Attr
}

func (h RingBufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h RingBufferHandler) Handle(ctx context.Context, r slog.Record) error {
	all := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	all = append(all, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		all = append(all, a)
		return true
	})

	entry := Entry{Time: r.Time, Level: LevelString(r.Level), Message: r.Message}
	fields := make(map[string]any, len(all))
	for _, a := range all {
		v := a.Value.Resolve().Any()
		switch a.Key {
		case "trace_id":
			entry.TraceID, _ = v.(string)
		case "span_id":
			entry.SpanID, _ = v.(string)
		case "parent_span_id":
			entry.ParentSpanID, _ = v.(string)
		}
		fields[a.Key] = v
	}
	if encoded, err := json.Marshal(fields); err == nil {
		entry.Attrs = encoded
	}
	h.Buffer.append(entry)

	return h.Handler.Handle(ctx, r)
}

func (h RingBufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return RingBufferHandler{Handler: h.Handler.WithAttrs(attrs), Buffer: h.Buffer, attrs: merged}
}

func (h RingBufferHandler) WithGroup(name string) slog.Handler {
	return RingBufferHandler{Handler: h.Handler.WithGroup(name), Buffer: h.Buffer, attrs: h.attrs}
}

// NewTextLoggerWithCapture is NewTextLogger plus an opt-in capture
// buffer — additive to the real sink, never a replacement for it. A
// caller that doesn't need export keeps using NewTextLogger; this is
// a parallel constructor, not a behavior change to the existing one.
func NewTextLoggerWithCapture(w io.Writer, resource Resource, minLevel slog.Level, buf *RingBuffer) *slog.Logger {
	inner := slog.NewTextHandler(w, &slog.HandlerOptions{Level: minLevel, ReplaceAttr: ReplaceAttr})
	return newLogger(RingBufferHandler{Handler: inner, Buffer: buf}, resource)
}

// NewJSONLoggerWithCapture is NewJSONLogger's capture-buffer counterpart.
func NewJSONLoggerWithCapture(w io.Writer, resource Resource, minLevel slog.Level, buf *RingBuffer) *slog.Logger {
	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: minLevel, ReplaceAttr: ReplaceAttr})
	return newLogger(RingBufferHandler{Handler: inner, Buffer: buf}, resource)
}
