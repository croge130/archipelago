package typeconstraints

import (
	"fmt"

	"github.com/croge130/archipelago/typedvalue"
)

// MergeMode decides both how a scoped override combines with a wider
// default and how several simultaneously-applicable overrides combine
// with each other (see docs/architecture/10-typedvalue-and-policy-model.md's
// resolution section) — the same operator serves both jobs, because
// the wider default is just one more value in the same merge.
type MergeMode string

const (
	MergeMinimum           MergeMode = "minimum"
	MergeMaximum           MergeMode = "maximum"
	MergeBooleanAnd        MergeMode = "boolean_and"
	MergeBooleanOr         MergeMode = "boolean_or"
	MergeOverride          MergeMode = "override"
	MergeEnumStrengthOrder MergeMode = "enum_strength_order"
	MergeObjectMerge       MergeMode = "object_merge"
)

func (m MergeMode) Valid() bool {
	switch m {
	case MergeMinimum, MergeMaximum, MergeBooleanAnd, MergeBooleanOr,
		MergeOverride, MergeEnumStrengthOrder, MergeObjectMerge:
		return true
	default:
		return false
	}
}

// Commutative reports whether merging is safe for any number of
// simultaneously-applicable values in any order. minimum/maximum/
// boolean_and/boolean_or/enum_strength_order are — the operation
// itself doesn't care about order or count. override and object_merge
// aren't: "ignore everything else, use this" and "later keys win"
// both depend on there being a defined "last," which is why Policy
// requires write-time exclusivity for these two instead of ranking
// multiple sources at read time.
func (m MergeMode) Commutative() bool {
	switch m {
	case MergeMinimum, MergeMaximum, MergeBooleanAnd, MergeBooleanOr, MergeEnumStrengthOrder:
		return true
	default:
		return false
	}
}

// ValidateForType checks merge_mode type-coherence — minimum/maximum
// require an ordered type, boolean_and/boolean_or require bool,
// object_merge requires a JSON-shaped value, enum_strength_order
// requires an enum. Lighthouse's own ValidateSecurityPolicyDefinition
// checked that MergeMode was a known value but never checked it
// against the value's actual type, so boolean_and on an int-typed
// definition validated clean there; it doesn't here.
func (m MergeMode) ValidateForType(def typedvalue.Definition) error {
	switch m {
	case MergeMinimum, MergeMaximum:
		if def.Storage != typedvalue.StorageInt && def.Storage != typedvalue.StorageFloat {
			return fmt.Errorf("typeconstraints: merge_mode %q requires an ordered (int/float) type, got %q", m, def.Storage)
		}
	case MergeBooleanAnd, MergeBooleanOr:
		if def.Storage != typedvalue.StorageBool {
			return fmt.Errorf("typeconstraints: merge_mode %q requires bool storage, got %q", m, def.Storage)
		}
	case MergeObjectMerge:
		if def.Storage != typedvalue.StorageObject && def.Storage != typedvalue.StorageJSON {
			return fmt.Errorf("typeconstraints: merge_mode %q requires object or json storage, got %q", m, def.Storage)
		}
	case MergeEnumStrengthOrder:
		if def.Storage != typedvalue.StorageEnum {
			return fmt.Errorf("typeconstraints: merge_mode %q requires enum storage, got %q", m, def.Storage)
		}
	case MergeOverride:
		// Valid for any storage type — "ignore everything else, use
		// this" has no type requirement of its own.
	default:
		return fmt.Errorf("typeconstraints: unknown merge_mode %q", m)
	}
	return nil
}

// Merge combines already-normalized values (typedvalue.NormalizeValue
// results) per mode. Callers order values least-specific first — a
// global default, if any, goes first — since override and
// object_merge both treat the last value as authoritative: override
// returns it outright, object_merge folds its keys in last so they
// win on collision. This is a fallback behavior, not a substitute for
// the write-time exclusivity Policy enforces for these two modes —
// Merge will still produce a deterministic answer if that invariant
// is ever violated, it just won't be a surprising one.
func Merge(mode MergeMode, def typedvalue.Definition, values ...any) (any, error) {
	if err := mode.ValidateForType(def); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("typeconstraints: merge: no values to merge")
	}

	switch mode {
	case MergeMinimum, MergeMaximum:
		return mergeNumeric(mode, values)
	case MergeBooleanAnd, MergeBooleanOr:
		return mergeBoolean(mode, values)
	case MergeOverride:
		return values[len(values)-1], nil
	case MergeObjectMerge:
		return mergeObjects(values)
	case MergeEnumStrengthOrder:
		return mergeEnumStrength(def, values)
	default:
		return nil, fmt.Errorf("typeconstraints: unknown merge_mode %q", mode)
	}
}

func mergeNumeric(mode MergeMode, values []any) (any, error) {
	best, ok := asFloat(values[0])
	if !ok {
		return nil, fmt.Errorf("typeconstraints: merge: value %T is not numeric", values[0])
	}
	bestIsInt := isInt64(values[0])
	for _, v := range values[1:] {
		n, ok := asFloat(v)
		if !ok {
			return nil, fmt.Errorf("typeconstraints: merge: value %T is not numeric", v)
		}
		if (mode == MergeMinimum && n < best) || (mode == MergeMaximum && n > best) {
			best = n
			bestIsInt = isInt64(v)
		}
	}
	if bestIsInt {
		return int64(best), nil
	}
	return best, nil
}

func isInt64(v any) bool {
	_, ok := v.(int64)
	return ok
}

func mergeBoolean(mode MergeMode, values []any) (any, error) {
	result, ok := values[0].(bool)
	if !ok {
		return nil, fmt.Errorf("typeconstraints: merge: value %T is not bool", values[0])
	}
	for _, v := range values[1:] {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("typeconstraints: merge: value %T is not bool", v)
		}
		if mode == MergeBooleanAnd {
			result = result && b
		} else {
			result = result || b
		}
	}
	return result, nil
}

func mergeObjects(values []any) (any, error) {
	out := make(map[string]any)
	for _, v := range values {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("typeconstraints: merge: value %T is not an object", v)
		}
		for k, item := range m {
			out[k] = item
		}
	}
	return out, nil
}

// mergeEnumStrength picks the strongest value among those given,
// where strength is each value's position in def.AllowedValues
// (index 0 weakest, last index strongest — a convention this package
// defines since neither Lighthouse nor the design doc pinned down
// enum_strength_order's actual direction). Strongest-wins follows the
// same "ambiguity resolves toward restriction" default this design
// uses elsewhere, on the reasoning that a strength-ordered enum is
// most often expressing a risk or strictness level where the more
// cautious value should win when sources disagree.
func mergeEnumStrength(def typedvalue.Definition, values []any) (any, error) {
	strength := make(map[string]int, len(def.AllowedValues))
	for i, v := range def.AllowedValues {
		strength[v] = i
	}
	bestIdx := -1
	var best string
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("typeconstraints: merge: value %T is not a string", v)
		}
		idx, known := strength[s]
		if !known {
			return nil, fmt.Errorf("typeconstraints: merge: value %q is not among AllowedValues", s)
		}
		if idx > bestIdx {
			bestIdx = idx
			best = s
		}
	}
	return best, nil
}
