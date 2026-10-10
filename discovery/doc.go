// Package discovery is the bootstrap tier of peer discovery
// (docs/architecture/18-peer-discovery-and-capabilities-model.md): how a node
// with no configured peer location finds a first candidate on the local link.
//
// It announces one thing, "an Archipelago peer is here, speaks protocol
// versions min to max, at this port", as DNS-SD over mDNS
// (service _archipelago._tcp, the versions in a single TXT key). It does not
// announce a domain, a node role, a label or a capability: a node may sit in
// several domains and a passive observer on the link should not learn which,
// and the connection itself (mTLS, then the router's endpoint listing) is
// where those are learned and verified.
//
// **Everything Browse returns is a hint.** Anyone on the link can forge an
// announcement. A Candidate's only legitimate use is to make a node try a
// connection; trust is established by the handshake, never by this package.
// Browse therefore bounds what a hostile link can make it hold (candidates,
// addresses, field sizes) and discards anything malformed.
//
// The package depends on no other Archipelago module, so a node with no
// database access can announce and discover.
package discovery
