package structure

import "testing"

func TestRefValidateRequiresKindAndKey(t *testing.T) {
	if err := (Ref{}).Validate(); err == nil {
		t.Fatal("expected an error for an empty Ref")
	}
	if err := (Ref{Kind: "principal"}).Validate(); err == nil {
		t.Fatal("expected an error for a missing Key")
	}
	if err := (Ref{Kind: "principal", Key: "abc"}).Validate(); err != nil {
		t.Fatalf("expected a complete Ref to validate, got: %v", err)
	}
}

func TestRefIsZero(t *testing.T) {
	zero := Ref{}
	if !zero.IsZero() {
		t.Fatal("expected a zero-value Ref to report IsZero")
	}
	populated := Ref{Kind: "principal", Key: "abc"}
	if populated.IsZero() {
		t.Fatal("expected a populated Ref to not report IsZero")
	}
}
