package typeconstraints

import (
	"testing"

	"github.com/croge130/archipelago/typedvalue"
)

func TestMergeModeCommutative(t *testing.T) {
	commutative := []MergeMode{MergeMinimum, MergeMaximum, MergeBooleanAnd, MergeBooleanOr, MergeEnumStrengthOrder}
	for _, m := range commutative {
		if !m.Commutative() {
			t.Errorf("expected %q to be commutative", m)
		}
	}
	notCommutative := []MergeMode{MergeOverride, MergeObjectMerge}
	for _, m := range notCommutative {
		if m.Commutative() {
			t.Errorf("expected %q to not be commutative", m)
		}
	}
}

func TestValidateForTypeRejectsIncoherentPairing(t *testing.T) {
	// Lighthouse's own gap: boolean_and against an int-typed
	// definition validated clean. It must not here.
	if err := MergeBooleanAnd.ValidateForType(typedvalue.Int("second", "")); err == nil {
		t.Fatal("expected boolean_and against an int definition to fail")
	}
	if err := MergeMinimum.ValidateForType(typedvalue.Bool("")); err == nil {
		t.Fatal("expected minimum against a bool definition to fail")
	}
	if err := MergeMinimum.ValidateForType(typedvalue.Int("second", "")); err != nil {
		t.Fatalf("expected minimum against an int definition to pass, got: %v", err)
	}
}

func TestMergeMinimumMaximum(t *testing.T) {
	def := typedvalue.Int("second", "")
	got, err := Merge(MergeMinimum, def, int64(5), int64(2), int64(8))
	if err != nil || got != int64(2) {
		t.Fatalf("Merge(minimum) = (%v, %v), want (2, nil)", got, err)
	}
	got, err = Merge(MergeMaximum, def, int64(5), int64(2), int64(8))
	if err != nil || got != int64(8) {
		t.Fatalf("Merge(maximum) = (%v, %v), want (8, nil)", got, err)
	}
}

func TestMergeBooleanAndOr(t *testing.T) {
	def := typedvalue.Bool("")
	if got, err := Merge(MergeBooleanAnd, def, true, true, false); err != nil || got != false {
		t.Fatalf("Merge(boolean_and) = (%v, %v), want (false, nil)", got, err)
	}
	if got, err := Merge(MergeBooleanOr, def, false, false, true); err != nil || got != true {
		t.Fatalf("Merge(boolean_or) = (%v, %v), want (true, nil)", got, err)
	}
}

func TestMergeOverrideTakesLastValue(t *testing.T) {
	def := typedvalue.Int("second", "")
	// Caller convention: least-specific (global) first, most-specific
	// last — override returns the last value, consistent with "ignore
	// everything else, use this."
	got, err := Merge(MergeOverride, def, int64(10), int64(99))
	if err != nil || got != int64(99) {
		t.Fatalf("Merge(override) = (%v, %v), want (99, nil)", got, err)
	}
}

func TestMergeObjectMergeLaterKeysWin(t *testing.T) {
	def := typedvalue.JSON("")
	got, err := Merge(MergeObjectMerge, def,
		map[string]any{"a": 1, "b": 2},
		map[string]any{"b": 99, "c": 3},
	)
	if err != nil {
		t.Fatalf("Merge(object_merge): %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected a map[string]any result, got %T", got)
	}
	if m["a"] != 1 || m["b"] != 99 || m["c"] != 3 {
		t.Fatalf("merged = %+v, want a=1 b=99 (later wins) c=3", m)
	}
}

func TestMergeEnumStrengthOrderPicksStrongest(t *testing.T) {
	def := typedvalue.Enum([]string{"low", "medium", "high"}, "")
	got, err := Merge(MergeEnumStrengthOrder, def, "low", "high", "medium")
	if err != nil || got != "high" {
		t.Fatalf("Merge(enum_strength_order) = (%v, %v), want (high, nil)", got, err)
	}
}

func TestMergeEnumStrengthOrderRejectsUnknownValue(t *testing.T) {
	def := typedvalue.Enum([]string{"low", "medium", "high"}, "")
	if _, err := Merge(MergeEnumStrengthOrder, def, "low", "extreme"); err == nil {
		t.Fatal("expected an error for a value not among AllowedValues")
	}
}

func TestMergeRejectsEmptyValues(t *testing.T) {
	if _, err := Merge(MergeMinimum, typedvalue.Int("second", "")); err == nil {
		t.Fatal("expected an error merging zero values")
	}
}
