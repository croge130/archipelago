// Package recurrence answers one question, "when is the next occurrence after
// t?", for the three shapes of schedule the design needs: every so often
// (Every), at wall-clock times on a calendar (Calendar), and once (Once). It
// is shared by the local scheduler and by recurring jobs
// (docs/architecture/19-node-roles-and-resource-governance-model.md and
// 20-jobs-model.md), so the two cannot disagree about it.
//
// # Calendar schedules follow the wall clock, with one rule for DST
//
// A calendar expression ("30 2 * * *") is matched against civil time: the
// date and clock a person on that wall would read. The cron library is only
// ever asked about civil time, where daylight saving does not exist, and this
// package maps each result to a real instant under a single stated rule:
//
//   - A civil time that exists once is that instant.
//   - A civil time that happens twice (clocks go back) fires once, at the
//     first occurrence. The repeated hour does not run a second time.
//   - A civil time that never happens (clocks go forward) is not skipped: it
//     fires at the first instant after the gap, so 02:30 in a spring-forward
//     gap fires at 03:00. Several nonexistent times in one gap are one
//     instant, so a schedule denser than the gap fires once for the gap, not
//     once per missing slot.
//
// Consequently a calendar schedule fires every day it matches, exactly once.
// It also means "every hour at :00" is evaluated on the wall clock and has a
// two-hour gap across a fall-back night. For "every N hours of real elapsed
// time", use Every, which knows nothing of calendars or zones.
//
// Next is strictly increasing: asked from an occurrence it never returns that
// occurrence again, including from inside a repeated hour.
package recurrence
