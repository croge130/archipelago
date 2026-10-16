package sdk

import (
	"context"
	"errors"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/noderolesvitals"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	vitalsStructure "github.com/croge130/archipelago/vitals/structure"
	"github.com/croge130/archipelago/vitalsauth"
	"github.com/google/uuid"
)

// authorizedVitalsSink writes through vitalsauth as the node's principal, so a
// node reports into its own scope only if it was granted permission to write
// there.
type authorizedVitalsSink struct {
	gatehouse evaluation.Store
	reader    vitalsFacade.Reader
	writer    vitalsFacade.Writer
	principal uuid.UUID
}

func (s authorizedVitalsSink) EnsureInstance(ctx context.Context, i vitalsStructure.Instance) (vitalsStructure.Instance, error) {
	return vitalsauth.EnsureInstance(ctx, s.gatehouse, s.reader, s.writer, s.principal, i)
}

func (s authorizedVitalsSink) WriteReading(ctx context.Context, r vitalsStructure.Reading) (vitalsStructure.Reading, error) {
	return vitalsauth.WriteReading(ctx, s.gatehouse, s.reader, s.writer, s.principal, r)
}

// NodeRoleVitals is this App's reporter of node role status into Vitals,
// created on first use. It needs the NodeRoles and Vitals modes and a Vitals
// writer. With Gatehouse on, readings are written as the node's principal and
// need its permission to write in the scope ("node", the principal's ID);
// without it they are written directly. With Policy on, what policy could not
// apply as asked appears in each node role's reading.
func (a *App) NodeRoleVitals() (*noderolesvitals.Reporter, error) {
	if !a.Modes.NodeRoles || !a.Modes.Vitals {
		return nil, errors.New("sdk: node role vitals needs both the NodeRoles and Vitals modes enabled")
	}
	if a.Vitals.Writer == nil {
		return nil, errors.New("sdk: node role vitals needs a Vitals Writer: it registers instances and writes readings")
	}
	a.nodeRoleVitalsOnce.Do(func() {
		rt, err := a.NodeRoles()
		if err != nil {
			a.nodeRoleVitalsErr = err
			return
		}
		nr := a.Config.NodeRoles
		cfg := noderolesvitals.Config{
			Runtime: rt, Governor: nr.Governor, Definitions: a.Vitals.Reader,
			Sink: noderolesvitals.NewDirectSink(a.Vitals.Reader, a.Vitals.Writer), ScopeID: nr.Domain, Logger: nr.Logger,
		}
		if nr.Clock != nil {
			cfg.Clock = nr.Clock.Now
		}
		if nr.PrincipalID != uuid.Nil {
			id := nr.PrincipalID
			cfg.ScopeID, cfg.Actor = id.String(), &id
		}
		if a.Modes.Gatehouse {
			cfg.Sink = authorizedVitalsSink{gatehouse: a.Gatehouse.Reader, reader: a.Vitals.Reader, writer: a.Vitals.Writer, principal: nr.PrincipalID}
		}
		if a.Modes.Policy {
			b, err := a.NodeRolePolicy()
			if err != nil {
				a.nodeRoleVitalsErr = err
				return
			}
			cfg.Policy = b
		}
		a.nodeRoleVitals, a.nodeRoleVitalsErr = noderolesvitals.New(cfg)
	})
	return a.nodeRoleVitals, a.nodeRoleVitalsErr
}

// NodeRoleVitalsReport returns the node role that reports the status of every
// adopted node role to Vitals each interval. Adopt it with AdoptNodeRole like
// any other.
func (a *App) NodeRoleVitalsReport(every time.Duration) (noderoles.Definition, error) {
	rep, err := a.NodeRoleVitals()
	if err != nil {
		return noderoles.Definition{}, err
	}
	anchor := time.Now()
	if c := a.Config.NodeRoles.Clock; c != nil {
		anchor = c.Now()
	}
	return rep.Definition(every, anchor)
}
