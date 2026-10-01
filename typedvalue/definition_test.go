package typedvalue

import "testing"

func TestDefinitionValidateRejectsUnknownStorage(t *testing.T) {
	d := Definition{Storage: StorageType("nonsense")}
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for an unknown storage type")
	}
}

func TestDefinitionValidateEnumRequiresAllowedValues(t *testing.T) {
	d := Definition{Storage: StorageEnum}
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for an enum with no AllowedValues — it's not a complete type description")
	}
	d.AllowedValues = []string{"active", "archived"}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected a properly completed enum to validate, got: %v", err)
	}
}

func TestDefinitionValidateObjectRequiresFields(t *testing.T) {
	d := Definition{Storage: StorageObject}
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for an object with no Fields")
	}
}

func TestDefinitionValidateRejectsBothUnitNameAndDimensions(t *testing.T) {
	d := Definition{
		Storage:    StorageFloat,
		UnitName:   "second",
		Dimensions: []UnitTerm{{UnitName: "meter", Power: 1}},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected UnitName and Dimensions to be mutually exclusive")
	}
}

func TestDefinitionValidateRecursesIntoFields(t *testing.T) {
	d := Object(map[string]Definition{
		"max_requests": Count("request", "the rate limit ceiling"),
		"bad_field":    {Storage: StorageType("nonsense")},
	}, "a rate limit config")
	if err := d.Validate(); err == nil {
		t.Fatal("expected a bad nested field to fail validation of the whole object")
	}
}

func TestDefinitionDimensionDerivesFromUnitName(t *testing.T) {
	d := DurationSeconds("how long the operation took")
	dim, factor, err := d.Dimension()
	if err != nil {
		t.Fatalf("Dimension: %v", err)
	}
	if !dim.Equal(Dimension{DimTime: 1}) || factor != 1 {
		t.Fatalf("dim=%v factor=%v, want time^1 factor 1", dim, factor)
	}
}

func TestDefinitionDimensionDerivesFromCompoundTerms(t *testing.T) {
	d := RatePerSecond("count:request", "requests per second")
	dim, _, err := d.Dimension()
	if err != nil {
		t.Fatalf("Dimension: %v", err)
	}
	want := Dimension{CountDimension("request"): 1, DimTime: -1}
	if !dim.Equal(want) {
		t.Fatalf("dim = %v, want %v", dim, want)
	}
}

func TestDefinitionDimensionBareTypeIsDimensionless(t *testing.T) {
	dim, factor, err := Bool("a flag").Dimension()
	if err != nil {
		t.Fatalf("Dimension: %v", err)
	}
	if !dim.IsDimensionless() || factor != 1 {
		t.Fatalf("dim=%v factor=%v, want dimensionless factor 1", dim, factor)
	}
}
