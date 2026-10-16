package sdk

// Modes names which bases a node uses. A mode gates which stores New
// requires and which steps Seed attempts; it is never a reason for this
// package to touch storage itself.
//
// Integrations are not modes: they switch on when the bases they combine
// are both enabled. Alias+Gatehouse enables aliasauth's permission
// registration, Vitals+Gatehouse enables vitalsauth's, Jobs+Gatehouse
// enables jobsauth's, NodeRoles+Gatehouse enables noderolesauth's,
// NodeRoles+Policy enables noderolespolicy's definitions and applies Policy to
// each adopted node role, NodeRoles+Vitals enables noderolesvitals' definitions
// and the reporter of node role status, and Vitals+Policy enables vitalsdefaults' policy
// definition. A node can therefore run
// Vitals without Gatehouse-core (a scoped node) and simply gets the
// narrower set of seeding steps.
type Modes struct {
	Gatehouse bool
	Policy    bool
	Alias     bool
	Vitals    bool
	Jobs      bool
	Certs     bool
	// NodeRoles means this node adopts node roles in this domain
	// (19-node-roles-and-resource-governance-model.md). It needs no store of
	// its own, but it does need Config.NodeRoles: a node-wide governor, a
	// domain name, and, if Gatehouse is on, the node's principal. With
	// Gatehouse on, every adoption is authorized against that principal's
	// grants; without it, a node role that needs permissions is refused.
	NodeRoles bool
	// SSOProvider means this node issues SSO tickets. It requires
	// Gatehouse and Certs, plus Config.SSOSigner.
	SSOProvider bool
}
