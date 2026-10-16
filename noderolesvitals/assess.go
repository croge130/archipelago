package noderolesvitals

import (
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/vitals/structure"
)

// Reason codes. Fixed phrases, never carrying numbers (see the package doc).
const (
	ReasonDisabled      = "noderole.disabled"
	ReasonNotAuthorized = "noderole.not_authorized"
	ReasonRunsFailed    = "noderole.runs_failed"
	ReasonWorkShed      = "noderole.work_shed"
	ReasonSlotsMissed   = "noderole.slots_missed"
	ReasonClamped       = "noderole.budget_clamped"
	ReasonPolicyNote    = "noderole.policy_note"

	ReasonPressureElevated = "governor.pressure_elevated"
	ReasonPressureHigh     = "governor.pressure_high"
)

// counters is what the state is judged on: the cumulative counts a node role
// has accumulated, to be differenced between reports.
type counters struct {
	failed, shed, missed, unauthorized uint64
}

func countersOf(s noderoles.RoleStatus) counters {
	c := counters{
		failed:       s.Governor.Failed,
		shed:         s.Governor.ShedQueueFull + s.Governor.ShedDeferred + s.Governor.ShedOther,
		unauthorized: s.Unauthorized,
	}
	for _, d := range s.Duties {
		c.missed += d.MissedSlots
	}
	return c
}

// since is the change from prev to cur. A count that went down means the node
// role was re-adopted and started again from zero, so what it has now is all
// new.
func since(cur, prev uint64) uint64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}

// assessment is the verdict on one node role for one report.
type assessment struct {
	state   structure.State
	reason  string
	summary string
	delta   counters
}

// assess judges a node role from its status, the counters at the previous
// report (nil for the first), and the notes policy left for it.
func assess(cur noderoles.RoleStatus, prev *counters, policyNotes []string) assessment {
	var base counters
	if prev != nil {
		base = *prev
	}
	now := countersOf(cur)
	d := counters{
		failed:       since(now.failed, base.failed),
		shed:         since(now.shed, base.shed),
		missed:       since(now.missed, base.missed),
		unauthorized: since(now.unauthorized, base.unauthorized),
	}
	a := assessment{delta: d, state: structure.StateOK, summary: "running as adopted"}

	allOff := len(cur.Duties) > 0
	for _, duty := range cur.Duties {
		allOff = allOff && duty.Disabled
	}
	switch {
	case allOff:
		a.state, a.reason, a.summary = structure.StateDisabled, ReasonDisabled, "every duty is switched off"
	case d.unauthorized > 0:
		a.state, a.reason, a.summary = structure.StateFailed, ReasonNotAuthorized, "the node is not authorized to run this node role"
	case d.failed > 0:
		a.state, a.reason, a.summary = structure.StateWarning, ReasonRunsFailed, "runs failed since the last report"
	case d.shed > 0:
		a.state, a.reason, a.summary = structure.StateWarning, ReasonWorkShed, "work was shed since the last report"
	case d.missed > 0:
		a.state, a.reason, a.summary = structure.StateWarning, ReasonSlotsMissed, "scheduled slots were missed since the last report"
	case len(cur.Clamps) > 0:
		a.reason, a.summary = ReasonClamped, "running with a budget reduced by a ceiling"
	case len(policyNotes) > 0:
		a.reason, a.summary = ReasonPolicyNote, "policy asked for something that could not be applied as asked"
	}
	return a
}

// assessGovernor judges the node's governor from its pressure alone: holding
// work back is the one thing that is the node's own doing and worth seeing at
// a glance. Load shows in the details.
func assessGovernor(p governor.Pressure) (structure.State, string, string) {
	switch p {
	case governor.PressureHigh:
		return structure.StateDegraded, ReasonPressureHigh, "the node is holding back work under high pressure"
	case governor.PressureElevated:
		return structure.StateWarning, ReasonPressureElevated, "the node is holding back lower-priority work"
	}
	return structure.StateOK, "", "no pressure"
}

func seconds(d time.Duration) float64 { return d.Seconds() }
