package typedvalue

import "testing"

func TestDimensionEqualIgnoresConstructionOrder(t *testing.T) {
	a := Dimension{DimMass: 1, DimLength: 2, DimTime: -3}
	b := Dimension{DimTime: -3, DimMass: 1, DimLength: 2}
	if !a.Equal(b) {
		t.Fatalf("expected %v to equal %v regardless of how it was built", a, b)
	}
}

func TestDimensionlessIsEmpty(t *testing.T) {
	if !Dimensionless().IsDimensionless() {
		t.Fatal("expected Dimensionless() to report as dimensionless")
	}
	empty := Dimension{}
	if !empty.IsDimensionless() {
		t.Fatal("expected an empty literal to report as dimensionless too")
	}
}

func TestDimensionMulAddsExponents(t *testing.T) {
	watt := Dimension{DimMass: 1, DimLength: 2, DimTime: -3}
	newton := Dimension{DimMass: 1, DimLength: 1, DimTime: -2}
	velocity := Dimension{DimLength: 1, DimTime: -1}
	if got := newton.Mul(velocity); !got.Equal(watt) {
		t.Fatalf("newton * velocity = %v, want %v", got, watt)
	}
}

func TestDimensionMulCancelsToZero(t *testing.T) {
	requestsPerUser := Dimension{CountDimension("request"): 1, CountDimension("user"): -1}
	usersPerRequest := Dimension{CountDimension("user"): 1, CountDimension("request"): -1}
	if got := requestsPerUser.Mul(usersPerRequest); !got.IsDimensionless() {
		t.Fatalf("expected requests/user * users/request to cancel to dimensionless, got %v", got)
	}
}

func TestDimensionDivSubtractsExponents(t *testing.T) {
	area := Dimension{DimLength: 2}
	length := Dimension{DimLength: 1}
	if got := area.Div(length); !got.Equal(length) {
		t.Fatalf("area / length = %v, want %v", got, length)
	}
}

func TestDimensionPow(t *testing.T) {
	length := Dimension{DimLength: 1}
	area := Dimension{DimLength: 2}
	if got := length.Pow(2); !got.Equal(area) {
		t.Fatalf("length^2 = %v, want %v", got, area)
	}
	if got := length.Pow(0); !got.IsDimensionless() {
		t.Fatalf("length^0 = %v, want dimensionless", got)
	}
}

func TestCountDimensionMustBeParameterized(t *testing.T) {
	name := CountDimension("request")
	what, ok := IsCountDimension(name)
	if !ok || what != "request" {
		t.Fatalf("IsCountDimension(%q) = (%q, %v), want (request, true)", name, what, ok)
	}
	if _, ok := IsCountDimension(DimLength); ok {
		t.Fatal("expected a non-count dimension name to not parse as a count dimension")
	}
}

func TestAngleDoesNotCollapseToDimensionless(t *testing.T) {
	angle := Dimension{DimAngle: 1}
	if angle.Equal(Dimensionless()) {
		t.Fatal("expected angle to compare unequal to dimensionless, even though both are physically unitless in strict SI")
	}
}
