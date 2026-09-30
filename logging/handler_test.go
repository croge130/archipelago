package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// failingHandler always fails, to exercise FallbackHandler's
// never-recurse-into-logging guard.
type failingHandler struct{}

func (failingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (failingHandler) Handle(context.Context, slog.Record) error {
	return errors.New("sink is down")
}
func (h failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h failingHandler) WithGroup(string) slog.Handler      { return h }

func TestFallbackHandlerSwallowsError(t *testing.T) {
	var fallback bytes.Buffer
	h := FallbackHandler{Handler: failingHandler{}, Fallback: &fallback}
	logger := slog.New(h)

	logger.Info("this will fail to write")

	if fallback.Len() == 0 {
		t.Fatal("FallbackHandler wrote nothing to the fallback writer")
	}
	out := fallback.String()
	if !strings.Contains(out, "sink is down") {
		t.Errorf("fallback output missing underlying error, got: %s", out)
	}
	if !strings.Contains(out, "this will fail to write") {
		t.Errorf("fallback output missing dropped message, got: %s", out)
	}
}

func TestFallbackHandlerDefaultsToStderrWithoutPanicking(t *testing.T) {
	// Fallback left nil: exercise the os.Stderr default path. This
	// can't assert on stderr's contents without capturing the process
	// fd, but it must not panic or error.
	h := FallbackHandler{Handler: failingHandler{}}
	logger := slog.New(h)
	logger.Info("still must not panic")
}

func TestFallbackHandlerPassesThroughOnSuccess(t *testing.T) {
	var buf bytes.Buffer
	h := FallbackHandler{Handler: slog.NewTextHandler(&buf, nil)}
	logger := slog.New(h)
	logger.Info("all good")
	if !strings.Contains(buf.String(), "all good") {
		t.Errorf("expected successful write to reach the wrapped handler, got: %s", buf.String())
	}
}

func TestFallbackHandlerWithAttrsPreservesFallback(t *testing.T) {
	var fallback bytes.Buffer
	h := FallbackHandler{Handler: failingHandler{}, Fallback: &fallback}
	withAttrs := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).(FallbackHandler)
	if withAttrs.Fallback != &fallback {
		t.Error("WithAttrs lost the Fallback writer")
	}
}

func TestFallbackHandlerWithGroupPreservesFallback(t *testing.T) {
	var fallback bytes.Buffer
	h := FallbackHandler{Handler: failingHandler{}, Fallback: &fallback}
	withGroup := h.WithGroup("g").(FallbackHandler)
	if withGroup.Fallback != &fallback {
		t.Error("WithGroup lost the Fallback writer")
	}
}
