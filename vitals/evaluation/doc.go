// Package evaluation holds Vitals' pure decision logic: deriving
// quality warnings for a semantically odd (but not invalid) reading,
// and deciding whether a new reading is a "notable" transition worth
// a history row. Nothing here touches a database — that's storage's
// job, wired together in facade.
package evaluation
