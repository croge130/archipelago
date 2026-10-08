// Package structure is the jobs base's plain data: what a task kind is
// (TaskDefinition), what a job is (Job), and the small enums around
// them. No logic beyond field validation, no storage — see
// docs/architecture/20-jobs-model.md.
//
// A job is data, never code: it names a task kind and carries
// parameters validated against that kind's ParamSpecs. Nothing in this
// package can express a script, a command line, or anything a node
// would interpret as instructions, and that is deliberate.
//
// Principals are opaque UUIDs here, the same discipline vitals follows
// for ActorPrincipalID, so this base needs no Gatehouse-core. Whose
// authority a job runs under is recorded (AuthorityMode, RunAs) but
// decided by the jobsauth integration, not by this base.
package structure
