package typedvalue

import "testing"

func TestDurationVsTimestampShareDimensionButNotAffineness(t *testing.T) {
	duration := DurationSeconds("")
	timestamp := TimestampUnix("")

	durDim, _, err := duration.Dimension()
	if err != nil {
		t.Fatalf("Dimension: %v", err)
	}
	tsDim, _, err := timestamp.Dimension()
	if err != nil {
		t.Fatalf("Dimension: %v", err)
	}
	if !durDim.Equal(tsDim) {
		t.Fatalf("expected duration and timestamp to share a dimension, got %v and %v", durDim, tsDim)
	}

	durUnit, _ := LookupUnit(duration.UnitName)
	tsUnit, _ := LookupUnit(timestamp.UnitName)
	if durUnit.Affine {
		t.Fatal("expected DurationSeconds' unit to be a vector (non-affine) — adding two durations is fine")
	}
	if !tsUnit.Affine {
		t.Fatal("expected TimestampUnix's unit to be affine — adding two timestamps is meaningless")
	}
}

func TestTemperatureVsTemperatureCelsius(t *testing.T) {
	kelvinUnit, _ := LookupUnit(Temperature("").UnitName)
	celsiusUnit, _ := LookupUnit(TemperatureCelsius("").UnitName)
	if kelvinUnit.Affine {
		t.Fatal("expected Temperature() (kelvin) to be a vector, not a point")
	}
	if !celsiusUnit.Affine {
		t.Fatal("expected TemperatureCelsius() to be affine")
	}
}

func TestSizeBytesVsSizeBits(t *testing.T) {
	bytesUnit, _ := LookupUnit(SizeBytes("").UnitName)
	bitsUnit, _ := LookupUnit(SizeBits("").UnitName)
	if bytesUnit.Factor != 8*bitsUnit.Factor {
		t.Fatalf("expected byte's factor to be 8x bit's, got %v vs %v", bytesUnit.Factor, bitsUnit.Factor)
	}
}

func TestFractionVsPercentFactor(t *testing.T) {
	fractionUnit, _ := LookupUnit(Fraction("").UnitName)
	percentUnit, _ := LookupUnit(Percent("").UnitName)
	if fractionUnit.Factor != 1 {
		t.Fatalf("fraction.Factor = %v, want 1", fractionUnit.Factor)
	}
	if percentUnit.Factor != 0.01 {
		t.Fatalf("percent.Factor = %v, want 0.01", percentUnit.Factor)
	}
}

func TestMoneyStoresIntegerMinorUnits(t *testing.T) {
	def := Money("usd", "price")
	if def.Storage != StorageInt {
		t.Fatalf("Money() storage = %v, want StorageInt — money is never a float", def.Storage)
	}
	v, err := NormalizeValue(def, 1999) // $19.99 in cents
	if err != nil {
		t.Fatalf("NormalizeValue: %v", err)
	}
	if v != int64(1999) {
		t.Fatalf("v = %v, want int64(1999)", v)
	}
}

func TestMoneyRegistersCurrencyIfNeeded(t *testing.T) {
	def := Money("gbp", "price in GBP")
	if _, ok := LookupCurrency("GBP"); !ok {
		t.Fatal("expected Money() to register the currency if it wasn't already")
	}
	u, ok := LookupUnit(def.UnitName)
	if !ok || u.Convertible {
		t.Fatalf("expected Money()'s unit to be the non-convertible registered currency, got %+v (ok=%v)", u, ok)
	}
}

func TestCountAndRatePerSecondCompose(t *testing.T) {
	if err := RegisterCountKind("widget_sale", false); err != nil {
		t.Fatalf("RegisterCountKind: %v", err)
	}
	count := Count("widget_sale", "widgets sold")
	if err := count.Validate(); err != nil {
		t.Fatalf("Count().Validate(): %v", err)
	}

	rate := RatePerSecond("count:widget_sale", "widget sales per second")
	dim, _, err := rate.Dimension()
	if err != nil {
		t.Fatalf("Dimension: %v", err)
	}
	want := Dimension{CountDimension("widget_sale"): 1, DimTime: -1}
	if !dim.Equal(want) {
		t.Fatalf("dim = %v, want %v", dim, want)
	}
}
