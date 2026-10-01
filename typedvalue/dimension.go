package typedvalue

import (
	"fmt"
	"sort"
	"strings"
)

// Dimension is a sparse map from dimension name to integer exponent —
// {"mass":1,"length":2,"time":-3} for a watt, {} for dimensionless.
// A map, not Lighthouse's original []UnitTerm, because a map has
// exactly one canonical form regardless of how it was built: two
// authored orderings of the same quantity are equal as Dimensions even
// though they'd be unequal JSON as a term list. Zero-exponent entries
// are never stored — Mul/Div/Pow all drop them, so Equal is a plain
// map comparison with nothing to normalize away first.
type Dimension map[string]int

// The seven SI base dimensions, in full, plus Archipelago's
// non-SI additions (information, angle, solid_angle, currency).
// Included even though some will never be exercised: a partial base
// set breaks closure, since every derived unit is a product of base
// dimensions and omitting one makes an entire class inexpressible.
const (
	DimMass              = "mass"
	DimLength            = "length"
	DimTime              = "time"
	DimElectricCurrent   = "electric_current"
	DimTemperature       = "temperature"
	DimAmountOfSubstance = "amount_of_substance"
	DimLuminousIntensity = "luminous_intensity"
	DimInformation       = "information"
	DimAngle             = "angle"
	DimSolidAngle        = "solid_angle"
	DimCurrency          = "currency"
	countDimensionPrefix = "count:"
)

// CountDimension names a countable-thing dimension — always
// parameterized ("count:request", never a bare "count"), because a
// generic count dimension cancels against itself (count¹·count⁻¹ =
// dimensionless, indistinguishable from a bare ratio) the same way an
// un-tracked angle would collapse into "dimensionless". what must
// already be a registered count kind — see RegisterCountKind.
func CountDimension(what string) string {
	return countDimensionPrefix + strings.TrimSpace(what)
}

// IsCountDimension reports whether name is a count:<what> dimension
// name, and returns the <what> part.
func IsCountDimension(name string) (what string, ok bool) {
	if !strings.HasPrefix(name, countDimensionPrefix) {
		return "", false
	}
	return strings.TrimPrefix(name, countDimensionPrefix), true
}

// Dimensionless is the empty dimension — a meaningful value in its own
// right (fraction/percent/ppm live here), not an absence.
func Dimensionless() Dimension { return Dimension{} }

func (d Dimension) clone() Dimension {
	out := make(Dimension, len(d))
	for k, v := range d {
		if v != 0 {
			out[k] = v
		}
	}
	return out
}

// Equal reports whether two dimensions describe the same physical
// quantity. Dimensionless (nil or empty) compares equal to itself
// either way, since Go distinguishes nil and empty maps but this type
// never should.
func (d Dimension) Equal(other Dimension) bool {
	if len(d) != len(other) {
		return false
	}
	for k, v := range d {
		if other[k] != v {
			return false
		}
	}
	return true
}

func (d Dimension) IsDimensionless() bool { return len(d) == 0 }

// Mul composes two dimensions as a product — elementwise addition of
// exponents, dropping any that cancel to zero.
func (d Dimension) Mul(other Dimension) Dimension {
	out := d.clone()
	for k, v := range other {
		out[k] += v
	}
	return pruneZero(out)
}

// Div composes two dimensions as a quotient — elementwise subtraction.
func (d Dimension) Div(other Dimension) Dimension {
	out := d.clone()
	for k, v := range other {
		out[k] -= v
	}
	return pruneZero(out)
}

// Pow raises every exponent to the given power. Pow(0) is always
// dimensionless, regardless of what d was.
func (d Dimension) Pow(power int) Dimension {
	if power == 0 {
		return Dimensionless()
	}
	out := make(Dimension, len(d))
	for k, v := range d {
		out[k] = v * power
	}
	return pruneZero(out)
}

func pruneZero(d Dimension) Dimension {
	for k, v := range d {
		if v == 0 {
			delete(d, k)
		}
	}
	return d
}

// String renders a dimension deterministically (sorted keys) for logs
// and error messages — never for comparison, which is Equal's job.
func (d Dimension) String() string {
	if len(d) == 0 {
		return "dimensionless"
	}
	names := make([]string, 0, len(d))
	for k := range d {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s^%d", name, d[name]))
	}
	return strings.Join(parts, "·")
}
