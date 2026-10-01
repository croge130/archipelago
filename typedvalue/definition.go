package typedvalue

import "fmt"

// StorageType is how a value is represented in Go/JSON — closed,
// unlike SemanticType, because the set of representations a value can
// actually take is finite.
type StorageType string

const (
	StorageBool   StorageType = "bool"
	StorageInt    StorageType = "int"
	StorageFloat  StorageType = "float"
	StorageString StorageType = "string"
	StorageEnum   StorageType = "enum"
	StorageJSON   StorageType = "json"
	StorageObject StorageType = "object"
)

func (t StorageType) Valid() bool {
	switch t {
	case StorageBool, StorageInt, StorageFloat, StorageString, StorageEnum, StorageJSON, StorageObject:
		return true
	default:
		return false
	}
}

// SemanticType names a distinction dimensions provably can't make —
// duration vs. timestamp (both time-dimensioned; adding two durations
// is fine, adding two timestamps is an error), torque vs. energy (both
// kg·m²·s⁻²), cache-hit-rate vs. error-rate (both plain fractions).
// Open vocabulary, not a closed enum — the same "extensible, hygiene
// via namespace, not a validated switch" treatment certstore's
// EnrollmentPurpose already uses — because the set of meaningful
// distinctions an app might need is not something this package can
// enumerate for it.
type SemanticType string

const (
	SemanticGeneric     SemanticType = "generic"
	SemanticDuration    SemanticType = "duration"
	SemanticTimestamp   SemanticType = "timestamp"
	SemanticSize        SemanticType = "size"
	SemanticRatio       SemanticType = "ratio"
	SemanticCount       SemanticType = "count"
	SemanticRate        SemanticType = "rate"
	SemanticMoney       SemanticType = "money"
	SemanticTemperature SemanticType = "temperature"
	SemanticIdentifier  SemanticType = "identifier"
	SemanticStructured  SemanticType = "structured"
)

// Definition describes what a value means. The dimension vector is
// never persisted here — it's derived on demand from UnitName (a
// simple unit reference) or Dimensions (the authored compound-unit
// term list), exactly one of which may be set. Deriving rather than
// storing means the stored shape never has to change as the unit
// registry grows.
type Definition struct {
	Storage       StorageType
	Semantic      SemanticType
	UnitName      string     // simple unit reference, e.g. "second", "byte"; mutually exclusive with Dimensions
	Dimensions    []UnitTerm // authored compound unit, e.g. count:request / second; mutually exclusive with UnitName
	AllowedValues []string   // required when Storage == StorageEnum; the one constraint that stays here, because an enum without its member set is not a complete description of the type
	Description   string
	Fields        map[string]Definition // required when Storage == StorageObject
}

// Dimension derives this definition's dimension and conversion factor
// from UnitName or Dimensions. A definition with neither (a bare bool,
// a plain string) is dimensionless with factor 1.
func (d Definition) Dimension() (Dimension, float64, error) {
	if d.UnitName != "" {
		u, ok := LookupUnit(d.UnitName)
		if !ok {
			return nil, 0, fmt.Errorf("typedvalue: unknown unit %q", d.UnitName)
		}
		return u.Dim, u.Factor, nil
	}
	if len(d.Dimensions) > 0 {
		return ComposeTerms(d.Dimensions...)
	}
	return Dimensionless(), 1, nil
}

// Validate checks that the definition is internally coherent — not
// that any particular value satisfies it (that's ValidateValue) and
// not range/length/pattern rules (that's typeconstraints, one way
// dependent on this package).
func (d Definition) Validate() error {
	if !d.Storage.Valid() {
		return fmt.Errorf("typedvalue: unknown storage type %q", d.Storage)
	}
	if d.UnitName != "" && len(d.Dimensions) > 0 {
		return fmt.Errorf("typedvalue: UnitName and Dimensions are mutually exclusive")
	}
	if _, _, err := d.Dimension(); err != nil {
		return err
	}
	switch d.Storage {
	case StorageEnum:
		if len(d.AllowedValues) == 0 {
			return fmt.Errorf("typedvalue: enum definitions require AllowedValues")
		}
	case StorageObject:
		if len(d.Fields) == 0 {
			return fmt.Errorf("typedvalue: object definitions require Fields")
		}
	}
	for name, field := range d.Fields {
		if name == "" {
			return fmt.Errorf("typedvalue: object field name must not be empty")
		}
		if err := field.Validate(); err != nil {
			return fmt.Errorf("typedvalue: field %q: %w", name, err)
		}
	}
	return nil
}

func allowedValueSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}
