package evaluation

import (
	"testing"

	"github.com/croge130/archipelago/vitals/structure"
)

func TestQualityWarningsNoneWhenImpactAbsent(t *testing.T) {
	r := structure.Reading{State: structure.StateOK}
	if got := QualityWarnings(r, []structure.State{structure.StateOK}); len(got) != 0 {
		t.Fatalf("expected no warnings, got %v", got)
	}
}

func TestQualityWarningsOKWithCriticalImpact(t *testing.T) {
	r := structure.Reading{State: structure.StateOK, Impact: structure.ImpactCritical}
	// expectedStates deliberately excludes ok, so the expected-state
	// rule doesn't also fire — isolating just this one warning.
	got := QualityWarnings(r, []structure.State{structure.StateRunning})
	if len(got) != 1 || got[0].Code != "ok_with_critical_impact" {
		t.Fatalf("expected ok_with_critical_impact, got %v", got)
	}
}

func TestQualityWarningsNoneImpactWithScore(t *testing.T) {
	score := 10
	r := structure.Reading{State: structure.StateRunning, Impact: structure.ImpactNone, ImpactScore: &score}
	got := QualityWarnings(r, []structure.State{structure.StateRunning})
	if len(got) != 1 || got[0].Code != "none_impact_with_score" {
		t.Fatalf("expected none_impact_with_score, got %v", got)
	}
}

func TestQualityWarningsExpectedStateWithHighImpact(t *testing.T) {
	r := structure.Reading{State: structure.StateRunning, Impact: structure.ImpactHigh}
	got := QualityWarnings(r, []structure.State{structure.StateRunning})
	if len(got) != 1 || got[0].Code != "expected_state_with_high_impact" {
		t.Fatalf("expected expected_state_with_high_impact, got %v", got)
	}
}

func TestQualityWarningsUnexpectedStateWithHighImpactIsFine(t *testing.T) {
	r := structure.Reading{State: structure.StateDegraded, Impact: structure.ImpactHigh}
	got := QualityWarnings(r, []structure.State{structure.StateOK})
	if len(got) != 0 {
		t.Fatalf("expected no warnings for an unexpected state with high impact, got %v", got)
	}
}
