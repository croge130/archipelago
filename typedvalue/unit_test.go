package typedvalue

import "testing"

func TestCoreUnitsRegistered(t *testing.T) {
	gram, ok := LookupUnit("gram")
	if !ok {
		t.Fatal("expected gram to be registered")
	}
	if !gram.Dim.Equal(Dimension{DimMass: 1}) {
		t.Fatalf("gram.Dim = %v, want mass^1", gram.Dim)
	}

	byte_, ok := LookupUnit("byte")
	if !ok {
		t.Fatal("expected byte to be registered")
	}
	if byte_.Factor != 8 {
		t.Fatalf("byte.Factor = %v, want 8 (bits per byte)", byte_.Factor)
	}
}

func TestKilogramIsNotARegisteredUnit(t *testing.T) {
	if _, ok := LookupUnit("kilogram"); ok {
		t.Fatal("kilogram should not be a registry entry — it's {Unit: gram, Prefix: kilo}")
	}
}

func TestComposeTermsDerivesNamedQuantity(t *testing.T) {
	// watt = kg * m^2 * s^-3, expressed here from the registered
	// gram/meter/second units (with a kilo prefix on gram to get to
	// kilogram's factor).
	dim, factor, err := ComposeTerms(
		UnitTerm{UnitName: "gram", Power: 1, Prefix: Prefix{Step: 1}}, // kilo
		UnitTerm{UnitName: "meter", Power: 2},
		UnitTerm{UnitName: "second", Power: -3},
	)
	if err != nil {
		t.Fatalf("ComposeTerms: %v", err)
	}
	want := Dimension{DimMass: 1, DimLength: 2, DimTime: -3}
	if !dim.Equal(want) {
		t.Fatalf("dim = %v, want %v", dim, want)
	}
	if factor != 1000 {
		t.Fatalf("factor = %v, want 1000 (kilogram's factor over the canonical gram)", factor)
	}
}

func TestComposeTermsPermitsMeaninglessButWellDefinedCompound(t *testing.T) {
	// m/ft: dimensionally meaningless (nobody divides a length by a
	// length to get "feet per meter" as a quantity), arithmetically
	// well-defined (dimensionless, factor ~3.28084) — the design doc's
	// own example of a garbage-but-reachable intermediate. The system
	// permits it rather than rejecting it, because closure over the
	// operations requires admitting it.
	dim, factor, err := ComposeTerms(
		UnitTerm{UnitName: "meter", Power: 1},
		UnitTerm{UnitName: "foot", Power: -1},
	)
	if err != nil {
		t.Fatalf("ComposeTerms: %v", err)
	}
	if !dim.IsDimensionless() {
		t.Fatalf("m/ft dim = %v, want dimensionless", dim)
	}
	const wantFactor = 1 / 0.3048
	if diff := factor - wantFactor; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("m/ft factor = %v, want approximately %v", factor, wantFactor)
	}
}

func TestComposeTermsRejectsAffineUnit(t *testing.T) {
	_, _, err := ComposeTerms(
		UnitTerm{UnitName: "celsius", Power: 1},
		UnitTerm{UnitName: "second", Power: -1},
	)
	if err == nil {
		t.Fatal("expected an error composing an affine unit (celsius) into a compound expression")
	}
}

func TestComposeTermsUnknownUnit(t *testing.T) {
	if _, _, err := ComposeTerms(UnitTerm{UnitName: "furlong", Power: 1}); err == nil {
		t.Fatal("expected an error for an unregistered unit")
	}
}

func TestCountDimensionRequiresRegistration(t *testing.T) {
	if err := ValidateDimension(Dimension{CountDimension("request"): 1}); err != nil {
		t.Fatalf("expected the platform's own registered count:request to validate, got: %v", err)
	}
	if err := ValidateDimension(Dimension{CountDimension("totally_unregistered_thing"): 1}); err == nil {
		t.Fatal("expected an unregistered count kind to fail validation")
	}
}

func TestRegisterCountKindRejectsReservedNamespace(t *testing.T) {
	if err := RegisterCountKind("typedvalue.sneaky", false); err == nil {
		t.Fatal("expected registering a count kind under the reserved typedvalue namespace to fail")
	}
	if err := RegisterCountKind("typedvalue.sneaky", true); err != nil {
		t.Fatalf("expected allowReserved=true to permit it, got: %v", err)
	}
}

func TestRegisterCountKindThenValidates(t *testing.T) {
	if err := RegisterCountKind("widget", false); err != nil {
		t.Fatalf("RegisterCountKind: %v", err)
	}
	if err := ValidateDimension(Dimension{CountDimension("widget"): 1}); err != nil {
		t.Fatalf("expected count:widget to validate after registration, got: %v", err)
	}
}

func TestRegisterCurrencyIsNonConvertible(t *testing.T) {
	usd := RegisterCurrency("usd")
	if usd.Convertible {
		t.Fatal("expected a currency unit to be non-convertible — there's no static USD/EUR factor")
	}
	if !usd.Dim.Equal(Dimension{DimCurrency: 1}) {
		t.Fatalf("usd.Dim = %v, want currency^1", usd.Dim)
	}
	got, ok := LookupCurrency("USD")
	if !ok || got.Name != usd.Name {
		t.Fatalf("LookupCurrency(\"USD\") = (%+v, %v), want the same registered usd unit", got, ok)
	}
}

func TestCurrencyPerHourStillComposes(t *testing.T) {
	// Non-convertible only blocks usd+eur arithmetic (unit identity
	// must match, not just dimension); compound composition with
	// other dimensions is unaffected.
	RegisterCurrency("usd")
	dim, _, err := ComposeTerms(
		UnitTerm{UnitName: "currency:USD", Power: 1},
		UnitTerm{UnitName: "second", Power: -1},
	)
	if err != nil {
		t.Fatalf("ComposeTerms: %v", err)
	}
	want := Dimension{DimCurrency: 1, DimTime: -1}
	if !dim.Equal(want) {
		t.Fatalf("dim = %v, want %v", dim, want)
	}
}
