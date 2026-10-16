package sdk

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	certstoreEvaluation "github.com/croge130/archipelago/certstore/evaluation"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/noderolespolicy"
	"github.com/google/uuid"
)

// ErrMissingStore is wrapped by every error New returns for a mode whose
// required store, or required configuration, was not supplied.
var ErrMissingStore = errors.New("sdk: required store or configuration missing for an enabled mode")

// Config is the non-store configuration the SDK needs.
type Config struct {
	// Actor is recorded on Policy writes made during Seed (the actor
	// string vitalsdefaults.EnsurePolicyDefinition takes). Defaults to
	// "sdk".
	Actor string

	// SSOSigner is the ticket-signing key for the SSOProvider mode. The
	// SDK never generates or stores this key itself — 12-sso-tickets-
	// model.md requires it be distinct from any mTLS key.
	SSOSigner certstoreEvaluation.Signer

	// NodeRoles configures node role adoption; required by the NodeRoles
	// mode, ignored otherwise.
	NodeRoles *NodeRoleConfig
}

// App is a validated set of stores plus the modes they were validated
// against. Stores' fields are promoted, so callers write
// app.Gatehouse.Reader and hand it to whichever integration they use.
type App struct {
	Stores
	Modes  Modes
	Config Config

	nodeRolesOnce sync.Once
	nodeRoles     *noderoles.Runtime
	nodeRolesErr  error

	nodeRolePolicyOnce sync.Once
	nodeRolePolicy     *noderolespolicy.Binding
	nodeRolePolicyErr  error
}

// New validates stores against modes and returns an App. It performs no
// storage I/O of any kind — it never calls a method on a store — so a
// node with conditional access can build an App before its stores are
// reachable. Every problem found is reported together, each naming the
// mode that needs it, and the returned error wraps ErrMissingStore.
func New(stores Stores, modes Modes, cfg Config) (*App, error) {
	var problems []string
	need := func(ok bool, mode, what string) {
		if !ok {
			problems = append(problems, fmt.Sprintf("%s mode requires %s", mode, what))
		}
	}

	if modes.Gatehouse {
		need(stores.Gatehouse.Reader != nil, "Gatehouse", "Stores.Gatehouse.Reader")
	}
	if modes.Policy {
		need(stores.Policy.Reader != nil, "Policy", "Stores.Policy.Reader")
	}
	if modes.Alias {
		need(stores.Alias.Reader != nil, "Alias", "Stores.Alias.Reader")
	}
	if modes.Vitals {
		need(stores.Vitals.Reader != nil, "Vitals", "Stores.Vitals.Reader")
	}
	if modes.Jobs {
		need(stores.Jobs.Reader != nil, "Jobs", "Stores.Jobs.Reader")
	}
	if modes.Certs {
		need(stores.Certs.Reader != nil, "Certs", "Stores.Certs.Reader")
	}
	if modes.NodeRoles {
		nr := cfg.NodeRoles
		need(nr != nil, "NodeRoles", "Config.NodeRoles")
		if nr != nil {
			need(nr.Governor != nil, "NodeRoles", "Config.NodeRoles.Governor (one per node, shared by every App)")
			need(nr.Domain != "", "NodeRoles", "Config.NodeRoles.Domain")
			if modes.Gatehouse {
				need(nr.PrincipalID != uuid.Nil, "NodeRoles", "Config.NodeRoles.PrincipalID (with Gatehouse on, the node's authority is checked)")
			}
		}
	}
	if modes.SSOProvider {
		need(modes.Gatehouse, "SSOProvider", "the Gatehouse mode")
		need(modes.Certs, "SSOProvider", "the Certs mode")
		need(cfg.SSOSigner != nil, "SSOProvider", "Config.SSOSigner")
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissingStore, strings.Join(problems, "; "))
	}

	if cfg.Actor == "" {
		cfg.Actor = "sdk"
	}
	return &App{Stores: stores, Modes: modes, Config: cfg}, nil
}
