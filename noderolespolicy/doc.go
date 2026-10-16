// Package noderolespolicy joins node roles (noderoles) and Policy: it lets an
// operator steer how much a node role may use and how often its duties run,
// per domain, per node role or per duty, with the node's own adoption as the
// ceiling and each duty's designed MinInterval as the floor.
//
// # What policy can and cannot do
//
// Policy only ever narrows. The node's adoption (Adopted.Base) is the most a
// node role may use: a policy value above it is capped, and the cap is
// reported, never silent. A policy interval below a duty's MinInterval is
// raised to it, and reported. Two scopes that both speak resolve to the more
// restrictive value (minimum for budgets, maximum for intervals, boolean-and
// for enabled), because Policy's override mode allows only one non-global
// instance per definition and a per-role override is exactly what is wanted
// here. "Raise a role above what its node agreed to" is not a thing policy can
// do; that is the node's own decision, made at adoption.
//
// # Dimensions
//
// Each dimension is its own scalar policy key (all under
// archipelago.noderoles): budget.concurrency, budget.queue_depth,
// budget.run_timeout, schedule.interval and schedule.enabled. A governor Rate
// is a pair and merging its halves independently is meaningless, so rate is
// not yet policy-driven.
//
// # Applying
//
// A Binding resolves the policy for an adopted node role and applies the
// difference to the running node role. It remembers what it applied, so a
// refresh that finds nothing changed touches nothing (re-applying a schedule
// restarts its phase) and removing a policy value restores what the node
// adopted. If resolving fails, nothing is applied and the node role keeps
// running as it was.
package noderolespolicy
