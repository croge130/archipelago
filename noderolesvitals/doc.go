// Package noderolesvitals joins node roles (noderoles) and Vitals: it reports
// what each adopted node role is really doing, as readings an operator can
// look at without reading logs.
//
// # What is reported
//
// One reading per adopted node role, under the definition
// archipelago.noderole.status, and one for the node's governor under
// archipelago.noderole.governor. A node role's reading says in its state
// whether it is healthy (ok), stopped by policy (disabled), losing work
// (warning: runs failed, work shed, or slots missed) or unable to run at all
// (failed: the node is no longer authorized). Its details carry the numbers:
// what the node adopted, what actually runs after the ceilings, every clamp,
// the governor's counters, each duty's counters and next slot, and any note
// policy left (a request capped, an interval raised to a duty's floor).
//
// # Readings are about change, not totals
//
// The counters in Status are cumulative, so a state judged on them would stay
// bad forever after one failure. The Reporter remembers the previous counters
// and judges the difference since the last report; a node role that had a bad
// minute is a warning for one report and ok again after. A node role that is
// re-adopted starts its counters from zero, which is read as a reset, not as a
// negative change.
//
// Vitals keeps a history row only when the state, impact, summary or reason
// code changes. Summaries and reason codes are therefore fixed phrases with no
// numbers in them, and the numbers go in the details, so a steady node writes
// one current row and no history.
//
// # Authority
//
// The Reporter writes through a Sink. NewDirectSink writes straight to the
// Vitals facade (a scoped or trusted node); a caller who has Gatehouse supplies
// a Sink that checks the node's principal first (the SDK does, with
// vitalsauth), so a node reports into its own scope only if it was granted
// permission to.
//
// # Not here
//
// Nothing is deleted when a node role is closed: its reading expires, which is
// what Vitals' ExpiresAt is for. There is no per-duty instance; duties appear
// in their node role's details.
package noderolesvitals
