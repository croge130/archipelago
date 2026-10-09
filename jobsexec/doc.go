// Package jobsexec is the executor side of remote job execution
// (docs/architecture/20-jobs-model.md, "Remote executors"): a node with
// handlers and no store access that pulls work from a director, runs it,
// heartbeats, and reports the outcome. It only ever calls its director.
//
// It depends on the router and nothing that touches Gatehouse-core or a
// database, so a node with no direct database access (17) can carry it.
// The wire types live here because both sides share them; the director
// side is the jobsdirector package.
package jobsexec
