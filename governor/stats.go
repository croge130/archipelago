package governor

import "sort"

// Counters are cumulative counts for one node role, for reporting (as Vitals
// readings, by the integration that has Vitals).
type Counters struct {
	Submitted     uint64
	Started       uint64
	Completed     uint64
	Failed        uint64
	Cancelled     uint64
	Coalesced     uint64
	ShedQueueFull uint64
	ShedDeferred  uint64
	ShedOther     uint64
}

// RoleStats is a snapshot for one node role.
type RoleStats struct {
	Key, Domain string
	Limits      Limits
	Running     int
	Waiting     int
	Counters
}

// Stats is a snapshot of the governor.
type Stats struct {
	Pressure Pressure
	Running  int
	Waiting  int
	Roles    []RoleStats
}

// Stats takes a snapshot.
func (g *Governor) Stats() Stats {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := Stats{Pressure: g.pressure, Running: g.node.running, Waiting: len(g.waiting)}
	for _, r := range g.roles {
		s.Roles = append(s.Roles, RoleStats{
			Key: r.spec.Key, Domain: r.spec.Domain, Limits: r.limits,
			Running: r.running, Waiting: g.waitingCount(r), Counters: r.counters,
		})
	}
	sort.Slice(s.Roles, func(i, j int) bool {
		if s.Roles[i].Domain != s.Roles[j].Domain {
			return s.Roles[i].Domain < s.Roles[j].Domain
		}
		return s.Roles[i].Key < s.Roles[j].Key
	})
	return s
}
