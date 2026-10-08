// Package facade is the jobs base's convenience layer: registering a
// task kind, submitting a job, reporting a failure, cancelling. It holds
// only functions with behavior of their own — defaults, validation,
// idempotency, error classification. Operations that would be a bare
// pass-through (claim, heartbeat, complete) are not re-wrapped; callers
// use the Writer directly, as registry/doc.go decided for the same
// reason.
//
// Nothing here authorizes anything. Who may submit, claim, read or
// cancel, and whose permissions a job's operations run under, belongs to
// the jobsauth integration (jobs + Gatehouse-core), the same split
// vitals/vitalsauth uses. See docs/architecture/20-jobs-model.md.
package facade
