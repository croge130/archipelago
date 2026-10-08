// Package jobsauth is the Layer 2 integration of the jobs base and
// Gatehouse-core: it decides who may submit, claim, read and cancel
// jobs, and whose authority a claimed job's operations run under. The
// jobs base itself stores and sequences jobs and knows nothing of
// principals' permissions — the same split vitals/vitalsauth uses. See
// docs/architecture/20-jobs-model.md and, for the assumed-session
// machinery this builds on, 09-gatehouse-core-model.md.
//
// Authorization is by context, with no second ACL for jobs: a job
// belongs to a queue, whose key is a context ID under the context type
// "jobs.queue", and the generic permissions jobs.submit, jobs.read,
// jobs.claim and jobs.cancel are each evaluated on that queue context.
// The owner of a job (the principal who submitted it) can always read
// and cancel it.
//
// Whose authority a job runs under is its AuthorityMode:
//
//   - owner: the effective principal is the requester, narrowed to the
//     task kind's declared scope. The executor needs permission to
//     execute as the owner; the owner needs nothing extra.
//   - service: the effective principal is the executor itself.
//   - assumed: a third principal; both consent edges are required, the
//     requester's at submission and again at claim, the executor's at
//     claim.
//
// Claiming mints an assumed session (when the effective principal is
// not the executor) bounded by the job's attempt timeout, and finishing
// or failing the attempt revokes it, so a stale executor's later
// authorized operations find it dead. Authorize is the one call a
// handler makes to check an operation: it requires the operation to fall
// inside the task kind's declared scope and to be permitted to the
// effective principal right now.
package jobsauth
