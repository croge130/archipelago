package typedvalue

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// NormalizeValue coerces value into the Go type Storage calls for
// (an int-like value into int64, a string into a trimmed string, ...).
// It does not check range/length/pattern rules — typeconstraints does
// that, against the normalized value this returns.
func NormalizeValue(def Definition, value any) (any, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	switch def.Storage {
	case StorageBool:
		return normalizeBool(value)
	case StorageInt:
		return normalizeInt(value)
	case StorageFloat:
		return normalizeFloat(value)
	case StorageString, StorageEnum:
		return normalizeString(value)
	case StorageJSON:
		return normalizeJSONValue(value)
	case StorageObject:
		return normalizeObject(def, value)
	default:
		return nil, fmt.Errorf("typedvalue: unknown storage type %q", def.Storage)
	}
}

// ValidateValue normalizes value and checks the one constraint that
// lives in this package rather than typeconstraints: enum membership,
// because an enum's AllowedValues completes what the type *is*, the
// same reasoning that keeps AllowedValues itself here.
func ValidateValue(def Definition, value any) error {
	normalized, err := NormalizeValue(def, value)
	if err != nil {
		return err
	}
	if def.Storage == StorageEnum && !allowedValueSet(def.AllowedValues)[normalized.(string)] {
		return fmt.Errorf("typedvalue: value %q is not among AllowedValues", normalized)
	}
	return nil
}

func normalizeBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return false, fmt.Errorf("typedvalue: invalid bool value")
		}
		return parsed, nil
	default:
		return false, fmt.Errorf("typedvalue: invalid bool value")
	}
}

func normalizeInt(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, fmt.Errorf("typedvalue: invalid int value")
		}
		return int64(v), nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("typedvalue: invalid int value")
		}
		return int64(v), nil
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("typedvalue: invalid int value")
		}
		return parsed, nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("typedvalue: invalid int value")
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("typedvalue: invalid int value")
	}
}

func normalizeFloat(value any) (float64, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("typedvalue: invalid float value")
		}
		return parsed, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("typedvalue: invalid float value")
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("typedvalue: invalid float value")
	}
}

func normalizeString(value any) (string, error) {
	v, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("typedvalue: invalid string value")
	}
	return strings.TrimSpace(v), nil
}

func normalizeJSONValue(value any) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("typedvalue: json value must not be null")
	}
	return value, nil
}

func normalizeObject(def Definition, value any) (map[string]any, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("typedvalue: invalid object value")
	}
	out := make(map[string]any, len(def.Fields))
	for name, field := range def.Fields {
		item, ok := raw[name]
		if !ok {
			return nil, fmt.Errorf("typedvalue: missing object field %q", name)
		}
		normalized, err := NormalizeValue(field, item)
		if err != nil {
			return nil, fmt.Errorf("typedvalue: field %q: %w", name, err)
		}
		out[name] = normalized
	}
	return out, nil
}
