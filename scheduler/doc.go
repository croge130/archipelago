// Package scheduler is the local scheduler of
// docs/architecture/19-node-roles-and-resource-governance-model.md: the part
// that says *when* a duty is due and hands it, due, to something that decides
// whether it may run now (the resource governor, through a Dispatcher).
//
// It is in-process. It has no durable queue, no cross-node deduplication and
// no workflow: anything that must survive a restart or be retried belongs to
// the job system, which this feeds rather than replaces.
//
// A duty has a schedule (every so often, on a calendar, or once; see the
// recurrence package), optionally event triggers, and declares up front what
// happens when its previous run is still going (skip, queue one, or allow),
// when runs were missed while the node was down (skip, run once, or run each,
// bounded), and what to do when the dispatcher defers it.
//
// The scheduler does not run duties itself and does not bound them: it
// builds the function for one execution and offers it to the Dispatcher.
// Keeping those apart is what lets a node with no governor use a plain
// dispatcher in tests, and a node with one adopt its admission rules without
// the scheduler knowing them.
package scheduler
