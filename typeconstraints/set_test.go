package typeconstraints

import (
	"testing"

	"github.com/croge130/archipelago/typedvalue"
)

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestSetValidateRejectsMinMaxOnNonNumeric(t *testing.T) {
	s := Set{Min: floatPtr(0)}
	if err := s.Validate(typedvalue.String("")); err == nil {
		t.Fatal("expected an error for Min on a string definition")
	}
	if err := s.Validate(typedvalue.Int("second", "")); err != nil {
		t.Fatalf("expected Min on an int definition to validate, got: %v", err)
	}
}

func TestSetValidateRejectsLengthPatternOnNonString(t *testing.T) {
	s := Set{MaxLength: intPtr(10)}
	if err := s.Validate(typedvalue.Int("second", "")); err == nil {
		t.Fatal("expected an error for MaxLength on an int definition")
	}
	if err := s.Validate(typedvalue.String("")); err != nil {
		t.Fatalf("expected MaxLength on a string definition to validate, got: %v", err)
	}
}

func TestSetValidateRejectsMinGreaterThanMax(t *testing.T) {
	s := Set{Min: floatPtr(10), Max: floatPtr(5)}
	if err := s.Validate(typedvalue.Int("second", "")); err == nil {
		t.Fatal("expected an error when Min > Max")
	}
}

func TestSetValidateRejectsInvalidPattern(t *testing.T) {
	s := Set{Pattern: "(unterminated"}
	if err := s.Validate(typedvalue.String("")); err == nil {
		t.Fatal("expected an error for an invalid regular expression")
	}
}

func TestSetCheckRange(t *testing.T) {
	s := Set{Min: floatPtr(1), Max: floatPtr(10)}
	if err := s.Check(int64(5)); err != nil {
		t.Fatalf("expected 5 to satisfy [1,10], got: %v", err)
	}
	if err := s.Check(int64(0)); err == nil {
		t.Fatal("expected 0 to fail the minimum")
	}
	if err := s.Check(int64(11)); err == nil {
		t.Fatal("expected 11 to fail the maximum")
	}
}

func TestSetCheckRangeRejectsNonNumericInsteadOfSkipping(t *testing.T) {
	// The exact bug the design doc calls out in Lighthouse's own
	// validateValueRange: a range constraint against a non-numeric
	// value must fail loudly, not silently pass.
	s := Set{Min: floatPtr(1)}
	if err := s.Check("not a number"); err == nil {
		t.Fatal("expected an error, not a silent pass, for a non-numeric value under a Min constraint")
	}
}

func TestSetCheckLengthAndPattern(t *testing.T) {
	s := Set{MinLength: intPtr(3), MaxLength: intPtr(5), Pattern: "^[a-z]+$"}
	if err := s.Check("abcd"); err != nil {
		t.Fatalf("expected abcd to satisfy the constraint, got: %v", err)
	}
	if err := s.Check("ab"); err == nil {
		t.Fatal("expected ab to fail MinLength")
	}
	if err := s.Check("abcdef"); err == nil {
		t.Fatal("expected abcdef to fail MaxLength")
	}
	if err := s.Check("ABCD"); err == nil {
		t.Fatal("expected ABCD to fail the lowercase-only pattern")
	}
}

func TestSetClampCoercesInsteadOfRejecting(t *testing.T) {
	s := Set{Min: floatPtr(0), Max: floatPtr(100)}
	clamped, wasClamped, err := s.Clamp(int64(5000))
	if err != nil {
		t.Fatalf("Clamp: %v", err)
	}
	if !wasClamped {
		t.Fatal("expected 5000 to be reported as clamped")
	}
	if clamped != int64(100) {
		t.Fatalf("clamped = %v, want 100", clamped)
	}
}

func TestSetClampNoOpWhenWithinBounds(t *testing.T) {
	s := Set{Min: floatPtr(0), Max: floatPtr(100)}
	clamped, wasClamped, err := s.Clamp(int64(50))
	if err != nil {
		t.Fatalf("Clamp: %v", err)
	}
	if wasClamped {
		t.Fatal("expected a within-bounds value to not be reported as clamped")
	}
	if clamped != int64(50) {
		t.Fatalf("clamped = %v, want 50 unchanged", clamped)
	}
}

func TestSetClampPreservesFloatType(t *testing.T) {
	s := Set{Max: floatPtr(1.0)}
	clamped, wasClamped, err := s.Clamp(1.5)
	if err != nil {
		t.Fatalf("Clamp: %v", err)
	}
	if !wasClamped {
		t.Fatal("expected 1.5 to be clamped to the max")
	}
	if _, isFloat := clamped.(float64); !isFloat {
		t.Fatalf("expected the clamped result to stay a float64, got %T", clamped)
	}
}
