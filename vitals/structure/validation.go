package structure

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxSummaryRunes         = 280
	MaxReasonCodeRunes      = 128
	MaxCategoryPathDepth    = 8
	MaxCategorySegmentRunes = 64
)

// validateDottedKey enforces the shape every *Key field shares
// (DefinitionKey, InstanceKey, GroupKey, MemberKey): lowercase,
// digits, '.', '_', '-' only, no empty segments. Ported from
// pkg/vitals/validation.go unchanged — this has nothing to do with
// realms.
func validateDottedKey(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
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

func ValidateDefinitionKey(value string) error { return validateDottedKey("definition_key", value) }
func ValidateInstanceKey(value string) error   { return validateDottedKey("instance_key", value) }
func ValidateGroupKey(value string) error      { return validateDottedKey("group_key", value) }
func ValidateMemberKey(value string) error     { return validateDottedKey("member_key", value) }

func ValidateReasonCode(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > MaxReasonCodeRunes {
		return fmt.Errorf("reason_code must be at most %d characters", MaxReasonCodeRunes)
	}
	return validateDottedKey("reason_code", value)
}

func ValidateSummary(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > MaxSummaryRunes {
		return fmt.Errorf("summary must be at most %d characters", MaxSummaryRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("summary must not contain control characters")
		}
	}
	return nil
}

func ValidateCategoryPath(path []string) error {
	if len(path) > MaxCategoryPathDepth {
		return fmt.Errorf("category_path must contain at most %d segments", MaxCategoryPathDepth)
	}
	for _, segment := range path {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return fmt.Errorf("category_path must not contain empty segments")
		}
		if utf8.RuneCountInString(segment) > MaxCategorySegmentRunes {
			return fmt.Errorf("category_path segment must be at most %d characters", MaxCategorySegmentRunes)
		}
	}
	return nil
}

// validateStates checks every entry against State.Valid and rejects
// duplicates — used for AllowedStates/DefaultExpectedStates/
// ExpectedStates, all of which are sets, not sequences.
func validateStates(field string, states []State) error {
	seen := map[State]struct{}{}
	for _, s := range states {
		if !s.Valid() {
			return fmt.Errorf("%s contains unknown state %q", field, s)
		}
		if _, ok := seen[s]; ok {
			return fmt.Errorf("%s contains duplicate state %q", field, s)
		}
		seen[s] = struct{}{}
	}
	return nil
}

// StateIn reports whether state appears in states — Evaluation's
// "expected = reading.state in instance.expected_states" rule, and
// Validate's own duplicate/membership checks, share this one helper.
func StateIn(state State, states []State) bool {
	for _, s := range states {
		if s == state {
			return true
		}
	}
	return false
}
