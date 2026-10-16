package noderolesvitals

import (
	"testing"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/scheduler"
	"github.com/croge130/archipelago/vitals/structure"
)

func status(mut func(*noderoles.RoleStatus)) noderoles.RoleStatus {
	s := noderoles.RoleStatus{Key: "myapp.sweeper", Duties: []scheduler.DutyStats{{Name: "sweep"}}}
	if mut != nil {
		mut(&s)
	}
	return s
}

func TestAssessJudgesTheChangeSinceTheLastReportNotTheTotal(t *testing.T) {
	failed := status(func(s *noderoles.RoleStatus) { s.Governor.Counters = governor.Counters{Failed: 3} })

	if a := assess(failed, nil, nil); a.state != structure.StateWarning || a.reason != ReasonRunsFailed || a.delta.failed != 3 {
		t.Fatalf("first report with failures: %+v", a)
	}
	prev := countersOf(failed)
	if a := assess(failed, &prev, nil); a.state != structure.StateOK || a.reason != "" {
		t.Fatalf("nothing new since the last report, yet %+v", a)
	}
	more := status(func(s *noderoles.RoleStatus) { s.Governor.Counters = governor.Counters{Failed: 4} })
	if a := assess(more, &prev, nil); a.state != structure.StateWarning || a.delta.failed != 1 {
		t.Fatalf("one more failure: %+v", a)
	}
}

func TestAssessReadsALowerCountAsAReAdoptionNotANegativeChange(t *testing.T) {
	prev := counters{failed: 10, shed: 10, missed: 10, unauthorized: 10}
	fresh := status(func(s *noderoles.RoleStatus) {
		s.Governor.Counters = governor.Counters{Failed: 2, ShedQueueFull: 1}
		s.Unauthorized = 1
	})
	a := assess(fresh, &prev, nil)
	if a.delta.failed != 2 || a.delta.shed != 1 || a.delta.unauthorized != 1 {
		t.Fatalf("a restart from zero is all new: %+v", a.delta)
	}
	if got := since(5, 5); got != 0 {
		t.Fatalf("equal counts: %d", got)
	}
}

func TestAssessPrecedenceFromWorstToBenign(t *testing.T) {
	everything := status(func(s *noderoles.RoleStatus) {
		s.Governor.Counters = governor.Counters{Failed: 1, ShedQueueFull: 1}
		s.Unauthorized = 1
		s.Duties = []scheduler.DutyStats{{Name: "sweep", MissedSlots: 1}}
		s.Clamps = []governor.Clamp{{Dimension: "concurrency", Requested: "4", Effective: "2", By: "domain"}}
	})
	notes := []string{"a note"}
	steps := []struct {
		name  string
		mut   func(*noderoles.RoleStatus)
		state structure.State
		code  string
	}{
		{"every duty off wins", func(s *noderoles.RoleStatus) { s.Duties[0].Disabled = true }, structure.StateDisabled, ReasonDisabled},
		{"then not authorized", func(s *noderoles.RoleStatus) { s.Duties[0].Disabled = false }, structure.StateFailed, ReasonNotAuthorized},
		{"then failed runs", func(s *noderoles.RoleStatus) { s.Unauthorized = 0 }, structure.StateWarning, ReasonRunsFailed},
		{"then shed work", func(s *noderoles.RoleStatus) { s.Governor.Failed = 0 }, structure.StateWarning, ReasonWorkShed},
		{"then missed slots", func(s *noderoles.RoleStatus) { s.Governor.ShedQueueFull = 0 }, structure.StateWarning, ReasonSlotsMissed},
		{"then a clamp, which is still ok", func(s *noderoles.RoleStatus) { s.Duties[0].MissedSlots = 0 }, structure.StateOK, ReasonClamped},
		{"then a policy note, still ok", func(s *noderoles.RoleStatus) { s.Clamps = nil }, structure.StateOK, ReasonPolicyNote},
	}
	cur := everything
	cur.Duties = append([]scheduler.DutyStats(nil), everything.Duties...)
	for _, st := range steps {
		st.mut(&cur)
		a := assess(cur, nil, notes)
		if a.state != st.state || a.reason != st.code {
			t.Fatalf("%s: got %s/%s, want %s/%s", st.name, a.state, a.reason, st.state, st.code)
		}
	}
	if a := assess(cur, nil, nil); a.state != structure.StateOK || a.reason != "" || a.summary == "" {
		t.Fatalf("a node role with nothing to say: %+v", a)
	}
}

func TestOnlyWhenEveryDutyIsOffIsTheRoleDisabled(t *testing.T) {
	s := status(func(s *noderoles.RoleStatus) {
		s.Duties = []scheduler.DutyStats{{Name: "a", Disabled: true}, {Name: "b"}}
	})
	if a := assess(s, nil, nil); a.state == structure.StateDisabled {
		t.Fatal("one duty still running, yet the role is reported disabled")
	}
	if a := assess(noderoles.RoleStatus{Key: "no.duties"}, nil, nil); a.state == structure.StateDisabled {
		t.Fatal("a node role with no duties is not disabled")
	}
}

func TestSummariesAndReasonsCarryNoNumbersSoSteadyNodesWriteNoHistory(t *testing.T) {
	s := status(func(s *noderoles.RoleStatus) {
		s.Governor.Counters = governor.Counters{Failed: 7, ShedQueueFull: 9}
		s.Duties[0].MissedSlots = 11
	})
	for _, p := range []*counters{nil, {}} {
		a := assess(s, p, nil)
		for _, r := range a.summary + a.reason {
			if r >= '0' && r <= '9' {
				t.Fatalf("a digit in %q / %q would make every report a history row", a.summary, a.reason)
			}
		}
	}
}

func TestGovernorPressureMapsToState(t *testing.T) {
	for _, tc := range []struct {
		p     governor.Pressure
		state structure.State
		code  string
	}{
		{governor.PressureNone, structure.StateOK, ""},
		{governor.PressureElevated, structure.StateWarning, ReasonPressureElevated},
		{governor.PressureHigh, structure.StateDegraded, ReasonPressureHigh},
	} {
		if state, code, summary := assessGovernor(tc.p); state != tc.state || code != tc.code || summary == "" {
			t.Errorf("%v: %s/%s/%q", tc.p, state, code, summary)
		}
	}
}

func TestEveryKindOfShedWorkCounts(t *testing.T) {
	for name, c := range map[string]governor.Counters{
		"queue full": {ShedQueueFull: 1},
		"deferred":   {ShedDeferred: 1},
		"other":      {ShedOther: 1},
	} {
		s := status(func(s *noderoles.RoleStatus) { s.Governor.Counters = c })
		if a := assess(s, nil, nil); a.reason != ReasonWorkShed {
			t.Errorf("%s shed work was not noticed: %+v", name, a)
		}
	}
}
