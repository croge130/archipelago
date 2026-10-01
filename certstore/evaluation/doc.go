// Package evaluation holds certstore's decision and signing logic: the
// enrollment ceremony's state transitions, the embedded CA's actual
// signing operation, and the conversion from a signed chain into a
// structure.Cert record. Nothing here talks to a database — that's
// storage's job, wired together in facade.
package evaluation
