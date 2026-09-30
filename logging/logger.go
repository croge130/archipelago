package logging

import (
	"context"
	"io"
	"log/slog"
)

// NewTextLogger returns a Logger writing human-readable text lines to w,
// tagged with resource, at or above minLevel. Every record it produces
// carries resource's attributes and, when the logging call's context
// carries one (via ContextWithSpan), the current span's trace context.
func NewTextLogger(w io.Writer, resource Resource, minLevel slog.Level) *slog.Logger {
	return newLogger(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       minLevel,
		ReplaceAttr: ReplaceAttr,
	}), resource)
}

// NewJSONLogger is NewTextLogger's structured-output counterpart, for
// sinks that want to parse log lines rather than read them directly.
func NewJSONLogger(w io.Writer, resource Resource, minLevel slog.Level) *slog.Logger {
	return newLogger(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       minLevel,
		ReplaceAttr: ReplaceAttr,
	}), resource)
}

func newLogger(h slog.Handler, resource Resource) *slog.Logger {
	return slog.New(FallbackHandler{Handler: h}).With(resource.Attrs()...)
}

// Log emits a record at the given level, automatically attaching the
// SpanContext carried on ctx (if any) ahead of the caller's own attrs.
// Prefer this over calling logger.Log/LogAttrs directly so trace
// correlation isn't something every call site has to remember.
func Log(ctx context.Context, logger *slog.Logger, level slog.Level, msg string, attrs ...slog.Attr) {
	if sc, ok := SpanFromContext(ctx); ok {
		attrs = append(sc.Attrs(), attrs...) //nolint:gocritic // deliberate attribute ordering
	}
	logger.LogAttrs(ctx, level, msg, attrs...)
}
