// Package facade composes Evaluation and Storage into Policy's own
// write-side operations — EnsurePolicyDefinition, EnsurePolicyContext,
// context membership, and SetPolicyInstance/ArchivePolicyInstance.
// Pure reads (Resolve) stay in evaluation and are called directly by
// consumers holding a Reader, the same split every other base in this
// design uses.
package facade
