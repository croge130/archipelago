package logging

import (
	"log/slog"
	"testing"
)

func TestLevelString(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{LevelTrace, "TRACE"},
		{LevelDebug, "DEBUG"},
		{LevelVerbose, "VERBOSE"},
		{LevelInfo, "INFO"},
		{LevelNotice, "NOTICE"},
		{LevelWarning, "WARNING"},
		{LevelError, "ERROR"},
		{LevelCritical, "CRITICAL"},
		{LevelFatal, "FATAL"},
	}
	for _, c := range cases {
		if got := LevelString(c.level); got != c.want {
			t.Errorf("LevelString(%v) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestLevelStringUnknownFallsBackToSlog(t *testing.T) {
	unknown := slog.Level(99)
	got := LevelString(unknown)
	want := unknown.String()
	if got != want {
		t.Errorf("LevelString(unknown) = %q, want slog's own formatting %q", got, want)
	}
}

func TestReplaceAttrRewritesLevel(t *testing.T) {
	a := slog.Attr{Key: slog.LevelKey, Value: slog.AnyValue(LevelNotice)}
	got := ReplaceAttr(nil, a)
	if got.Value.Kind() != slog.KindString || got.Value.String() != "NOTICE" {
		t.Errorf("ReplaceAttr level = %v, want string NOTICE", got.Value)
	}
}

func TestReplaceAttrLeavesOtherKeysAlone(t *testing.T) {
	a := slog.String("some.key", "value")
	got := ReplaceAttr(nil, a)
	if got.Key != a.Key || got.Value.String() != a.Value.String() {
		t.Errorf("ReplaceAttr changed a non-level attr: got %v, want %v", got, a)
	}
}

func TestReplaceAttrIgnoresLevelKeyInsideGroups(t *testing.T) {
	// The level key inside a nested group isn't the record's own level;
	// ReplaceAttr must not rewrite it.
	a := slog.Attr{Key: slog.LevelKey, Value: slog.AnyValue(LevelNotice)}
	got := ReplaceAttr([]string{"somegroup"}, a)
	if got.Value.Kind() != a.Value.Kind() {
		t.Errorf("ReplaceAttr rewrote a level-keyed attr inside a group: got kind %v, want unchanged kind %v", got.Value.Kind(), a.Value.Kind())
	}
}
