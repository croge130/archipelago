package typedvalue

import "testing"

func TestPrefixMultiplierDecimal(t *testing.T) {
	kilo := Prefix{Step: 1}
	if got := kilo.Multiplier(); got != 1000 {
		t.Fatalf("kilo.Multiplier() = %v, want 1000", got)
	}
}

func TestPrefixMultiplierBinary(t *testing.T) {
	kibi := Prefix{Step: 1, Binary: true}
	if got := kibi.Multiplier(); got != 1024 {
		t.Fatalf("kibi.Multiplier() = %v, want 1024", got)
	}
}

func TestPrefixMultiplierQuettaAndYobiDontOverflow(t *testing.T) {
	// int64 caps at ~9.22e18; quetta (1e30) and yobi (2^80) both
	// exceed that comfortably — the whole point of computing on
	// demand in float64 rather than precomputing into an int field.
	quetta := Prefix{Step: 10}
	if got := quetta.Multiplier(); got <= 1e29 {
		t.Fatalf("quetta.Multiplier() = %v, want approximately 1e30", got)
	}
	yobi := Prefix{Step: 8, Binary: true}
	want := 1208925819614629174706176.0 // 2^80
	if got := yobi.Multiplier(); got != want {
		t.Fatalf("yobi.Multiplier() = %v, want %v", got, want)
	}
}

func TestConfusablePredicate(t *testing.T) {
	kilo := Prefix{Step: 1, Binary: false}
	kibi := Prefix{Step: 1, Binary: true}
	if !Confusable(kilo, kibi) {
		t.Fatal("expected kilo/kibi (same step, different base) to be confusable")
	}
	mega := Prefix{Step: 2, Binary: false}
	if Confusable(kilo, mega) {
		t.Fatal("expected different steps to never be confusable regardless of base")
	}
	kilo2 := Prefix{Step: 1, Binary: false}
	if Confusable(kilo, kilo2) {
		t.Fatal("expected the same prefix to not be confusable with itself")
	}
}

func TestPrefixesIncludesFullLadders(t *testing.T) {
	all := Prefixes()
	var sawQuetta, sawQuecto, sawYobi bool
	for _, p := range all {
		switch {
		case p.Name == "quetta" && p.Step == 10 && !p.Binary:
			sawQuetta = true
		case p.Name == "quecto" && p.Step == -10 && !p.Binary:
			sawQuecto = true
		case p.Name == "yobi" && p.Step == 8 && p.Binary:
			sawYobi = true
		}
	}
	if !sawQuetta || !sawQuecto {
		t.Fatal("expected the decimal ladder to run through the 2022 additions (quetta/quecto)")
	}
	if !sawYobi {
		t.Fatal("expected the binary ladder to run through yobi (step 8), its actual ceiling")
	}
}
