package typedvalue

import "testing"

func TestNormalizeValueBool(t *testing.T) {
	v, err := NormalizeValue(Bool(""), "true")
	if err != nil || v != true {
		t.Fatalf("NormalizeValue(bool, \"true\") = (%v, %v), want (true, nil)", v, err)
	}
}

func TestNormalizeValueInt(t *testing.T) {
	v, err := NormalizeValue(Int("second", ""), float64(42))
	if err != nil || v != int64(42) {
		t.Fatalf("NormalizeValue(int, 42.0) = (%v, %v), want (42, nil)", v, err)
	}
	if _, err := NormalizeValue(Int("second", ""), 42.5); err == nil {
		t.Fatal("expected an error normalizing a non-integral float into an int")
	}
}

func TestNormalizeValueFloat(t *testing.T) {
	v, err := NormalizeValue(Float("second", ""), "3.5")
	if err != nil || v != 3.5 {
		t.Fatalf("NormalizeValue(float, \"3.5\") = (%v, %v), want (3.5, nil)", v, err)
	}
}

func TestNormalizeValueStringTrims(t *testing.T) {
	v, err := NormalizeValue(String(""), "  hello  ")
	if err != nil || v != "hello" {
		t.Fatalf("NormalizeValue(string) = (%q, %v), want (hello, nil)", v, err)
	}
}

func TestValidateValueEnumMembership(t *testing.T) {
	def := Enum([]string{"active", "archived"}, "lifecycle state")
	if err := ValidateValue(def, "active"); err != nil {
		t.Fatalf("expected a member value to validate, got: %v", err)
	}
	if err := ValidateValue(def, "deleted"); err == nil {
		t.Fatal("expected a non-member value to fail validation")
	}
}

func TestNormalizeValueObjectRecurses(t *testing.T) {
	def := Object(map[string]Definition{
		"max_requests": Count("request", ""),
		"window":       DurationSeconds(""),
	}, "a rate limit config")

	v, err := NormalizeValue(def, map[string]any{
		"max_requests": 100,
		"window":       "60",
	})
	if err != nil {
		t.Fatalf("NormalizeValue: %v", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected a map[string]any result, got %T", v)
	}
	if obj["max_requests"] != int64(100) {
		t.Fatalf("max_requests = %v, want int64(100)", obj["max_requests"])
	}
	if obj["window"] != float64(60) {
		t.Fatalf("window = %v, want float64(60)", obj["window"])
	}
}

func TestNormalizeValueObjectRejectsMissingField(t *testing.T) {
	def := Object(map[string]Definition{
		"max_requests": Count("request", ""),
	}, "")
	if _, err := NormalizeValue(def, map[string]any{}); err == nil {
		t.Fatal("expected an error for a missing required object field")
	}
}

func TestNormalizeValueJSONRejectsNull(t *testing.T) {
	if _, err := NormalizeValue(JSON(""), nil); err == nil {
		t.Fatal("expected an error normalizing a null json value")
	}
}
