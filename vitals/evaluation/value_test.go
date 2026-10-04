package evaluation

import (
	"encoding/json"
	"testing"

	"github.com/croge130/archipelago/typedvalue"
	"github.com/croge130/archipelago/vitals/structure"
)

func TestNormalizeAndValidateValuePassesThroughWhenNoValueMetadata(t *testing.T) {
	def := structure.Definition{}
	raw := json.RawMessage(`"anything at all"`)
	got, err := NormalizeAndValidateValue(def, raw)
	if err != nil {
		t.Fatalf("NormalizeAndValidateValue: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("expected passthrough, got %s", got)
	}
}

func TestNormalizeAndValidateValuePassesThroughWhenValueEmpty(t *testing.T) {
	vm := typedvalue.Count("item", "queue depth")
	def := structure.Definition{ValueMetadata: &vm}
	got, err := NormalizeAndValidateValue(def, nil)
	if err != nil {
		t.Fatalf("NormalizeAndValidateValue: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil passthrough, got %s", got)
	}
}

func TestNormalizeAndValidateValueCoercesIntFromString(t *testing.T) {
	vm := typedvalue.Count("item", "queue depth")
	def := structure.Definition{ValueMetadata: &vm}
	got, err := NormalizeAndValidateValue(def, json.RawMessage(`"42"`))
	if err != nil {
		t.Fatalf("NormalizeAndValidateValue: %v", err)
	}
	if string(got) != "42" {
		t.Fatalf("expected normalized int 42, got %s", got)
	}
}

func TestNormalizeAndValidateValueRejectsWrongShape(t *testing.T) {
	vm := typedvalue.Count("item", "queue depth")
	def := structure.Definition{ValueMetadata: &vm}
	if _, err := NormalizeAndValidateValue(def, json.RawMessage(`"not a number"`)); err == nil {
		t.Fatal("expected an error for a non-numeric value against an Int definition")
	}
}

func TestNormalizeAndValidateValueEnumMembership(t *testing.T) {
	vm := typedvalue.Enum([]string{"starting", "running", "stopped"}, "job phase")
	def := structure.Definition{ValueMetadata: &vm}

	if _, err := NormalizeAndValidateValue(def, json.RawMessage(`"running"`)); err != nil {
		t.Fatalf("expected a valid enum member to pass, got %v", err)
	}
	if _, err := NormalizeAndValidateValue(def, json.RawMessage(`"not-a-phase"`)); err == nil {
		t.Fatal("expected an error for a value outside AllowedValues")
	}
}

func TestNormalizeAndValidateValueObjectRecurses(t *testing.T) {
	vm := typedvalue.Object(map[string]typedvalue.Definition{
		"used":  typedvalue.SizeBytes("used bytes"),
		"total": typedvalue.SizeBytes("total bytes"),
	}, "storage usage")
	def := structure.Definition{ValueMetadata: &vm}

	if _, err := NormalizeAndValidateValue(def, json.RawMessage(`{"used": 10, "total": 100}`)); err != nil {
		t.Fatalf("expected a complete object to pass, got %v", err)
	}
	if _, err := NormalizeAndValidateValue(def, json.RawMessage(`{"used": 10}`)); err == nil {
		t.Fatal("expected an error for a missing object field")
	}
}

func TestNormalizeAndValidateValueRejectsInvalidJSON(t *testing.T) {
	vm := typedvalue.Int("items", "queue depth")
	def := structure.Definition{ValueMetadata: &vm}
	if _, err := NormalizeAndValidateValue(def, json.RawMessage(`{not json`)); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
