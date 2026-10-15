// Package noderolesauth joins the node role runtime to Gatehouse-core
// (docs/architecture/19-node-roles-and-resource-governance-model.md, "Whose
// authority duties run under"). It supplies the three things the runtime asks
// of its surroundings and cannot supply itself:
//
//   - Authorize: does the node's principal hold the permissions a node role
//     needs? Checked at adoption and before every run, it fails closed, and
//     it names every permission that is missing, not just the first.
//   - Authority: for each run, who it ran as and why, in the three-identity
//     shape of docs/architecture/09-gatehouse-core-model.md (actor, requested
//     by, authority source). A run a scheduler triggered is a scheduled_task;
//     one an event triggered is a service_action; both under the node's own
//     standing authority, with the node role and duty as the reference. The
//     duty reads it from its context and passes it on to whatever it writes.
//   - RegisterPermissions: a node role declares the permissions it needs and
//     registers them like any other, so the check has something to find.
//
// recovery_elevation is never available to a node role: every check here is at
// standard authority, and nothing in this package can ask for more.
package noderolesauth
