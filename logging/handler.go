package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// FallbackHandler wraps another slog.Handler and never lets a write
// failure propagate back into logging itself. If the wrapped Handler's
// Handle call fails, the failure is written directly to Fallback as a
// plain line and swallowed — it is never retried through this or any
// other Handler, and never returned as an error the caller has to
// decide whether to "log". This is the recursive-failure-loop guard
// called for in the project's logging conventions: a logging-system
// failure is reported to a non-logging fallback (stderr by default, for
// systemd/journald to capture), not back into the system that just
// failed.
type FallbackHandler struct {
	Handler slog.Handler
	// Fallback receives the plain failure line when Handler.Handle
	// errors. Defaults to os.Stderr when nil.
	Fallback io.Writer
}

func (h FallbackHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h FallbackHandler) Handle(ctx context.Context, r slog.Record) error {
	if err := h.Handler.Handle(ctx, r); err != nil {
		w := h.Fallback
		if w == nil {
			w = os.Stderr
		}
		fmt.Fprintf(w, "logging: sink write failed, dropping record: level=%s msg=%q err=%v\n",
			LevelString(r.Level), r.Message, err)
	}
	return nil
}

func (h FallbackHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return FallbackHandler{Handler: h.Handler.WithAttrs(attrs), Fallback: h.Fallback}
}

func (h FallbackHandler) WithGroup(name string) slog.Handler {
	return FallbackHandler{Handler: h.Handler.WithGroup(name), Fallback: h.Fallback}
}
