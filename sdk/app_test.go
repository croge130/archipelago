package sdk

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/governor"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	"github.com/croge130/archipelago/noderoles"
	policyFacade "github.com/croge130/archipelago/policy/facade"
	policyStructure "github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	vitalsFacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/google/uuid"
)

// The fakes embed a nil interface, so any method call on them panics.
// That makes "New and Seed-skip paths perform no storage I/O" a property
// the tests below prove by merely not panicking.
type nilGatehouseReader struct{ gatehouseFacade.Reader }
type nilJobsReader struct{ jobsFacade.Reader }
type nilVitalsReader struct{ vitalsFacade.Reader }
type nilVitalsWriter struct{ vitalsFacade.Writer }

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

// ---- node roles ----------------------------------------------------------

// denyingReader answers the one lookup an authorization starts with: the
// permission is not registered, so every check is a denial. Anything else
// panics, which proves nothing else is consulted.
type denyingReader struct{ gatehouseFacade.Reader }

func (denyingReader) GetPermissionDefinition(context.Context, string) (gatehouseStructure.PermissionDefinition, bool, error) {
	return gatehouseStructure.PermissionDefinition{}, false, nil
}

func nodeRoleConfig(t *testing.T) *NodeRoleConfig {
	t.Helper()
	gov, err := governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 2}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gov.Close)
	return &NodeRoleConfig{Governor: gov, Domain: "d", PrincipalID: uuid.New()}
}

func sweeper(perms ...string) noderoles.Definition {
	every, _ := recurrence.Every(time.Hour, time.Time{})
	return noderoles.Definition{
		Key: "myapp.sweeper", Permissions: perms, Default: &governor.Limits{Concurrency: 1},
		Duties: []noderoles.Duty{{Name: "sweep", Schedule: every, Run: func(context.Context, scheduler.Fire) error { return nil }}},
	}
}

func useDefault() noderoles.Adoption {
	return noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}
}

func TestNodeRolesModeNamesEverythingItNeeds(t *testing.T) {
	_, err := New(Stores{}, Modes{NodeRoles: true}, Config{})
	if !errors.Is(err, ErrMissingStore) || !strings.Contains(err.Error(), "Config.NodeRoles") {
		t.Fatalf("no config: %v", err)
	}
	_, err = New(gatehouseOnly(), Modes{Gatehouse: true, NodeRoles: true}, Config{NodeRoles: &NodeRoleConfig{}})
	if !errors.Is(err, ErrMissingStore) {
		t.Fatalf("empty config: %v", err)
	}
	for _, want := range []string{"Governor", "Domain", "PrincipalID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	// Without Gatehouse there is no authority to check, so no principal is needed.
	cfg := nodeRoleConfig(t)
	cfg.PrincipalID = uuid.Nil
	if _, err := New(Stores{}, Modes{NodeRoles: true}, Config{NodeRoles: cfg}); err != nil {
		t.Errorf("a scoped node with node roles but no Gatehouse: %v", err)
	}
	// And New touched no store: the fake would have panicked.
	if _, err := New(gatehouseOnly(), Modes{Gatehouse: true, NodeRoles: true}, Config{NodeRoles: nodeRoleConfig(t)}); err != nil {
		t.Errorf("New: %v", err)
	}
}

func TestTheRuntimeIsCreatedOnceAndOnlyWhenTheModeIsOn(t *testing.T) {
	off, _ := New(Stores{}, Modes{}, Config{})
	if _, err := off.NodeRoles(); !errors.Is(err, ErrNodeRolesDisabled) {
		t.Errorf("mode off: %v", err)
	}
	if _, err := off.AdoptNodeRole(context.Background(), sweeper(), useDefault()); !errors.Is(err, ErrNodeRolesDisabled) {
		t.Errorf("AdoptNodeRole with the mode off: %v", err)
	}
	app, err := New(Stores{}, Modes{NodeRoles: true}, Config{NodeRoles: nodeRoleConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	a, err1 := app.NodeRoles()
	b, err2 := app.NodeRoles()
	if err1 != nil || err2 != nil || a != b {
		t.Errorf("two calls gave different runtimes or an error: %p %p %v %v", a, b, err1, err2)
	}
	t.Cleanup(func() { a.Stop(context.Background()) })
}

func TestWithoutGatehouseARoleNeedingPermissionsIsRefusedNotRunUnchecked(t *testing.T) {
	app, err := New(Stores{}, Modes{NodeRoles: true}, Config{NodeRoles: nodeRoleConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := app.NodeRoles()
	t.Cleanup(func() { rt.Stop(context.Background()) })
	if _, err := app.AdoptNodeRole(context.Background(), sweeper("myapp.sweep"), useDefault()); !errors.Is(err, ErrCannotAuthorize) {
		t.Fatalf("a node role needing permissions: %v", err)
	}
	if _, err := app.AdoptNodeRole(context.Background(), sweeper(), useDefault()); err != nil {
		t.Errorf("a node role needing none: %v", err)
	}
}

func TestAdoptionAlwaysBindsTheNodesAuthorityAndCannotBeAdoptedAround(t *testing.T) {
	stores := Stores{Gatehouse: GatehouseStores{Reader: denyingReader{}}}
	app, err := New(stores, Modes{Gatehouse: true, NodeRoles: true}, Config{NodeRoles: nodeRoleConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := app.NodeRoles()
	t.Cleanup(func() { rt.Stop(context.Background()) })

	// The caller supplies an Authorize that says yes to everything. The App's
	// own authority check replaces it, and the node principal holds nothing.
	lax := useDefault()
	lax.Authorize = func(context.Context, []string) error { return nil }
	if _, err := app.AdoptNodeRole(context.Background(), sweeper("myapp.sweep"), lax); !errors.Is(err, noderoles.ErrNotAuthorized) {
		t.Fatalf("a caller-supplied Authorize got a node role adopted: %v", err)
	}
	// A node role needing nothing is unaffected.
	if _, err := app.AdoptNodeRole(context.Background(), sweeper(), useDefault()); err != nil {
		t.Errorf("a node role needing no permissions: %v", err)
	}
}

func TestSeedRegistersNodeRolePermissionsOnlyWithGatehouseAndANodeRolesMode(t *testing.T) {
	cfg := nodeRoleConfig(t)
	cfg.Definitions = []noderoles.Definition{sweeper("myapp.sweep")}

	app, _ := New(gatehouseOnly(), Modes{Gatehouse: true, NodeRoles: true}, Config{NodeRoles: cfg})
	report, err := app.Seed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range report {
		if s.Name == "noderolesauth.RegisterPermissions" {
			found = true
			if s.Status != SeedSkipped {
				t.Errorf("without a Writer the step should be skipped: %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("the step is missing: %+v", report)
	}

	noGate, _ := New(Stores{}, Modes{NodeRoles: true}, Config{NodeRoles: cfg})
	report, _ = noGate.Seed(context.Background())
	for _, s := range report {
		if strings.HasPrefix(s.Name, "noderolesauth") {
			t.Errorf("a step for a mode that is not on: %+v", s)
		}
	}
}

func TestMembershipNeedsItsModesAndAWriter(t *testing.T) {
	app, _ := New(gatehouseOnly(), Modes{Gatehouse: true, NodeRoles: true}, Config{NodeRoles: nodeRoleConfig(t)})
	if _, err := app.Membership("workers", nil, 0); err == nil || !strings.Contains(err.Error(), "Writer") {
		t.Errorf("a read-only node: %v", err)
	}
	none, _ := New(Stores{}, Modes{}, Config{})
	if _, err := none.Membership("workers", nil, 0); err == nil {
		t.Error("Membership worked with the modes off")
	}
}

// brokenPolicy is a Policy Reader that cannot read anything.
type brokenPolicy struct{ policyFacade.Reader }

func (brokenPolicy) GetPolicyDefinition(context.Context, string) (policyStructure.PolicyDefinition, bool, error) {
	return policyStructure.PolicyDefinition{}, false, errors.New("policy store unreachable")
}

func TestNodeRolePolicyNeedsBothModes(t *testing.T) {
	for _, m := range []Modes{{NodeRoles: true}, {Policy: true}, {}} {
		var stores Stores
		cfg := Config{}
		if m.Policy {
			stores.Policy.Reader = brokenPolicy{}
		}
		if m.NodeRoles {
			cfg.NodeRoles = nodeRoleConfig(t)
		}
		app, err := New(stores, m, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.NodeRolePolicy(); err == nil {
			t.Errorf("modes %+v: node role policy worked without both modes", m)
		}
		if _, err := app.NodeRolePolicyRefresh(time.Minute); err == nil {
			t.Errorf("modes %+v: the refresh node role was built without both modes", m)
		}
	}
}

func TestAPolicyThatCannotBeReadDoesNotUndoAnAdoptionButIsLoud(t *testing.T) {
	var logs bytes.Buffer
	cfg := nodeRoleConfig(t)
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	app, err := New(Stores{Policy: PolicyStores{Reader: brokenPolicy{}}}, Modes{NodeRoles: true, Policy: true}, Config{NodeRoles: cfg})
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := app.NodeRoles()
	t.Cleanup(func() { rt.Stop(context.Background()) })

	adopted, err := app.AdoptNodeRole(context.Background(), sweeper(), useDefault())
	if err != nil || adopted == nil {
		t.Fatalf("an unreadable policy undid the node's own adoption: %v", err)
	}
	if adopted.Limits() != adopted.Base() {
		t.Errorf("it must run as adopted: %+v vs %+v", adopted.Limits(), adopted.Base())
	}
	if !strings.Contains(logs.String(), "its policy could not be applied") || !strings.Contains(logs.String(), "policy store unreachable") {
		t.Errorf("the failure was not logged with its cause:\n%s", logs.String())
	}
	if _, err := app.ApplyNodeRolePolicy(context.Background(), adopted); err == nil {
		t.Error("an explicit apply must surface the failure")
	}
}

func TestSeedEnsuresNodeRolePolicyDefinitionsOnlyWithBothModes(t *testing.T) {
	has := func(m Modes) (SeedStep, bool) {
		var stores Stores
		cfg := Config{}
		if m.Policy {
			stores.Policy.Reader = brokenPolicy{}
		}
		if m.NodeRoles {
			cfg.NodeRoles = nodeRoleConfig(t)
		}
		app, err := New(stores, m, cfg)
		if err != nil {
			t.Fatal(err)
		}
		report, _ := app.Seed(context.Background())
		for _, s := range report {
			if s.Name == "noderolespolicy.EnsureDefinitions" {
				return s, true
			}
		}
		return SeedStep{}, false
	}
	if s, ok := has(Modes{NodeRoles: true, Policy: true}); !ok || s.Status != SeedSkipped {
		t.Errorf("with both modes and no Writer the step should be present and skipped: %+v %v", s, ok)
	}
	for _, m := range []Modes{{NodeRoles: true}, {Policy: true}} {
		if _, ok := has(m); ok {
			t.Errorf("modes %+v: a step for a mode combination that is not on", m)
		}
	}
}

func TestNodeRoleVitalsNeedsItsModesAndAWriter(t *testing.T) {
	for _, m := range []Modes{{NodeRoles: true}, {Vitals: true}, {}} {
		var stores Stores
		cfg := Config{}
		// Both stores are present whatever the modes, so only the modes can be what refuses.
		stores.Vitals = VitalsStores{Reader: nilVitalsReader{}, Writer: nilVitalsWriter{}}
		if m.NodeRoles {
			cfg.NodeRoles = nodeRoleConfig(t)
		}
		app, err := New(stores, m, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.NodeRoleVitals(); err == nil {
			t.Errorf("modes %+v: node role vitals worked without both modes", m)
		}
		if _, err := app.NodeRoleVitalsReport(time.Minute); err == nil {
			t.Errorf("modes %+v: the report node role was built without both modes", m)
		}
	}
	app, _ := New(Stores{Vitals: VitalsStores{Reader: nilVitalsReader{}}}, Modes{NodeRoles: true, Vitals: true}, Config{NodeRoles: nodeRoleConfig(t)})
	if _, err := app.NodeRoleVitals(); err == nil || !strings.Contains(err.Error(), "Writer") {
		t.Errorf("a read-only Vitals node: %v", err)
	}
}

func TestSeedEnsuresNodeRoleVitalsDefinitionsOnlyWithBothModes(t *testing.T) {
	has := func(m Modes) (SeedStep, bool) {
		var stores Stores
		cfg := Config{}
		if m.Vitals {
			stores.Vitals.Reader = nilVitalsReader{}
		}
		if m.NodeRoles {
			cfg.NodeRoles = nodeRoleConfig(t)
		}
		app, err := New(stores, m, cfg)
		if err != nil {
			t.Fatal(err)
		}
		report, _ := app.Seed(context.Background())
		for _, s := range report {
			if s.Name == "noderolesvitals.EnsureDefinitions" {
				return s, true
			}
		}
		return SeedStep{}, false
	}
	if s, ok := has(Modes{NodeRoles: true, Vitals: true}); !ok || s.Status != SeedSkipped {
		t.Errorf("with both modes and no Writer the step should be present and skipped: %+v %v", s, ok)
	}
	for _, m := range []Modes{{NodeRoles: true}, {Vitals: true}} {
		if _, ok := has(m); ok {
			t.Errorf("modes %+v: a step for a mode combination that is not on", m)
		}
	}
}
