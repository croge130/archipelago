package typedvalue

import "math"

// Prefix scales a unit by Base()^Step — 1000 for the decimal (SI)
// ladder, 1024 for the binary (IEC) ladder. No precomputed multiplier
// field: int64 overflows before zetta (10^21), so the multiplier is
// computed on demand at the call site (float64 for display math,
// big.Int if exact integer arithmetic above ~9 PB is ever needed),
// keeping this table pure data.
type Prefix struct {
	Name   string
	Symbol string
	Step   int
	Binary bool
}

func (p Prefix) Base() float64 {
	if p.Binary {
		return 1024
	}
	return 1000
}

// Multiplier is Base()^Step.
func (p Prefix) Multiplier() float64 {
	return math.Pow(p.Base(), float64(p.Step))
}

// Confusable reports whether given and expected share a step but
// differ in binary-ness — k/Ki, M/Mi, G/Gi, and every reverse. The
// suffix always determines the parse (G is 10^9, Gi is 2^30,
// unconditionally, per IEC 80000-13); this predicate only says whether
// a human might have meant the other one, informing a future
// plausibility advisory, never reinterpreting the value itself.
func Confusable(given, expected Prefix) bool {
	return given.Step == expected.Step && given.Binary != expected.Binary
}

// NoPrefix is the identity prefix — Step 0, either ladder.
var NoPrefix = Prefix{Name: "", Symbol: "", Step: 0, Binary: false}

// Decimal SI prefixes, through the 2022 additions (ronna/quetta,
// ronto/quecto) and down to quecto. The full ladder costs four rows
// each way over stopping at exa/zepto; nothing about keeping it
// uniform is optional once the decision is "store (Base, Step), not a
// precomputed multiplier" — that choice is what makes the extra rows
// free.
var decimalPrefixes = []Prefix{
	{Name: "quetta", Symbol: "Q", Step: 10},
	{Name: "ronna", Symbol: "R", Step: 9},
	{Name: "yotta", Symbol: "Y", Step: 8},
	{Name: "zetta", Symbol: "Z", Step: 7},
	{Name: "exa", Symbol: "E", Step: 6},
	{Name: "peta", Symbol: "P", Step: 5},
	{Name: "tera", Symbol: "T", Step: 4},
	{Name: "giga", Symbol: "G", Step: 3},
	{Name: "mega", Symbol: "M", Step: 2},
	{Name: "kilo", Symbol: "k", Step: 1},
	{Name: "", Symbol: "", Step: 0},
	{Name: "milli", Symbol: "m", Step: -1},
	{Name: "micro", Symbol: "μ", Step: -2},
	{Name: "nano", Symbol: "n", Step: -3},
	{Name: "pico", Symbol: "p", Step: -4},
	{Name: "femto", Symbol: "f", Step: -5},
	{Name: "atto", Symbol: "a", Step: -6},
	{Name: "zepto", Symbol: "z", Step: -7},
	{Name: "yocto", Symbol: "y", Step: -8},
	{Name: "ronto", Symbol: "r", Step: -9},
	{Name: "quecto", Symbol: "q", Step: -10},
}

// Binary IEC 80000-13 prefixes. The ladder stops at yobi (step 8) —
// there are no binary counterparts to ronna/quetta, so the binary walk
// a future auto-scale formatter does must cap at 8, not mirror the
// decimal ladder's step 10.
var binaryPrefixes = []Prefix{
	{Name: "yobi", Symbol: "Yi", Step: 8, Binary: true},
	{Name: "zebi", Symbol: "Zi", Step: 7, Binary: true},
	{Name: "exbi", Symbol: "Ei", Step: 6, Binary: true},
	{Name: "pebi", Symbol: "Pi", Step: 5, Binary: true},
	{Name: "tebi", Symbol: "Ti", Step: 4, Binary: true},
	{Name: "gibi", Symbol: "Gi", Step: 3, Binary: true},
	{Name: "mebi", Symbol: "Mi", Step: 2, Binary: true},
	{Name: "kibi", Symbol: "Ki", Step: 1, Binary: true},
	{Name: "", Symbol: "", Step: 0, Binary: true},
}

// Prefixes returns the full decimal and binary ladders.
func Prefixes() []Prefix {
	out := make([]Prefix, 0, len(decimalPrefixes)+len(binaryPrefixes))
	out = append(out, decimalPrefixes...)
	out = append(out, binaryPrefixes...)
	return out
}
