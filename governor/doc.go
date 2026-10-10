// Package governor is the node-wide resource governor of
// docs/architecture/19-node-roles-and-resource-governance-model.md: the part
// that decides whether, and how much, a node role's work may run right now,
// so a node is never overwhelmed by work outside its own application's
// without its consent.
//
// It knows nothing about what the work is, and depends on no other
// Archipelago module. Its limits are cooperative and stated in terms Go can
// really enforce: how many run at once, how many may wait (and what happens
// to the overflow), how often they may start, and how long a run may take.
// It does not account for memory, and it does not preempt: a running piece of
// work is never killed to make room.
//
// Capacity is held at three nested levels, a node ceiling, an optional share
// per domain, and each node role's own budget, and work starts only when all
// three have room. The node ceiling is local configuration applied after
// whatever a domain's policy asked for: the narrowest scope caps the wider
// ones, the opposite direction to Policy's own clamp, and anything clamped is
// reported, never silently run with a different number.
package governor
