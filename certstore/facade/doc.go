// Package facade composes Evaluation and Storage into certstore's own
// operations: submitting a CSR under the enrollment ceremony, an
// operator's confirm/reject decision, and cert revocation. Writer is
// declared here, its consumer, not in storage/dbstore — the same rule
// as gatehouse-core's and alias's own facade.Writer.
package facade
