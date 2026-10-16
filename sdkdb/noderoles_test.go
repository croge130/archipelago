// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. They show a node adopting node roles through
// nothing but the SDK: real stores from OpenDB, the node's own principal, a
// governor shared by the process, and a runtime on a manual clock.
package sdkdb

import (
	"context"
	"errors"
	"testing"
	"time"

	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/noderolespolicy"
	"github.com/croge130/archipelago/noderolesvitals"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/croge130/archipelago/sdk"
)

var nodeRoleModes = sdk.Modes{Gatehouse: true, NodeRoles: true}

func TestANodeAdoptsNodeRolesThroughTheSDKAndAuthorityIsEnforcedAgainstItsOwnGrants(t *testing.T) {
	stores, pool := openStores(t, nodeRoleModes)
	for _, table := range []string{"gatehouse_leases", "gatehouse_instances"} {
		_, _ = pool.Pgx().Exec(context.Background(), "TRUNCATE "+table+" CASCADE")
	}
	setupCtx := ctx5(t)
	node, err := gatehouseFacade.EnsurePrincipal(setupCtx, stores.Gatehouse.Reader, stores.Gatehouse.Writer, "node.one", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatal(err)
	}

	gov, err := governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 4}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gov.Close)
	clk := scheduler.NewManualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	ran := make(chan struct{}, 8)
	every, _ := recurrence.Every(time.Minute, time.Time{})
	sweeper := noderoles.Definition{
		Key: "myapp.sweeper", Permissions: []string{"myapp.sweep"}, Default: &governor.Limits{Concurrency: 1},
		Duties: []noderoles.Duty{{Name: "sweep", Schedule: every, Run: func(context.Context, scheduler.Fire) error { ran <- struct{}{}; return nil }}},
	}
	app, err := sdk.New(stores, nodeRoleModes, sdk.Config{NodeRoles: &sdk.NodeRoleConfig{
		Governor: gov, Domain: "d", PrincipalID: node.PrincipalID, Clock: clk, Definitions: []noderoles.Definition{sweeper},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// Seed registers the permission the node role names.
	report, err := app.Seed(setupCtx)
	if err != nil {
		t.Fatalf("Seed: %v (%+v)", err, report)
	}
	seeded := false
	for _, s := range report {
		if s.Name == "noderolesauth.RegisterPermissions" && s.Status == sdk.SeedRan {
			seeded = true
		}
	}
	if !seeded {
		t.Fatalf("Seed did not register node role permissions: %+v", report)
	}
	if _, found, _ := app.Gatehouse.Reader.GetPermissionDefinition(setupCtx, "myapp.sweep"); !found {
		t.Fatal("the node role's permission was not registered")
	}

	rt, err := app.NodeRoles()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	// The node principal holds no grant, so adoption fails closed through the SDK.
	adopt := noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}
	if _, err := app.AdoptNodeRole(setupCtx, sweeper, adopt); !errors.Is(err, noderoles.ErrNotAuthorized) {
		t.Fatalf("adopting without the grant: %v", err)
	}
	if _, err := gatehouseFacade.GrantPermission(setupCtx, stores.Gatehouse.Writer, gatehouseStructure.GrantSubjectTypePrincipal, node.PrincipalID, "myapp.sweep"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AdoptNodeRole(setupCtx, sweeper, adopt); err != nil {
		t.Fatalf("adopting with the grant: %v", err)
	}

	// Registry membership, built from the App's own stores and the node's principal.
	m, err := app.Membership("workers", nil, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AdoptNodeRole(setupCtx, m.Definition(), adopt); err != nil {
		t.Fatalf("adopting membership: %v", err)
	}
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}

	peers, err := gatehouseFacade.ListPeers(setupCtx, app.Gatehouse.Reader, "workers", time.Hour)
	if err != nil || len(peers) != 1 || peers[0].PrincipalID != node.PrincipalID || peers[0].InstanceID != m.InstanceID() {
		t.Fatalf("the node is not in the registry as itself: %+v %v", peers, err)
	}
	registered := peers[0].LastHeartbeatAt

	clk.Advance(10 * time.Second)
	waitIdle(t, rt, gov)
	clk.Advance(50 * time.Second)
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("the adopted node role's duty never ran")
	}
	waitIdle(t, rt, gov)
	after, err := gatehouseFacade.ListPeers(setupCtx, app.Gatehouse.Reader, "workers", time.Hour)
	if err != nil || len(after) != 1 || !after[0].LastHeartbeatAt.After(registered) {
		t.Fatalf("the heartbeat did not advance: %+v %v", after, err)
	}

	// Revoking the grant stops the swept duty on its next run, no restart.
	if _, err := pool.Pgx().Exec(setupCtx, `UPDATE gatehouse_grants SET status = 'revoked' WHERE subject_id = $1 AND permission_key = 'myapp.sweep'`, node.PrincipalID.String()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Minute)
	waitIdle(t, rt, gov)
	select {
	case <-ran:
		t.Error("a duty ran after the node's grant was revoked")
	default:
	}

	// Stopping the runtime deregisters the node.
	rt.Stop(setupCtx)
	if left, _ := gatehouseFacade.ListPeers(setupCtx, app.Gatehouse.Reader, "workers", time.Hour); len(left) != 0 {
		t.Errorf("still registered after stopping: %+v", left)
	}
}

func waitIdle(t *testing.T, rt *noderoles.Runtime, gov *governor.Governor) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		idle := gov.Stats().Running == 0 && gov.Stats().Waiting == 0
		for _, r := range rt.Status() {
			for _, d := range r.Duties {
				idle = idle && d.InFlight == 0
			}
		}
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("runs never finished")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestPolicyShapesNodeRolesThroughTheSDKAtAdoptionAndAfterwards(t *testing.T) {
	modes := sdk.Modes{Gatehouse: true, Policy: true, NodeRoles: true}
	stores, _ := openStores(t, modes)
	setupCtx := ctx5(t)
	node, err := gatehouseFacade.EnsurePrincipal(setupCtx, stores.Gatehouse.Reader, stores.Gatehouse.Writer, "node.policy", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	gov, err := governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 8}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gov.Close)
	clk := scheduler.NewManualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	hourly, _ := recurrence.Every(time.Hour, time.Time{})
	sweeper := noderoles.Definition{
		Key: "myapp.sweeper", Default: &governor.Limits{Concurrency: 3, QueueDepth: 3},
		Duties: []noderoles.Duty{{Name: "sweep", Schedule: hourly, Run: func(context.Context, scheduler.Fire) error { return nil }}},
	}
	app, err := sdk.New(stores, modes, sdk.Config{NodeRoles: &sdk.NodeRoleConfig{
		Governor: gov, Domain: "d", PrincipalID: node.PrincipalID, Clock: clk, Definitions: []noderoles.Definition{sweeper},
	}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := app.Seed(setupCtx)
	if err != nil {
		t.Fatalf("Seed: %v (%+v)", err, report)
	}
	var ensured bool
	for _, s := range report {
		ensured = ensured || (s.Name == "noderolespolicy.EnsureDefinitions" && s.Status == sdk.SeedRan)
	}
	if !ensured {
		t.Fatalf("Seed did not register the node role policy definitions: %+v", report)
	}
	rt, err := app.NodeRoles()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	// Policy set before the node role exists shapes it from the first moment.
	set := func(scope noderolespolicy.Scope, v any) {
		t.Helper()
		if _, err := noderolespolicy.Set(setupCtx, stores.Policy.Reader, stores.Policy.Writer, noderolespolicy.KeyConcurrency, scope, v, "operator:test"); err != nil {
			t.Fatal(err)
		}
	}
	set(noderolespolicy.ForRole("myapp.sweeper"), 1)
	adopted, err := app.AdoptNodeRole(setupCtx, sweeper, noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}})
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Base().Concurrency != 3 || adopted.Limits().Concurrency != 1 {
		t.Fatalf("base %+v limits %+v: policy was not applied at adoption", adopted.Base(), adopted.Limits())
	}

	// The refresh node role carries later edits to the running node role.
	refresh, err := app.NodeRolePolicyRefresh(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AdoptNodeRole(setupCtx, refresh, noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}
	set(noderolespolicy.ForRole("myapp.sweeper"), 2)
	clk.Advance(time.Minute)
	deadline := time.Now().Add(3 * time.Second)
	for adopted.Limits().Concurrency != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the edit never reached the running node role: %+v", adopted.Limits())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if adopted.Base().Concurrency != 3 {
		t.Fatalf("policy moved the adopted base: %+v", adopted.Base())
	}
}

func TestNodeRoleStatusReachesVitalsThroughTheSDKAsTheNodeAndOnlyWithPermission(t *testing.T) {
	modes := sdk.Modes{Gatehouse: true, Policy: true, Vitals: true, NodeRoles: true}
	stores, pool := openStores(t, modes)
	setupCtx := ctx5(t)
	node, err := gatehouseFacade.EnsurePrincipal(setupCtx, stores.Gatehouse.Reader, stores.Gatehouse.Writer, "node.vitals", gatehouseStructure.PrincipalTypeServiceAccount)
	if err != nil {
		t.Fatal(err)
	}
	gov, err := governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 8}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gov.Close)
	clk := scheduler.NewManualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	hourly, _ := recurrence.Every(time.Hour, time.Time{})
	sweeper := noderoles.Definition{
		Key: "myapp.sweeper", Default: &governor.Limits{Concurrency: 3, QueueDepth: 3},
		Duties: []noderoles.Duty{{Name: "sweep", Schedule: hourly, Run: func(context.Context, scheduler.Fire) error { return nil }}},
	}
	app, err := sdk.New(stores, modes, sdk.Config{NodeRoles: &sdk.NodeRoleConfig{
		Governor: gov, Domain: "d", PrincipalID: node.PrincipalID, Clock: clk, Definitions: []noderoles.Definition{sweeper},
	}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := app.Seed(setupCtx)
	if err != nil {
		t.Fatalf("Seed: %v (%+v)", err, report)
	}
	var ensured bool
	for _, s := range report {
		ensured = ensured || (s.Name == "noderolesvitals.EnsureDefinitions" && s.Status == sdk.SeedRan)
	}
	if !ensured {
		t.Fatalf("Seed did not register the node role vitals definitions: %+v", report)
	}
	rt, err := app.NodeRoles()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Stop(context.Background()) })

	// Policy asks for more than the node adopted; the reading should say so.
	if _, err := noderolespolicy.Set(setupCtx, stores.Policy.Reader, stores.Policy.Writer, noderolespolicy.KeyConcurrency,
		noderolespolicy.ForRole("myapp.sweeper"), 50, "operator:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AdoptNodeRole(setupCtx, sweeper, noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}); err != nil {
		t.Fatal(err)
	}
	reportRole, err := app.NodeRoleVitalsReport(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AdoptNodeRole(setupCtx, reportRole, noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(); err != nil {
		t.Fatal(err)
	}

	scope := node.PrincipalID.String()
	instanceFor := func() (found bool) {
		_, found, err := stores.Vitals.Reader.GetInstanceByScopeKey(setupCtx, "node", scope, "noderole.myapp.sweeper")
		if err != nil {
			t.Fatal(err)
		}
		return found
	}

	// No permission to write in its own scope: the report fails closed.
	clk.Advance(time.Minute)
	waitIdle(t, rt, gov)
	if instanceFor() {
		t.Fatal("a node with no permission to write vitals registered an instance")
	}

	if _, err := gatehouseFacade.GrantPermission(setupCtx, stores.Gatehouse.Writer, gatehouseStructure.GrantSubjectTypePrincipal, node.PrincipalID, "vitals.write"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Minute)
	waitIdle(t, rt, gov)
	deadline := time.Now().Add(3 * time.Second)
	for !instanceFor() {
		if time.Now().After(deadline) {
			t.Fatal("with permission, the node role status never reached Vitals")
		}
		time.Sleep(2 * time.Millisecond)
	}
	inst, _, _ := stores.Vitals.Reader.GetInstanceByScopeKey(setupCtx, "node", scope, "noderole.myapp.sweeper")
	rd, found, err := stores.Vitals.Reader.GetReading(setupCtx, inst.InstanceID)
	if err != nil || !found {
		t.Fatalf("reading: %v %v", found, err)
	}
	if rd.ReasonCode != noderolesvitals.ReasonPolicyNote || rd.ActorPrincipalID == nil || *rd.ActorPrincipalID != node.PrincipalID {
		t.Fatalf("the reading should carry policy's note and be written as the node: %+v", rd)
	}
	if rd.ExpiresAt == nil {
		t.Fatal("readings written by the report node role must expire")
	}

	// Revoking the permission stops further readings too, not only the first
	// registration: the instance exists now, so only the write is checked.
	if _, err := pool.Pgx().Exec(setupCtx, `UPDATE gatehouse_grants SET status = 'revoked' WHERE subject_id = $1 AND permission_key = 'vitals.write'`, node.PrincipalID.String()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Minute)
	waitIdle(t, rt, gov)
	after, _, err := stores.Vitals.Reader.GetReading(setupCtx, inst.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ObservedAt.Equal(*rd.ObservedAt) {
		t.Fatalf("a reading was written after the node lost permission to write: observed %v then %v", rd.ObservedAt, after.ObservedAt)
	}
}
