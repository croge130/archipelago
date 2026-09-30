package logging

import "log/slog"

// Severity levels, carried forward from the design docs unchanged. They
// don't map one-to-one onto slog's four built-in levels (Debug, Info,
// Warn, Error), so each is given its own slog.Level value, spaced to
// leave room between and around the built-ins rather than colliding
// with them.
const (
	LevelTrace    slog.Level = -12
	LevelDebug    slog.Level = slog.LevelDebug // -4
	LevelVerbose  slog.Level = -2
	LevelInfo     slog.Level = slog.LevelInfo // 0
	LevelNotice   slog.Level = 2
	LevelWarning  slog.Level = slog.LevelWarn  // 4
	LevelError    slog.Level = slog.LevelError // 8
	LevelCritical slog.Level = 10
	LevelFatal    slog.Level = 12
)

// LevelString renders a Level using the nine-level scale's own names.
// Any other value (a level this package didn't define) falls back to
// slog's own "INFO+2"-style formatting rather than guessing a name.
func LevelString(l slog.Level) string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelVerbose:
		return "VERBOSE"
	case LevelInfo:
		return "INFO"
	case LevelNotice:
		return "NOTICE"
	case LevelWarning:
		return "WARNING"
	case LevelError:
		return "ERROR"
	case LevelCritical:
		return "CRITICAL"
	case LevelFatal:
		return "FATAL"
	default:
		return l.String()
	}
}

// ReplaceAttr is a slog.HandlerOptions.ReplaceAttr function that renders
// the record's level using LevelString instead of slog's default
// formatting. Pass it to NewTextLogger/NewJSONLogger's HandlerOptions, or
// directly if constructing a slog.Handler by hand.
func ReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.LevelKey {
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(LevelString(lvl))
		}
	}
	return a
}
