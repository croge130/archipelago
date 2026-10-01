package typeconstraints

import (
	"fmt"
	"regexp"

	"github.com/croge130/archipelago/typedvalue"
)

// Set bundles the constraints a value may need beyond what completes
// its type. Every field is optional; a zero Set imposes no
// restriction at all.
type Set struct {
	Min       *float64 // inclusive lower bound; int/float storage only
	Max       *float64 // inclusive upper bound; int/float storage only
	MinLength *int     // inclusive lower bound on rune count; string storage only
	MaxLength *int     // inclusive upper bound on rune count; string storage only
	Pattern   string   // regular expression; string storage only
}

// Validate checks that Set is coherent with def's Storage type —
// Min/Max require int or float; MinLength/MaxLength/Pattern require
// string. Rejected here, at registration, rather than silently
// accepted and later ignored.
func (s Set) Validate(def typedvalue.Definition) error {
	if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
		return fmt.Errorf("typeconstraints: Min must be <= Max")
	}
	numeric := def.Storage == typedvalue.StorageInt || def.Storage == typedvalue.StorageFloat
	if (s.Min != nil || s.Max != nil) && !numeric {
		return fmt.Errorf("typeconstraints: Min/Max require an int or float storage type, got %q", def.Storage)
	}
	stringy := def.Storage == typedvalue.StorageString
	if (s.MinLength != nil || s.MaxLength != nil || s.Pattern != "") && !stringy {
		return fmt.Errorf("typeconstraints: MinLength/MaxLength/Pattern require string storage, got %q", def.Storage)
	}
	if s.MinLength != nil && s.MaxLength != nil && *s.MinLength > *s.MaxLength {
		return fmt.Errorf("typeconstraints: MinLength must be <= MaxLength")
	}
	if s.Pattern != "" {
		if _, err := regexp.Compile(s.Pattern); err != nil {
			return fmt.Errorf("typeconstraints: invalid Pattern: %w", err)
		}
	}
	return nil
}

// Check enforces Set against an already-normalized value (the result
// of typedvalue.NormalizeValue) — it never normalizes itself, so a
// caller controls exactly once, in one place, how the raw value
// became this value.
//
// Unlike Lighthouse's own validateValueRange, a constraint whose type
// doesn't match the value's actual type is an error here, not a
// silent skip — Min/Max declared against a value that turns out not
// to be numeric fails loudly rather than passing because the range
// check gave up and returned nil.
func (s Set) Check(value any) error {
	if s.Min != nil || s.Max != nil {
		numeric, ok := asFloat(value)
		if !ok {
			return fmt.Errorf("typeconstraints: Min/Max constraint set but value %T is not numeric", value)
		}
		if s.Min != nil && numeric < *s.Min {
			return fmt.Errorf("typeconstraints: value %v is below the minimum %v", numeric, *s.Min)
		}
		if s.Max != nil && numeric > *s.Max {
			return fmt.Errorf("typeconstraints: value %v is above the maximum %v", numeric, *s.Max)
		}
	}
	if s.MinLength != nil || s.MaxLength != nil || s.Pattern != "" {
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("typeconstraints: length/pattern constraint set but value %T is not a string", value)
		}
		length := len([]rune(str))
		if s.MinLength != nil && length < *s.MinLength {
			return fmt.Errorf("typeconstraints: value is shorter than the minimum length %d", *s.MinLength)
		}
		if s.MaxLength != nil && length > *s.MaxLength {
			return fmt.Errorf("typeconstraints: value is longer than the maximum length %d", *s.MaxLength)
		}
		if s.Pattern != "" {
			re, err := regexp.Compile(s.Pattern)
			if err != nil {
				return fmt.Errorf("typeconstraints: invalid Pattern: %w", err)
			}
			if !re.MatchString(str) {
				return fmt.Errorf("typeconstraints: value does not match pattern %q", s.Pattern)
			}
		}
	}
	return nil
}

// Clamp coerces a numeric value into [Min, Max] rather than rejecting
// it outside that range — the shape Lighthouse's Clamped/ClampedBy
// resolution-detail fields describe: a narrower scope's override gets
// silently bounded by a wider scope's clamp, and the caller is told
// that happened rather than just handed a quietly different number.
func (s Set) Clamp(value any) (clamped any, wasClamped bool, err error) {
	numeric, ok := asFloat(value)
	if !ok {
		return value, false, fmt.Errorf("typeconstraints: Clamp requires a numeric value, got %T", value)
	}
	original := numeric
	if s.Min != nil && numeric < *s.Min {
		numeric = *s.Min
	}
	if s.Max != nil && numeric > *s.Max {
		numeric = *s.Max
	}
	if numeric == original {
		return value, false, nil
	}
	if _, isInt := value.(int64); isInt {
		return int64(numeric), true, nil
	}
	return numeric, true, nil
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}
