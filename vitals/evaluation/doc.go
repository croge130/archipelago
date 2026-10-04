// Package evaluation holds Vitals' pure decision logic: deriving
// quality warnings for a semantically odd (but not invalid) reading,
// deciding whether a new reading is a "notable" transition worth a
// history row, and enforcing a Definition's optional ValueMetadata
// shape against a Reading's Value. Nothing here touches a database —
// that's storage's job, wired together in facade.
package evaluation
