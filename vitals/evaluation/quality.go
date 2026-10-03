package evaluation

import (
	"github.com/croge130/archipelago/vitals/structure"
)

// QualityWarnings derives warnings for reading, given expectedStates
// (per structure.ExpectedStatesOrDefault) — semantically odd
// combinations warn rather than reject, per the original Vitals
// supplement's §6.4. Never returns a nil slice, so a caller can treat
// "no warnings" and "not yet checked" distinctly if it needs to.
func QualityWarnings(reading structure.Reading, expectedStates []structure.State) []structure.QualityWarning {
	warnings := []structure.QualityWarning{}
	if reading.Impact == "" {
		return warnings
	}
	if reading.State == structure.StateOK && reading.Impact == structure.ImpactCritical {
		warnings = append(warnings, structure.QualityWarning{
			Code:    "ok_with_critical_impact",
			Message: "state ok reported with critical impact",
		})
	}
	if reading.Impact == structure.ImpactNone && reading.ImpactScore != nil && *reading.ImpactScore > 0 {
		warnings = append(warnings, structure.QualityWarning{
			Code:    "none_impact_with_score",
			Message: "impact none reported with non-zero impact score",
		})
	}
	if structure.StateIn(reading.State, expectedStates) &&
		(reading.Impact == structure.ImpactHigh || reading.Impact == structure.ImpactCritical) {
		warnings = append(warnings, structure.QualityWarning{
			Code:    "expected_state_with_high_impact",
			Message: "expected state reported with high or critical impact",
		})
	}
	return warnings
}
