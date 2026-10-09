// Package jobsdirector is the director side of remote job execution
// (docs/architecture/20-jobs-model.md, "Remote executors"): the routes a
// node with store access serves so that an executor with none can pull,
// heartbeat, complete, fail, abandon and ask for authorization on jobs.
//
// A director keeps nothing between calls. Each one names (job, attempt) and
// is checked against the store through jobsauth.Resume, and every consent
// decision is made against the executor's own verified principal, not the
// director's.
package jobsdirector
