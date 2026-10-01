// Package structure holds certstore's plain data types: Enrollment (the
// list-then-confirm ceremony's pending/decided state) and Cert (an
// issued certificate's record). Grounded directly in
// docs/architecture/05-pki-and-signing.md.
//
// The Signer abstraction (software key / HSM / TPM-sealed / YubiKey)
// isn't here — it's behavior, not data, and belongs to the Evaluation
// layer in a later round, once the actual CA integration is verified
// rather than assumed.
package structure
