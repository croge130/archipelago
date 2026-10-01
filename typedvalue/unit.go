package typedvalue

import (
	"fmt"
	"strings"
	"sync"
)

// Unit is one entry in the unit registry. Kind is deliberately absent
// — two units are the same kind iff their Dim maps are equal, so a
// separate string field that could disagree with the dimension map
// never exists. Kilogram is not a unit: it's {Unit: gram, Prefix:
// kilo}, one registry entry per unit with the prefix ladder
// generating mg/kg/µg for free.
type Unit struct {
	Name        string
	Symbol      string
	Dim         Dimension
	Factor      float64 // to the canonical unit of this Dim
	Offset      float64 // affine units only; 0 otherwise
	Affine      bool    // point rather than vector — a timestamp, not a duration
	Convertible bool    // false for currency and count units
}

var (
	registryMu sync.RWMutex
	units      = map[string]Unit{}
	countKinds = map[string]bool{}
	currencies = map[string]Unit{}
)

func registerUnit(u Unit) {
	registryMu.Lock()
	defer registryMu.Unlock()
	units[u.Name] = u
}

// LookupUnit returns a registered unit by name.
func LookupUnit(name string) (Unit, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	u, ok := units[name]
	return u, ok
}

// RegisterCountKind opens a count:<what> dimension name for use.
// Mass/length/time/etc. need no such gate — count specifically does,
// because a bare "count" dimension cancels against itself
// (requests/user would otherwise be indistinguishable from a bare
// ratio), so every count:<what> name has to be explicit, and
// explicit names need a registry to typo-check against. Follows the
// same reserved-prefix hygiene gatehouse-core's permission namespaces
// use — a collision guardrail, not a security boundary — enforced by
// requireUnreserved.
func RegisterCountKind(what string, allowReserved bool) error {
	what = strings.TrimSpace(what)
	if what == "" {
		return fmt.Errorf("typedvalue: count kind must not be empty")
	}
	if !allowReserved {
		if err := requireUnreserved(what); err != nil {
			return fmt.Errorf("typedvalue: register count kind %q: %w", what, err)
		}
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	countKinds[what] = true
	units["count:"+what] = Unit{
		Name:        "count:" + what,
		Dim:         Dimension{CountDimension(what): 1},
		Factor:      1,
		Convertible: false,
	}
	return nil
}

// KnownCountKind reports whether what has been registered via
// RegisterCountKind.
func KnownCountKind(what string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return countKinds[what]
}

// RegisterCurrency registers a non-convertible currency unit under
// the currency dimension. Non-convertible because there's no static
// factor between USD and EUR — conversion rates are time-varying, not
// unit conversions — so usd+eur fails structurally (Convertible is
// false) while compound composition (currency-per-hour,
// currency-per-byte) still works through the dimension itself.
func RegisterCurrency(code string) Unit {
	code = strings.ToUpper(strings.TrimSpace(code))
	u := Unit{
		Name:        "currency:" + code,
		Symbol:      code,
		Dim:         Dimension{DimCurrency: 1},
		Factor:      1,
		Convertible: false,
	}
	registryMu.Lock()
	currencies[code] = u
	registryMu.Unlock()
	registerUnit(u)
	return u
}

// LookupCurrency returns a registered currency unit by ISO code.
func LookupCurrency(code string) (Unit, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	u, ok := currencies[strings.ToUpper(strings.TrimSpace(code))]
	return u, ok
}

// ValidateDimension checks that every count:<what> name appearing in
// d has been registered. Every other dimension name is accepted as
// written — only count needs the typo-guard, per RegisterCountKind's
// own reasoning.
func ValidateDimension(d Dimension) error {
	for name := range d {
		if what, ok := IsCountDimension(name); ok {
			if !KnownCountKind(what) {
				return fmt.Errorf("typedvalue: unregistered count kind %q (call RegisterCountKind first)", what)
			}
		}
	}
	return nil
}

// UnitTerm is the authored representation of a compound unit — what a
// human writes and what renders back as kg·m²·s⁻³. The derived
// Dimension and Factor (via ComposeTerms) are what comparison and
// conversion actually use; keeping both means equality no longer
// depends on term order the way Lighthouse's original []UnitTerm-only
// model did.
type UnitTerm struct {
	UnitName string
	Power    int
	Prefix   Prefix
}

// ComposeTerms derives the Dimension and Factor a list of authored
// terms describes: Dim = Σ(component.Dim × power), Factor =
// Π(component.Factor × prefix.Multiplier())^power. This permits
// dimensionally meaningless but arithmetically well-defined compounds
// (m/ft yields a dimensionless Factor of ~3.28084) on purpose — a
// system that could only express named quantities would break on
// valid unnamed intermediates en route to a named one, and closure
// over the operations necessarily admits combinations nobody wants.
func ComposeTerms(terms ...UnitTerm) (Dimension, float64, error) {
	dim := Dimensionless()
	factor := 1.0
	for _, t := range terms {
		u, ok := LookupUnit(t.UnitName)
		if !ok {
			return nil, 0, fmt.Errorf("typedvalue: unknown unit %q", t.UnitName)
		}
		if u.Affine {
			return nil, 0, fmt.Errorf("typedvalue: affine unit %q cannot appear in a compound expression", t.UnitName)
		}
		dim = dim.Mul(u.Dim.Pow(t.Power))
		componentFactor := u.Factor * t.Prefix.Multiplier()
		for n := 0; n < intAbs(t.Power); n++ {
			if t.Power > 0 {
				factor *= componentFactor
			} else {
				factor /= componentFactor
			}
		}
	}
	return dim, factor, nil
}

func intAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func init() {
	// The seven SI base dimensions' canonical units. Mass is
	// canonicalized to the gram, not the kilogram — SI's own choice is
	// the one base unit with a prefix baked into its name, which would
	// otherwise force mass to be an exception in an otherwise-uniform
	// prefix ladder.
	registerUnit(Unit{Name: "gram", Symbol: "g", Dim: Dimension{DimMass: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "meter", Symbol: "m", Dim: Dimension{DimLength: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "foot", Symbol: "ft", Dim: Dimension{DimLength: 1}, Factor: 0.3048, Convertible: true})
	registerUnit(Unit{Name: "second", Symbol: "s", Dim: Dimension{DimTime: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "ampere", Symbol: "A", Dim: Dimension{DimElectricCurrent: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "kelvin", Symbol: "K", Dim: Dimension{DimTemperature: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "mole", Symbol: "mol", Dim: Dimension{DimAmountOfSubstance: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "candela", Symbol: "cd", Dim: Dimension{DimLuminousIntensity: 1}, Factor: 1, Convertible: true})

	// Affine companions to the vector base units — points, not
	// vectors; barred from compound expressions by ComposeTerms.
	registerUnit(Unit{Name: "celsius", Symbol: "°C", Dim: Dimension{DimTemperature: 1}, Factor: 1, Offset: 273.15, Affine: true, Convertible: true})
	registerUnit(Unit{Name: "unix_time", Symbol: "", Dim: Dimension{DimTime: 1}, Factor: 1, Affine: true, Convertible: true})

	// Information, non-SI but needed throughout.
	registerUnit(Unit{Name: "bit", Symbol: "b", Dim: Dimension{DimInformation: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "byte", Symbol: "B", Dim: Dimension{DimInformation: 1}, Factor: 8, Convertible: true})

	// Angle/solid angle, tracked separately from dimensionless so a
	// radian never compares equal to a bare ratio.
	registerUnit(Unit{Name: "radian", Symbol: "rad", Dim: Dimension{DimAngle: 1}, Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "steradian", Symbol: "sr", Dim: Dimension{DimSolidAngle: 1}, Factor: 1, Convertible: true})

	// Dimensionless, but still a meaningful unit in its own right.
	registerUnit(Unit{Name: "fraction", Symbol: "", Dim: Dimensionless(), Factor: 1, Convertible: true})
	registerUnit(Unit{Name: "percent", Symbol: "%", Dim: Dimensionless(), Factor: 0.01, Convertible: true})
	registerUnit(Unit{Name: "ppm", Symbol: "ppm", Dim: Dimensionless(), Factor: 1e-6, Convertible: true})

	// Core count kinds the platform ships; apps register their own
	// under RegisterCountKind — the same function, so there's only
	// one place that turns a count kind into both a registry entry
	// and a unit.
	for _, kind := range []string{"request", "item", "error", "attempt", "retry"} {
		if err := RegisterCountKind(kind, true); err != nil {
			panic(fmt.Sprintf("typedvalue: registering core count kind %q: %v", kind, err))
		}
	}
}
