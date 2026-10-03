package evaluation

import (
	"testing"

	"github.com/croge130/archipelago/vitals/structure"
)

func TestIsNotableTransitionFirstReadingIsAlwaysNotable(t *testing.T) {
	r := structure.Reading{State: structure.StateOK}
	if !IsNotableTransition(structure.Reading{}, r, false) {
		t.Fatal("expected an instance's first-ever reading to be notable")
	}
}

func TestIsNotableTransitionSameStateIsNotNotable(t *testing.T) {
	r := structure.Reading{State: structure.StateOK, Summary: "fine"}
	if IsNotableTransition(r, r, true) {
		t.Fatal("expected an identical repeat reading to not be notable")
	}
}

func TestIsNotableTransitionStateChange(t *testing.T) {
	prev := structure.Reading{State: structure.StateOK}
	cur := structure.Reading{State: structure.StateDegraded}
	if !IsNotableTransition(prev, cur, true) {
		t.Fatal("expected a state change to be notable")
	}
}

func TestIsNotableTransitionImpactScoreChange(t *testing.T) {
	a, b := 10, 20
	prev := structure.Reading{State: structure.StateOK, ImpactScore: &a}
	cur := structure.Reading{State: structure.StateOK, ImpactScore: &b}
	if !IsNotableTransition(prev, cur, true) {
		t.Fatal("expected an impact_score change to be notable")
	}
}

func TestIsNotableTransitionReasonCodeChange(t *testing.T) {
	prev := structure.Reading{State: structure.StateDegraded, ReasonCode: "database.unreachable"}
	cur := structure.Reading{State: structure.StateDegraded, ReasonCode: "index.queue_lag_high"}
	if !IsNotableTransition(prev, cur, true) {
		t.Fatal("expected a reason_code change to be notable")
	}
}
