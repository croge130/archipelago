package typedvalue

import "strings"

// The curated constructors are the actual interface most callers use
// — the general dimension/unit model exists so these can be correct,
// not so anyone builds a Definition by hand. New constructors the
// richer model wants beyond what Lighthouse already had: Count,
// RatePerSecond, Temperature, Money.

func Bool(description string) Definition {
	return Definition{Storage: StorageBool, Description: description}
}

func Int(unit, description string) Definition {
	return Definition{Storage: StorageInt, UnitName: unit, Description: description}
}

func Float(unit, description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: unit, Description: description}
}

func String(description string) Definition {
	return Definition{Storage: StorageString, Description: description}
}

func Enum(allowedValues []string, description string) Definition {
	values := make([]string, 0, len(allowedValues))
	for _, v := range allowedValues {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}
	return Definition{Storage: StorageEnum, AllowedValues: values, Description: description}
}

func JSON(description string) Definition {
	return Definition{Storage: StorageJSON, Semantic: SemanticStructured, Description: description}
}

func Object(fields map[string]Definition, description string) Definition {
	return Definition{Storage: StorageObject, Semantic: SemanticStructured, Fields: fields, Description: description}
}

// DurationSeconds is a vector quantity — adding two durations is
// fine, unlike TimestampUnix below.
func DurationSeconds(description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: "second", Semantic: SemanticDuration, Description: description}
}

// TimestampUnix is an affine quantity (a point, not a vector) —
// adding two timestamps is meaningless even though it shares
// DurationSeconds' dimension.
func TimestampUnix(description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: "unix_time", Semantic: SemanticTimestamp, Description: description}
}

func SizeBytes(description string) Definition {
	return Definition{Storage: StorageInt, UnitName: "byte", Semantic: SemanticSize, Description: description}
}

func SizeBits(description string) Definition {
	return Definition{Storage: StorageInt, UnitName: "bit", Semantic: SemanticSize, Description: description}
}

func Fraction(description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: "fraction", Semantic: SemanticRatio, Description: description}
}

func Percent(description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: "percent", Semantic: SemanticRatio, Description: description}
}

// Count is dimensioned count:<what> — what must already be registered
// via RegisterCountKind (the platform's own core kinds already are).
func Count(what, description string) Definition {
	return Definition{Storage: StorageInt, UnitName: "count:" + what, Semantic: SemanticCount, Description: description}
}

// RatePerSecond composes unit/second as a compound dimension — e.g.
// RatePerSecond("count:request") for requests per second.
func RatePerSecond(unit, description string) Definition {
	return Definition{
		Storage:     StorageFloat,
		Dimensions:  []UnitTerm{{UnitName: unit, Power: 1}, {UnitName: "second", Power: -1}},
		Semantic:    SemanticRate,
		Description: description,
	}
}

// Temperature is the vector (difference) quantity, in kelvin —
// kelvin-only sidesteps the affine Celsius case entirely, a legitimate
// choice per the design doc unless an app genuinely needs absolute
// Celsius values.
func Temperature(description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: "kelvin", Semantic: SemanticTemperature, Description: description}
}

// TemperatureCelsius is the affine (point) quantity — an absolute
// reading, not a difference; barred from compound expressions the
// same as any other affine unit.
func TemperatureCelsius(description string) Definition {
	return Definition{Storage: StorageFloat, UnitName: "celsius", Semantic: SemanticTemperature, Description: description}
}

// Money registers currencyCode on first use if it isn't already and
// stores values as integer minor units, never float — the design
// doc's explicit rule, since currency conversion rates are
// time-varying and not a unit-conversion factor at all.
func Money(currencyCode, description string) Definition {
	u, ok := LookupCurrency(currencyCode)
	if !ok {
		u = RegisterCurrency(currencyCode)
	}
	return Definition{Storage: StorageInt, UnitName: u.Name, Semantic: SemanticMoney, Description: description}
}
