package sdk

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/aliasauth"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitalsauth"
	"github.com/croge130/archipelago/vitalsdefaults"
)

// SeedStatus is the outcome of one seeding step.
type SeedStatus string

const (
	SeedRan     SeedStatus = "ran"
	SeedSkipped SeedStatus = "skipped"
	SeedFailed  SeedStatus = "failed"
)

// SeedStep is one step's outcome. Reason explains a skip; Err is set
// only for SeedFailed.
type SeedStep struct {
	Name   string
	Status SeedStatus
	Reason string
	Err    error
}

// SeedReport is every step Seed considered, in order. Steps whose modes
// are not enabled do not appear at all — absence means "not applicable",
// while SeedSkipped means "applicable, but this node cannot write".
type SeedReport []SeedStep

// Failed reports whether any step failed.
func (r SeedReport) Failed() bool {
	for _, s := range r {
		if s.Status == SeedFailed {
			return true
		}
	}
	return false
}

// Seed runs every idempotent startup step the enabled modes call for.
// It is the only SDK operation that writes, which is why it is separate
// from New: a read-only node never calls it, a scoped writer may be
// allowed some steps and not others, and a node with conditional access
// calls it when access is actually up.
//
// Steps are independent. A step needing a Writer the node lacks is
// reported SeedSkipped; a step that errors is reported SeedFailed and
// does not stop the others. The returned error joins every step's error
// and is nil when nothing failed; the report is always complete.
func (a *App) Seed(ctx context.Context) (SeedReport, error) {
	var report SeedReport
	var errs []error
	run := func(name string, canWrite bool, missing string, fn func() error) {
		if !canWrite {
			report = append(report, SeedStep{Name: name, Status: SeedSkipped, Reason: missing})
			return
		}
		if err := fn(); err != nil {
			report = append(report, SeedStep{Name: name, Status: SeedFailed, Err: err})
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		report = append(report, SeedStep{Name: name, Status: SeedRan})
	}

	gatehouseWritable := a.Gatehouse.Reader != nil && a.Gatehouse.Writer != nil
	if a.Modes.Alias && a.Modes.Gatehouse {
		run("aliasauth.RegisterPermissions", gatehouseWritable, "no Gatehouse Writer", func() error {
			return aliasauth.RegisterPermissions(ctx, a.Gatehouse.Reader, a.Gatehouse.Writer)
		})
	}
	if a.Modes.Vitals && a.Modes.Gatehouse {
		run("vitalsauth.RegisterPermissions", gatehouseWritable, "no Gatehouse Writer", func() error {
			return vitalsauth.RegisterPermissions(ctx, a.Gatehouse.Reader, a.Gatehouse.Writer)
		})
	}
	if a.Modes.Vitals {
		run("vitals.SeedBuiltinDefinitions", a.Vitals.Reader != nil && a.Vitals.Writer != nil, "no Vitals Writer", func() error {
			return vitalsFacade.SeedBuiltinDefinitions(ctx, a.Vitals.Reader, a.Vitals.Writer)
		})
	}
	if a.Modes.Vitals && a.Modes.Policy {
		run("vitalsdefaults.EnsurePolicyDefinition", a.Policy.Reader != nil && a.Policy.Writer != nil, "no Policy Writer", func() error {
			_, err := vitalsdefaults.EnsurePolicyDefinition(ctx, a.Policy.Reader, a.Policy.Writer, a.Config.Actor)
			return err
		})
	}

	return report, errors.Join(errs...)
}
