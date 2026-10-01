package transit

// PeerIdentity is the verified transport-level identity of the peer,
// populated from a presented mTLS client certificate. Present=false
// means no client cert was offered — the zero value. Transit surfaces
// this and nothing more: whether a cert is a credential, which
// principal it binds, and how it composes with other credentials is
// the later mTLS integration's concern (certstore + Gatehouse-core +
// Transit), never decided here.
type PeerIdentity struct {
	Present     bool
	Fingerprint string
	Subject     string
	SANs        []string
}

// Capabilities describes what a Session's backend can actually do —
// exposed for logging and for Transit's own realization choices,
// never for handler branching. A caller that varies behavior on
// Capabilities is reaching for a narrow accessor that doesn't exist
// yet, not a reason to add one speculatively before a second backend
// exists to test it against.
type Capabilities struct {
	NativeStreams   bool // independent network-level streams (no cross-channel head-of-line blocking)
	Datagrams       bool // real unreliable datagrams
	MaxDatagramSize int
}
