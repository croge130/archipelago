// Package registrynoderole is the first real node role
// (docs/architecture/19-node-roles-and-resource-governance-model.md,
// decision 7): registry membership. It is noderoles joined to the registry,
// which lives in Gatehouse-core (docs/architecture/13-registry-and-leases-model.md).
//
// The registry built the mechanism (register an Instance, heartbeat it, hold a
// lease) and left the background driver to each caller to remember. This is
// that driver, as a node role a node adopts: Membership registers the node's
// Instance, keeps its heartbeat fresh, re-registers if the row is lost, and
// deregisters on close. LeaseGate turns a registry lease into the Gate an
// exclusive node role needs, so "exactly one of these runs" is real instead of
// a comment.
//
// Both are ordinary node roles: they adopt with a stated budget, run inside the
// governor's limits, and report through the runtime's status like any other.
package registrynoderole
