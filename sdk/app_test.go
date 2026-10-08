package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	"github.com/google/uuid"
)

// The fakes embed a nil interface, so any method call on them panics.
// That makes "New and Seed-skip paths perform no storage I/O" a property
// the tests below prove by merely not panicking.
type nilGatehouseReader struct{ gatehouseFacade.Reader }
type nilJobsReader struct{ jobsFacade.Reader }

func gatehouseOnly() Stores {
	return Stores{Gatehouse: GatehouseStores{Reader: nilGatehouseReader{}}}
}

func TestNewPerformsNoStoreIO(t *testing.T) {
	// Every store is a nil-embedding fake; New must not call any of them.
	app, err := New(gatehouseOnly(), Modes{Gatehouse: true}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Config.Actor != "sdk" {
		t.Errorf("Actor default = %q, want %q", app.Config.Actor, "sdk")
	}
}

func TestNewNamesEveryMissingStoreTogether(t *testing.T) {
	_, err := New(Stores{}, Modes{Gatehouse: true, Alias: true, Vitals: true, Policy: true, Certs: true}, Config{})
	if !errors.Is(err, ErrMissingStore) {
		t.Fatalf("err = %v, want ErrMissingStore", err)
	}
	for _, want := range []string{"Gatehouse", "Alias", "Vitals", "Policy", "Certs"} {
		if !strings.Contains(err.Error(), want+" mode requires") {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

func TestNewAcceptsScopedNodeWithoutGatehouse(t *testing.T) {
	// A node holding no Gatehouse-core access can still enable modes that
	// don't need it — the scoped-access case from 17-sdk-model.md. Here
	// nothing is enabled that needs a store at all.
	if _, err := New(Stores{}, Modes{}, Config{}); err != nil {
		t.Fatalf("New with no modes: %v", err)
	}
}

func TestNewRejectsSSOProviderWithoutPrerequisites(t *testing.T) {
	_, err := New(gatehouseOnly(), Modes{Gatehouse: true, SSOProvider: true}, Config{})
	if !errors.Is(err, ErrMissingStore) {
		t.Fatalf("err = %v, want ErrMissingStore", err)
	}
	for _, want := range []string{"the Certs mode", "Config.SSOSigner"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestSeedSkipsEveryStepWhenNodeIsReadOnly(t *testing.T) {
	// Alias+Vitals+Gatehouse enabled, but no Writers anywhere: every
	// applicable step must be reported skipped, none attempted (the nil-
	// embedding fakes would panic if any were).
	stores := Stores{Gatehouse: GatehouseStores{Reader: nilGatehouseReader{}}}
	app := &App{Stores: stores, Modes: Modes{Gatehouse: true, Alias: true, Vitals: true}, Config: Config{Actor: "sdk"}}
	// Vitals/Alias readers are nil here, so only the Gatehouse-dependent
	// steps and the Vitals base step are in play — all must skip.
	report, err := app.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(report) != 4 {
		t.Fatalf("report has %d steps, want 4: %+v", len(report), report)
	}
	for _, step := range report {
		if step.Status != SeedSkipped {
			t.Errorf("step %s status = %s, want skipped", step.Name, step.Status)
		}
		if step.Reason == "" {
			t.Errorf("step %s skipped without a reason", step.Name)
		}
	}
	if report.Failed() {
		t.Error("report.Failed() true for an all-skipped report")
	}
}

func TestSeedOmitsStepsForDisabledModes(t *testing.T) {
	// Gatehouse alone: only its own step applies. Alias, Vitals and
	// Policy steps must not appear at all.
	app, err := New(gatehouseOnly(), Modes{Gatehouse: true}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	report, err := app.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(report) != 1 || report[0].Name != "gatehouse.RegisterAssumePermissions" || report[0].Status != SeedSkipped {
		t.Fatalf("report = %+v; want exactly the gatehouse step, skipped (no Writer)", report)
	}

	// No modes at all: nothing applies.
	none, err := New(Stores{}, Modes{}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if report, err := none.Seed(context.Background()); err != nil || len(report) != 0 {
		t.Fatalf("Seed with no modes = %+v, %v; want empty report and nil error", report, err)
	}
}

// errReader fails GetPermissionDefinition the way a narrowed or
// unreachable store should: with an error, not an empty result.
type errReader struct {
	gatehouseFacade.Reader
	err error
}

func (r errReader) GetPermissionDefinition(context.Context, string) (gatehouseStructure.PermissionDefinition, bool, error) {
	return gatehouseStructure.PermissionDefinition{}, false, r.err
}

func TestStoreErrorsReachTheCallerInsteadOfBecomingDenials(t *testing.T) {
	sentinel := errors.New("backend unreachable")
	app, err := New(Stores{Gatehouse: GatehouseStores{Reader: errReader{err: sentinel}}}, Modes{Gatehouse: true}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = evaluation.RequirePermission(context.Background(), app.Gatehouse.Reader, uuid.New(), "anything.read")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap the store's own error", err)
	}
}

func TestJobsModeRequiresItsReader(t *testing.T) {
	_, err := New(gatehouseOnly(), Modes{Gatehouse: true, Jobs: true}, Config{})
	if !errors.Is(err, ErrMissingStore) || !strings.Contains(err.Error(), "Jobs mode requires Stores.Jobs.Reader") {
		t.Fatalf("err = %v, want ErrMissingStore naming the Jobs reader", err)
	}
}

func TestJobsAuthNeedsBothJobsAndGatehouseModes(t *testing.T) {
	app, err := New(Stores{Jobs: JobsStores{Reader: nilJobsReader{}}}, Modes{Jobs: true}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := app.JobsAuth(); err == nil {
		t.Error("JobsAuth worked with the Jobs mode but no Gatehouse mode: jobsauth is the integration of both")
	}

	stores := gatehouseOnly()
	stores.Jobs = JobsStores{Reader: nilJobsReader{}}
	both, err := New(stores, Modes{Gatehouse: true, Jobs: true}, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	deps, err := both.JobsAuth()
	if err != nil {
		t.Fatalf("JobsAuth: %v", err)
	}
	if deps.GatehouseReader == nil || deps.JobsReader == nil {
		t.Error("the readers should be passed through")
	}
	if deps.GatehouseWriter != nil || deps.JobsWriter != nil {
		t.Error("absent writers must stay absent, so a read-only node's writes fail clearly")
	}
}

func TestSeedAddsTheJobsStepOnlyWhenBothModesAreOn(t *testing.T) {
	stores := gatehouseOnly()
	stores.Jobs = JobsStores{Reader: nilJobsReader{}}

	jobsOnly, _ := New(Stores{Jobs: stores.Jobs}, Modes{Jobs: true}, Config{})
	if report, err := jobsOnly.Seed(context.Background()); err != nil || len(report) != 0 {
		t.Fatalf("the jobs base alone seeds nothing: %+v err=%v", report, err)
	}
	both, _ := New(stores, Modes{Gatehouse: true, Jobs: true}, Config{})
	report, err := both.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	var names []string
	for _, s := range report {
		names = append(names, s.Name)
	}
	if len(report) != 2 || names[1] != "jobsauth.RegisterPermissions" || report[1].Status != SeedSkipped {
		t.Fatalf("want the gatehouse step then the jobsauth step, both skipped (no Writer): %v %+v", names, report)
	}
}
