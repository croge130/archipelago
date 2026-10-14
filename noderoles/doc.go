// Package noderoles is the node role runtime of
// docs/architecture/19-node-roles-and-resource-governance-model.md: how a node
// adopts a node role and has its duties run automatically, inside a budget the
// node consented to.
//
// A Definition says what a node role is: its duties, what each tier of budget
// means for its work, the permissions its duties need, whether only one node
// may hold it. Adoption is the node's own decision (configuration, never
// database state, so a node with no database can adopt) and must state a
// budget; there is no silent default. The Runtime resolves that budget, has
// the node's authority checked, registers the node role with the node-wide
// governor (which applies the node's ceiling and reports what it capped), and
// gives the scheduler the duties, offering each due run to the governor.
//
// This package imports only the governor, the scheduler and recurrence. What a
// node role needs from the outside, whether the node's principal holds its
// permissions, whether an exclusive node role's lease is held, what a domain's
// Policy says about its schedule, arrives as small functions and interfaces
// that an integration (noderoles with Gatehouse-core, with the registry, with
// Policy) supplies.
//
// A node role whose adoption fails fails alone and loudly. It never takes the
// node, or another node role, down with it.
package noderoles
