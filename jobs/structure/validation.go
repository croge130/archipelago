package structure

import (
	"fmt"
	"strings"
	"unicode"
)

// Bounds on the free-form parts of a job. Stated here rather than left
// to storage so a job is rejected before it is written, and so the
// numbers are visible in one place. They are working defaults (20-jobs-
// model.md leaves exact limits open), not a protocol.
const (
	MaxParamsBytes         = 64 * 1024
	MaxResultBytes         = 64 * 1024
	MaxErrorRunes          = 1024
	MaxIdempotencyKeyRunes = 128
	MaxKeyRunes            = 128
)

// validateDottedKey enforces the shape every key in this package shares
// (TaskKey, QueueKey): lowercase, digits, '.', '_', '-' only, no empty
// segments — the same rule as vitals' keys.
func validateDottedKey(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > MaxKeyRunes {
		return fmt.Errorf("%s must be at most %d characters", field, MaxKeyRunes)
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return fmt.Errorf("%s must be a dotted key without empty segments", field)
	}
	for _, r := range value {
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("%s contains invalid character %q", field, r)
	}
	return nil
}

func ValidateTaskKey(v string) error  { return validateDottedKey("task_key", v) }
func ValidateQueueKey(v string) error { return validateDottedKey("queue_key", v) }

// validateParamName is stricter than a dotted key: a parameter is a
// flat name, not a path.
func validateParamName(v string) error {
	if v == "" {
		return fmt.Errorf("param name is required")
	}
	if len(v) > 64 {
		return fmt.Errorf("param name %q is longer than 64 characters", v)
	}
	for _, r := range v {
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return fmt.Errorf("param name %q contains invalid character %q", v, r)
	}
	return nil
}

// TruncateError bounds a free-form error string to MaxErrorRunes,
// respecting rune boundaries.
func TruncateError(s string) string {
	runes := []rune(s)
	if len(runes) <= MaxErrorRunes {
		return s
	}
	return string(runes[:MaxErrorRunes])
}
