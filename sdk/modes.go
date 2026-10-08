package sdk

// Modes names which bases a node uses. A mode gates which stores New
// requires and which steps Seed attempts; it is never a reason for this
// package to touch storage itself.
//
// Integrations are not modes: they switch on when the bases they combine
// are both enabled. Alias+Gatehouse enables aliasauth's permission
// registration, Vitals+Gatehouse enables vitalsauth's, Jobs+Gatehouse
// enables jobsauth's, and Vitals+Policy enables vitalsdefaults' policy
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
	// SSOProvider means this node issues SSO tickets. It requires
	// Gatehouse and Certs, plus Config.SSOSigner.
	SSOProvider bool
}
