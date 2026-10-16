package noderolesvitals

import (
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
)

type limitsDoc struct {
	Concurrency       int     `json:"concurrency"`
	QueueDepth        int     `json:"queue_depth"`
	RateStarts        int     `json:"rate_starts,omitempty"`
	RatePerSeconds    float64 `json:"rate_per_seconds,omitempty"`
	RunTimeoutSeconds float64 `json:"run_timeout_seconds,omitempty"`
}

func limitsOf(l governor.Limits) limitsDoc {
	return limitsDoc{
		Concurrency: l.Concurrency, QueueDepth: l.QueueDepth,
		RateStarts: l.Rate.Starts, RatePerSeconds: seconds(l.Rate.Per), RunTimeoutSeconds: seconds(l.RunTimeout),
	}
}

type dutyDoc struct {
	Name           string     `json:"name"`
	Disabled       bool       `json:"disabled"`
	InFlight       int        `json:"in_flight"`
	Fired          uint64     `json:"fired"`
	Completed      uint64     `json:"completed"`
	Failed         uint64     `json:"failed"`
	SkippedOverlap uint64     `json:"skipped_overlap"`
	Dropped        uint64     `json:"dropped"`
	Deferred       uint64     `json:"deferred"`
	MissedSlots    uint64     `json:"missed_slots"`
	LastSlot       *time.Time `json:"last_slot,omitempty"`
	NextSlot       *time.Time `json:"next_slot,omitempty"`
}

type governorDoc struct {
	Running       int    `json:"running"`
	Waiting       int    `json:"waiting"`
	Submitted     uint64 `json:"submitted"`
	Started       uint64 `json:"started"`
	Completed     uint64 `json:"completed"`
	Failed        uint64 `json:"failed"`
	Cancelled     uint64 `json:"cancelled"`
	Coalesced     uint64 `json:"coalesced"`
	ShedQueueFull uint64 `json:"shed_queue_full"`
	ShedDeferred  uint64 `json:"shed_deferred"`
	ShedOther     uint64 `json:"shed_other"`
}

type roleDoc struct {
	Effective   limitsDoc   `json:"effective"`
	Clamps      []string    `json:"clamps,omitempty"`
	PolicyNotes []string    `json:"policy_notes,omitempty"`
	Governor    governorDoc `json:"governor"`
	// SinceLastReport is what the state was judged on.
	SinceLastReport struct {
		Failed       uint64 `json:"failed"`
		Shed         uint64 `json:"shed"`
		MissedSlots  uint64 `json:"missed_slots"`
		Unauthorized uint64 `json:"unauthorized"`
	} `json:"since_last_report"`
	Unauthorized uint64    `json:"unauthorized_total"`
	NotHeld      uint64    `json:"not_held_total"`
	Duties       []dutyDoc `json:"duties"`
}

func roleDetails(st noderoles.RoleStatus, a assessment, policyNotes []string) roleDoc {
	g := st.Governor
	d := roleDoc{
		Effective:   limitsOf(st.Limits),
		PolicyNotes: policyNotes,
		Governor: governorDoc{
			Running: g.Running, Waiting: g.Waiting, Submitted: g.Submitted, Started: g.Started, Completed: g.Completed,
			Failed: g.Failed, Cancelled: g.Cancelled, Coalesced: g.Coalesced,
			ShedQueueFull: g.ShedQueueFull, ShedDeferred: g.ShedDeferred, ShedOther: g.ShedOther,
		},
		Unauthorized: st.Unauthorized, NotHeld: st.NotHeld,
	}
	for _, c := range st.Clamps {
		d.Clamps = append(d.Clamps, c.String())
	}
	d.SinceLastReport.Failed = a.delta.failed
	d.SinceLastReport.Shed = a.delta.shed
	d.SinceLastReport.MissedSlots = a.delta.missed
	d.SinceLastReport.Unauthorized = a.delta.unauthorized
	for _, ds := range st.Duties {
		dd := dutyDoc{
			Name: ds.Name, Disabled: ds.Disabled, InFlight: ds.InFlight, Fired: ds.Fired, Completed: ds.Completed,
			Failed: ds.Failed, SkippedOverlap: ds.SkippedOverlap, Dropped: ds.Dropped, Deferred: ds.Deferred,
			MissedSlots: ds.MissedSlots,
		}
		if !ds.LastSlot.IsZero() {
			t := ds.LastSlot
			dd.LastSlot = &t
		}
		if !ds.NextSlot.IsZero() {
			t := ds.NextSlot
			dd.NextSlot = &t
		}
		d.Duties = append(d.Duties, dd)
	}
	return d
}

type governorStatsDoc struct {
	Pressure string `json:"pressure"`
	Running  int    `json:"running"`
	Waiting  int    `json:"waiting"`
	Roles    int    `json:"node_roles"`
}

func governorDetails(st governor.Stats) governorStatsDoc {
	return governorStatsDoc{Pressure: st.Pressure.String(), Running: st.Running, Waiting: st.Waiting, Roles: len(st.Roles)}
}
