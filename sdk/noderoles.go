package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/noderolesauth"
	"github.com/croge130/archipelago/noderolespolicy"
	"github.com/croge130/archipelago/registrynoderole"
	"github.com/croge130/archipelago/scheduler"
	"github.com/google/uuid"
)

// NodeRoleConfig is what the NodeRoles mode needs.
type NodeRoleConfig struct {
	// Governor is the node-wide resource governor. There is one per node,
	// created by the process and handed to the App of every domain on it, so
	// two domains cannot each take their share and together exceed what the
	// node agreed to. The process owns it; an App never closes it.
	Governor *governor.Governor
	// Domain names this App's domain to the governor, for its share of the
	// node ceiling.
	Domain string
	// PrincipalID is the principal this node authenticates as in the domain.
	// With the Gatehouse mode on, a node role is adopted only if this
	// principal holds the permissions it needs, and each run records it as
	// the actor.
	PrincipalID uuid.UUID

	// Definitions are the node roles this node expects to adopt. Seed
	// registers the permissions they name, since adopting needs them to exist.
	Definitions []noderoles.Definition
	// AllowReservedNamespace lets Seed register permissions under a reserved
	// namespace, which core node roles use.
	AllowReservedNamespace bool

	// Record, Clock, Rand and Logger are passed to the runtime; the defaults
	// (an in-memory catch-up record, the real clock) suit production.
	Record scheduler.Record
	Clock  scheduler.Clock
	Rand   func() float64
	Logger *slog.Logger
}

var (
	// ErrNodeRolesDisabled: the NodeRoles mode is not on.
	ErrNodeRolesDisabled = errors.New("sdk: the NodeRoles mode is not enabled")
	// ErrCannotAuthorize: a node role needs permissions and this App has no
	// Gatehouse mode to check them with. It is refused rather than run
	// unchecked.
	ErrCannotAuthorize = errors.New("sdk: this App cannot check the permissions the node role needs")
)

// NodeRoles is this App's node role runtime, created on first use from
// Config.NodeRoles and the same one every time after. Creating it starts
// nothing and touches no store; start it with Start when the node is ready.
func (a *App) NodeRoles() (*noderoles.Runtime, error) {
	if !a.Modes.NodeRoles {
		return nil, ErrNodeRolesDisabled
	}
	a.nodeRolesOnce.Do(func() {
		nr := a.Config.NodeRoles
		a.nodeRoles, a.nodeRolesErr = noderoles.New(noderoles.Config{
			Governor: nr.Governor, Domain: nr.Domain, Record: nr.Record, Clock: nr.Clock, Rand: nr.Rand, Logger: nr.Logger,
		})
	})
	return a.nodeRoles, a.nodeRolesErr
}

// AdoptNodeRole adopts a node role in this domain. With the Gatehouse mode on
// it always binds the node's authority first: the adoption is authorized
// against Config.NodeRoles.PrincipalID's grants, now and before every run, and
// each run records who it ran as. A caller cannot adopt around that, and an
// Authorize or Annotate it supplied is replaced. Without the Gatehouse mode a
// node role that needs permissions is refused (ErrCannotAuthorize), because
// they cannot be checked.
func (a *App) AdoptNodeRole(ctx context.Context, def noderoles.Definition, adoption noderoles.Adoption) (*noderoles.Adopted, error) {
	rt, err := a.NodeRoles()
	if err != nil {
		return nil, err
	}
	if a.Modes.Gatehouse {
		adoption = noderolesauth.Bind(adoption, a.Gatehouse.Reader, a.Config.NodeRoles.PrincipalID)
	} else if len(def.Permissions) > 0 {
		return nil, fmt.Errorf("%w: %s needs %v", ErrCannotAuthorize, def.Key, def.Permissions)
	}
	adopted, err := rt.Adopt(ctx, def, adoption)
	if err != nil {
		return nil, err
	}
	if a.Modes.Policy {
		// The adoption stands whatever policy says or fails to say: it is the
		// node's own decision and the safe fallback. A failure is logged, and
		// the refresh node role (or ApplyNodeRolePolicy) retries.
		if _, perr := a.ApplyNodeRolePolicy(ctx, adopted); perr != nil {
			a.nodeRoleLogger().Warn("node role adopted, but its policy could not be applied; it runs as adopted",
				"role", def.Key, "error", perr)
		}
	}
	return adopted, nil
}

func (a *App) nodeRoleLogger() *slog.Logger {
	if l := a.Config.NodeRoles.Logger; l != nil {
		return l
	}
	return slog.Default()
}

// NodeRolePolicy is this App's binding of Policy to its node roles, created on
// first use. It needs the NodeRoles and Policy modes.
func (a *App) NodeRolePolicy() (*noderolespolicy.Binding, error) {
	if !a.Modes.NodeRoles || !a.Modes.Policy {
		return nil, errors.New("sdk: node role policy needs both the NodeRoles and Policy modes enabled")
	}
	a.nodeRolePolicyOnce.Do(func() {
		nr := a.Config.NodeRoles
		cfg := noderolespolicy.BindingConfig{Store: a.Policy.Reader, Domain: nr.Domain, Logger: nr.Logger}
		if nr.Clock != nil {
			cfg.Clock = nr.Clock.Now
		}
		a.nodeRolePolicy, a.nodeRolePolicyErr = noderolespolicy.NewBinding(cfg)
	})
	return a.nodeRolePolicy, a.nodeRolePolicyErr
}

// ApplyNodeRolePolicy resolves Policy for an adopted node role and brings it
// in line: a budget and schedule within what the node adopted, honouring each
// duty's floor. Nothing is applied if Policy cannot be read.
func (a *App) ApplyNodeRolePolicy(ctx context.Context, adopted *noderoles.Adopted) (noderolespolicy.Report, error) {
	b, err := a.NodeRolePolicy()
	if err != nil {
		return noderolespolicy.Report{}, err
	}
	return b.Apply(ctx, adopted)
}

// NodeRolePolicyRefresh returns the node role that re-applies Policy to every
// adopted node role each interval, so an edit reaches running node roles. Adopt
// it with AdoptNodeRole like any other.
func (a *App) NodeRolePolicyRefresh(every time.Duration) (noderoles.Definition, error) {
	rt, err := a.NodeRoles()
	if err != nil {
		return noderoles.Definition{}, err
	}
	b, err := a.NodeRolePolicy()
	if err != nil {
		return noderoles.Definition{}, err
	}
	anchor := time.Now()
	if c := a.Config.NodeRoles.Clock; c != nil {
		anchor = c.Now()
	}
	return noderolespolicy.RefreshRole(rt, b, every, anchor)
}

// Membership builds the registry membership node role over this App's own
// Gatehouse stores and the node's principal, ready to adopt with
// AdoptNodeRole(ctx, m.Definition(), ...). It needs the NodeRoles and Gatehouse
// modes and a Gatehouse Writer, since it registers and heartbeats an Instance.
func (a *App) Membership(group string, metadata json.RawMessage, heartbeatEvery time.Duration) (*registrynoderole.Membership, error) {
	if !a.Modes.NodeRoles || !a.Modes.Gatehouse {
		return nil, errors.New("sdk: Membership needs both the NodeRoles and Gatehouse modes enabled")
	}
	if a.Gatehouse.Writer == nil {
		return nil, errors.New("sdk: Membership needs a Gatehouse Writer: it registers and heartbeats an Instance")
	}
	return registrynoderole.NewMembership(registrynoderole.MembershipConfig{
		Reader: a.Gatehouse.Reader, Writer: a.Gatehouse.Writer, PrincipalID: a.Config.NodeRoles.PrincipalID,
		Group: group, Metadata: metadata, HeartbeatEvery: heartbeatEvery, Logger: a.Config.NodeRoles.Logger,
	})
}
