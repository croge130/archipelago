// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. Vitals is the real Vitals over Postgres; the
// node roles are the real noderoles over the real governor and scheduler, on a
// manual clock.
package noderolesvitals

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/croge130/archipelago/vitals/storage/dbstore"
	"github.com/croge130/archipelago/vitals/structure"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ctx() context.Context { return context.Background() }

type env struct {
	pool *archidb.Pool
	r    *dbstore.PostgresReader
	w    *dbstore.PostgresWriter
	gov  *governor.Governor
	clk  *scheduler.ManualClock
	rt   *noderoles.Runtime
	sink *flakySink
	rep  *Reporter
}

// flakySink fails the next failWrites writes, then passes through.
type flakySink struct {
	Sink
	failWrites atomic.Int32
	ensures    atomic.Int32
}

func (f *flakySink) EnsureInstance(ctx context.Context, i structure.Instance) (structure.Instance, error) {
	f.ensures.Add(1)
	return f.Sink.EnsureInstance(ctx, i)
}

func (f *flakySink) WriteReading(ctx context.Context, r structure.Reading) (structure.Reading, error) {
	if f.failWrites.Add(-1) >= 0 {
		return structure.Reading{}, errors.New("vitals store unavailable")
	}
	f.failWrites.Store(0)
	return f.Sink.WriteReading(ctx, r)
}

type notes map[string][]string

func (n notes) Notes(role string) []string { return n[role] }

func setup(t *testing.T) *env { return setupWithDomain(t, governor.Ceiling{Concurrency: 100}) }

func setupWithDomain(t *testing.T, domain governor.Ceiling) *env {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping noderolesvitals integration test")
	}
	pool, err := archidb.Open(ctx(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrations, err := dbstore.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.ProvisionSchemas(ctx(), migrations); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Pgx().Exec(ctx(), `TRUNCATE vitals_group_members, vitals_groups, vitals_reading_history,
		vitals_current_readings, vitals_instances, vitals_definitions CASCADE`); err != nil {
		t.Fatal(err)
	}
	e := &env{pool: pool, r: dbstore.NewPostgresReader(pool.Pgx()), w: dbstore.NewPostgresWriter(pool.Pgx())}
	if err := EnsureDefinitions(ctx(), e.r, e.w); err != nil {
		t.Fatal(err)
	}
	e.gov, err = governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 100}, Domains: map[string]governor.Ceiling{"d": domain}})
	if err != nil {
		t.Fatal(err)
	}
	e.clk = scheduler.NewManualClock(t0)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.rt, err = noderoles.New(noderoles.Config{Governor: e.gov, Domain: "d", Record: scheduler.NewMemoryRecord(), Clock: e.clk, Rand: func() float64 { return 0 }, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.rt.Stop(ctx()); e.gov.Close() })
	e.sink = &flakySink{Sink: NewDirectSink(e.r, e.w)}
	e.rep, err = New(Config{
		Runtime: e.rt, Governor: e.gov, Definitions: e.r, Sink: e.sink, ScopeID: "node-1",
		TTL: 5 * time.Minute, Clock: e.clk.Now, Logger: quiet,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) idle() bool {
	if st := e.gov.Stats(); st.Running != 0 || st.Waiting != 0 {
		return false
	}
	for _, r := range e.rt.Status() {
		for _, d := range r.Duties {
			if d.InFlight != 0 {
				return false
			}
		}
	}
	return true
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// tick lets one minute pass once the previous run is fully over, and waits for
// the occurrence it triggers to finish.
func (e *env) tick(t *testing.T, ran *atomic.Int32) {
	t.Helper()
	waitFor(t, "the previous run to be fully over", e.idle)
	before := ran.Load()
	e.clk.Advance(time.Minute)
	waitFor(t, "the run for this minute", func() bool { return ran.Load() == before+1 })
	waitFor(t, "it to be fully over", e.idle)
}

type roleOpts struct {
	key       string
	fail      *atomic.Bool
	ran       *atomic.Int32
	authorize func(context.Context, []string) error
}

func (e *env) adopt(t *testing.T, o roleOpts) *noderoles.Adopted {
	t.Helper()
	if o.key == "" {
		o.key = "myapp.sweeper"
	}
	if o.ran == nil {
		o.ran = new(atomic.Int32)
	}
	every, err := recurrence.Every(time.Minute, t0)
	if err != nil {
		t.Fatal(err)
	}
	ad, err := e.rt.Adopt(ctx(), noderoles.Definition{
		Key: o.key, Tiers: governor.TierTable{governor.TierLow: {Concurrency: 4, QueueDepth: 8}}, Permissions: permsFor(o),
		Duties: []noderoles.Duty{{Name: "sweep", Schedule: every, Run: func(context.Context, scheduler.Fire) error {
			o.ran.Add(1)
			if o.fail != nil && o.fail.Load() {
				return errors.New("boom")
			}
			return nil
		}}},
	}, noderoles.Adoption{Budget: governor.Adoption{Tier: governor.TierLow}, Authorize: o.authorize})
	if err != nil {
		t.Fatal(err)
	}
	return ad
}

func permsFor(o roleOpts) []string {
	if o.authorize != nil {
		return []string{"myapp.sweep"}
	}
	return nil
}

func (e *env) roleReading(t *testing.T, key string) (structure.Reading, roleDoc) {
	t.Helper()
	inst, found, err := e.r.GetInstanceByScopeKey(ctx(), "node", "node-1", "noderole."+key)
	if err != nil || !found {
		t.Fatalf("instance for %s: found=%v err=%v", key, found, err)
	}
	rd, found, err := e.r.GetReading(ctx(), inst.InstanceID)
	if err != nil || !found {
		t.Fatalf("reading for %s: found=%v err=%v", key, found, err)
	}
	var doc roleDoc
	if err := json.Unmarshal(rd.DetailsJSON, &doc); err != nil {
		t.Fatalf("details: %v\n%s", err, rd.DetailsJSON)
	}
	return rd, doc
}

func (e *env) historyLen(t *testing.T, key string) int {
	t.Helper()
	inst, _, err := e.r.GetInstanceByScopeKey(ctx(), "node", "node-1", key)
	if err != nil {
		t.Fatal(err)
	}
	h, err := e.r.ListHistory(ctx(), inst.InstanceID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return len(h)
}

func (e *env) report(t *testing.T) {
	t.Helper()
	if err := e.rep.Report(ctx()); err != nil {
		t.Fatalf("Report: %v", err)
	}
}

func TestEnsureDefinitionsIsIdempotent(t *testing.T) {
	e := setup(t)
	if err := EnsureDefinitions(ctx(), e.r, e.w); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestAHealthyNodeRoleIsReportedWithWhatItRunsUnder(t *testing.T) {
	e := setup(t)
	e.adopt(t, roleOpts{})
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.report(t)

	rd, doc := e.roleReading(t, "myapp.sweeper")
	if rd.State != structure.StateOK || rd.ReasonCode != "" || rd.Summary == "" {
		t.Fatalf("reading %+v", rd)
	}
	if doc.Effective.Concurrency != 4 || doc.Effective.QueueDepth != 8 || len(doc.Duties) != 1 || doc.Duties[0].Name != "sweep" {
		t.Fatalf("details %+v", doc)
	}
	if doc.Duties[0].NextSlot == nil || !doc.Duties[0].NextSlot.After(t0) {
		t.Fatalf("a scheduled duty should say when it is next due: %+v", doc.Duties[0])
	}
	if rd.ObservedAt == nil || !rd.ObservedAt.Equal(t0) {
		t.Fatalf("observed %v, want the reporter's clock %v", rd.ObservedAt, t0)
	}
	if rd.ExpiresAt == nil || !rd.ExpiresAt.Equal(t0.Add(5*time.Minute)) {
		t.Fatalf("a reading must go stale: expires %v", rd.ExpiresAt)
	}

	g, found, err := e.r.GetInstanceByScopeKey(ctx(), "node", "node-1", "governor")
	if err != nil || !found {
		t.Fatalf("governor instance: %v %v", found, err)
	}
	if grd, _, _ := e.r.GetReading(ctx(), g.InstanceID); grd.State != structure.StateOK {
		t.Fatalf("governor reading %+v", grd)
	}
}

func TestACeilingsClampIsVisibleInTheReading(t *testing.T) {
	e := setupWithDomain(t, governor.Ceiling{Concurrency: 2})
	e.adopt(t, roleOpts{}) // asks for 4, the domain's share holds it to 2
	e.report(t)
	rd, doc := e.roleReading(t, "myapp.sweeper")
	if rd.State != structure.StateOK || rd.ReasonCode != ReasonClamped {
		t.Fatalf("reading %+v", rd)
	}
	if doc.Effective.Concurrency != 2 || len(doc.Clamps) != 1 || !strings.Contains(doc.Clamps[0], "domain") {
		t.Fatalf("details %+v", doc)
	}
}

func TestAFailureIsAWarningForOneReportThenOKAgainAndHistoryRecordsTheTransitions(t *testing.T) {
	e := setup(t)
	var fail atomic.Bool
	var ran atomic.Int32
	e.adopt(t, roleOpts{fail: &fail, ran: &ran})
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.report(t)
	if got := e.historyLen(t, "noderole.myapp.sweeper"); got != 1 {
		t.Fatalf("first report: %d history rows", got)
	}

	fail.Store(true)
	e.tick(t, &ran)
	e.tick(t, &ran)
	fail.Store(false)
	e.report(t)
	rd, doc := e.roleReading(t, "myapp.sweeper")
	if rd.State != structure.StateWarning || rd.ReasonCode != ReasonRunsFailed || doc.SinceLastReport.Failed != 2 {
		t.Fatalf("after two failures: %+v %+v", rd, doc.SinceLastReport)
	}

	e.tick(t, &ran) // a good run
	e.report(t)
	rd, doc = e.roleReading(t, "myapp.sweeper")
	if rd.State != structure.StateOK || doc.SinceLastReport.Failed != 0 {
		t.Fatalf("a bad minute is not forever: %+v %+v", rd, doc.SinceLastReport)
	}
	if doc.Governor.Failed != 2 {
		t.Fatalf("the cumulative count stays in the details: %+v", doc.Governor)
	}
	// ok, warning, ok: the transitions, nothing more.
	if got := e.historyLen(t, "noderole.myapp.sweeper"); got != 3 {
		t.Fatalf("%d history rows, want 3", got)
	}
}

func TestASteadyNodeWritesOneHistoryRowHoweverOftenItReports(t *testing.T) {
	e := setup(t)
	var ran atomic.Int32
	e.adopt(t, roleOpts{ran: &ran})
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		e.tick(t, &ran) // the counters move; the verdict does not
		e.report(t)
	}
	if got := e.historyLen(t, "noderole.myapp.sweeper"); got != 1 {
		t.Fatalf("a steady node wrote %d history rows", got)
	}
	_, doc := e.roleReading(t, "myapp.sweeper")
	if doc.Duties[0].Completed != 5 {
		t.Fatalf("but the current reading is live: %+v", doc.Duties[0])
	}
}

func TestAFailedWriteDoesNotLoseWhatHappenedInTheMeantime(t *testing.T) {
	e := setup(t)
	var fail atomic.Bool
	var ran atomic.Int32
	e.adopt(t, roleOpts{fail: &fail, ran: &ran})
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.report(t)
	fail.Store(true)
	e.tick(t, &ran)
	fail.Store(false)

	e.sink.failWrites.Store(1)
	if err := e.rep.Report(ctx()); err == nil || !strings.Contains(err.Error(), "myapp.sweeper") || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("a write failure must be reported with the node role: %v", err)
	}
	e.report(t)
	rd, doc := e.roleReading(t, "myapp.sweeper")
	if rd.State != structure.StateWarning || doc.SinceLastReport.Failed != 1 {
		t.Fatalf("the failure that happened while the sink was down must still show: %+v %+v", rd, doc.SinceLastReport)
	}
}

func TestPolicyNotesSurfaceInTheReading(t *testing.T) {
	e := setup(t)
	e.adopt(t, roleOpts{})
	e.rep.cfg.Policy = notes{"myapp.sweeper": {"concurrency: policy asked 50, the node adopted 4"}}
	e.report(t)
	rd, doc := e.roleReading(t, "myapp.sweeper")
	if rd.ReasonCode != ReasonPolicyNote || len(doc.PolicyNotes) != 1 || !strings.Contains(doc.PolicyNotes[0], "policy asked 50") {
		t.Fatalf("reading %+v details %+v", rd, doc)
	}
}

func TestALostAuthorizationIsFailedAndRecoversWhenGrantedAgain(t *testing.T) {
	e := setup(t)
	var denied atomic.Bool
	var ran atomic.Int32
	e.adopt(t, roleOpts{ran: &ran, authorize: func(context.Context, []string) error {
		if denied.Load() {
			return errors.New("no grant")
		}
		return nil
	}})
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	denied.Store(true)
	waitFor(t, "idle", e.idle)
	e.clk.Advance(time.Minute)
	waitFor(t, "the refused run to be counted", func() bool { return e.rt.Status()[0].Unauthorized == 1 })
	waitFor(t, "idle", e.idle)
	e.report(t)
	if rd, _ := e.roleReading(t, "myapp.sweeper"); rd.State != structure.StateFailed || rd.ReasonCode != ReasonNotAuthorized {
		t.Fatalf("reading %+v", rd)
	}

	denied.Store(false)
	e.tick(t, &ran)
	e.report(t)
	if rd, _ := e.roleReading(t, "myapp.sweeper"); rd.State != structure.StateOK {
		t.Fatalf("after the grant returned: %+v", rd)
	}
}

func TestAReAdoptedNodeRoleIsJudgedFromItsOwnFreshCounters(t *testing.T) {
	e := setup(t)
	var fail atomic.Bool
	var ran atomic.Int32
	ad := e.adopt(t, roleOpts{fail: &fail, ran: &ran})
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	e.tick(t, &ran)
	e.report(t) // one failure on record

	waitFor(t, "idle", e.idle)
	if err := ad.Close(ctx()); err != nil {
		t.Fatal(err)
	}
	e.report(t) // the closed role is forgotten here
	e.adopt(t, roleOpts{fail: &fail, ran: &ran})
	for i := 0; i < 3; i++ {
		e.tick(t, &ran)
	}
	e.report(t)
	if _, doc := e.roleReading(t, "myapp.sweeper"); doc.SinceLastReport.Failed != 3 {
		t.Fatalf("a re-adopted node role's failures are all new, got %d", doc.SinceLastReport.Failed)
	}
}

func TestGovernorPressureIsReported(t *testing.T) {
	e := setup(t)
	e.gov.SetPressure(governor.PressureHigh)
	e.report(t)
	g, _, _ := e.r.GetInstanceByScopeKey(ctx(), "node", "node-1", "governor")
	rd, _, _ := e.r.GetReading(ctx(), g.InstanceID)
	if rd.State != structure.StateDegraded || rd.ReasonCode != ReasonPressureHigh {
		t.Fatalf("reading %+v", rd)
	}
	e.gov.SetPressure(governor.PressureNone)
	e.report(t)
	if rd, _, _ = e.r.GetReading(ctx(), g.InstanceID); rd.State != structure.StateOK {
		t.Fatalf("pressure cleared: %+v", rd)
	}
}

func TestMissingDefinitionsAreNamedNotSilentlySkipped(t *testing.T) {
	e := setup(t)
	e.adopt(t, roleOpts{})
	if _, err := e.pool.Pgx().Exec(ctx(), `TRUNCATE vitals_reading_history, vitals_current_readings, vitals_instances, vitals_definitions CASCADE`); err != nil {
		t.Fatal(err)
	}
	// A fresh reporter, as after a restart, has no cached definition ids.
	rep, err := New(Config{Runtime: e.rt, Definitions: e.r, Sink: e.sink, ScopeID: "node-1"})
	if err != nil {
		t.Fatal(err)
	}
	err = rep.Report(ctx())
	if err == nil || !strings.Contains(err.Error(), DefinitionRoleStatus) || !strings.Contains(err.Error(), "EnsureDefinitions") {
		t.Fatalf("want the missing definition named, got %v", err)
	}
}

func TestTheReportNodeRoleReportsOnItsOwnScheduleAndExpiresReadingsByDefault(t *testing.T) {
	e := setup(t)
	e.rep.cfg.TTL = 0
	e.adopt(t, roleOpts{})
	def, err := e.rep.Definition(time.Minute, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.rt.Adopt(ctx(), def, noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}); err != nil {
		t.Fatal(err)
	}
	if err := e.rt.Start(); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(time.Minute)
	waitFor(t, "the first scheduled report", func() bool {
		inst, found, _ := e.r.GetInstanceByScopeKey(ctx(), "node", "node-1", "noderole.myapp.sweeper")
		if !found {
			return false
		}
		_, found, _ = e.r.GetReading(ctx(), inst.InstanceID)
		return found
	})
	rd, _ := e.roleReading(t, "myapp.sweeper")
	if rd.ExpiresAt == nil || rd.ExpiresAt.Sub(*rd.ObservedAt) != 3*time.Minute {
		t.Fatalf("readings from the report role last three intervals: %v", rd.ExpiresAt)
	}
	// It reports on itself too.
	if _, found, _ := e.r.GetInstanceByScopeKey(ctx(), "node", "node-1", "noderole."+ReportKey); !found {
		t.Error("the report node role is a node role like any other and is reported")
	}
}

func TestNewRefusesWhatItCannotWorkWith(t *testing.T) {
	e := setup(t)
	good := Config{Runtime: e.rt, Definitions: e.r, Sink: e.sink, ScopeID: "n"}
	for name, mut := range map[string]func(*Config){
		"no runtime":     func(c *Config) { c.Runtime = nil },
		"no definitions": func(c *Config) { c.Definitions = nil },
		"no sink":        func(c *Config) { c.Sink = nil },
		"no scope id":    func(c *Config) { c.ScopeID = "" },
		"negative ttl":   func(c *Config) { c.TTL = -1 },
	} {
		c := good
		mut(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: New accepted it", name)
		}
	}
	if _, err := New(good); err != nil {
		t.Fatalf("the good config: %v", err)
	}
}

func TestOneNodeRolesFailureDoesNotStopTheOthersBeingReported(t *testing.T) {
	e := setup(t)
	e.adopt(t, roleOpts{key: "a.first"})
	e.adopt(t, roleOpts{key: "b.second"})
	e.sink.failWrites.Store(1) // the first write, which is a.first's, fails
	err := e.rep.Report(ctx())
	if err == nil || !strings.Contains(err.Error(), "a.first") {
		t.Fatalf("the failure must be reported with its node role: %v", err)
	}
	if rd, _ := e.roleReading(t, "b.second"); rd.State != structure.StateOK {
		t.Fatalf("b.second was not reported past a.first's failure: %+v", rd)
	}
}

func TestInstancesAreEnsuredOnceNotOnEveryReport(t *testing.T) {
	e := setup(t)
	e.adopt(t, roleOpts{})
	e.report(t)
	first := e.sink.ensures.Load()
	if first != 2 { // the node role and the governor
		t.Fatalf("first report ensured %d instances, want 2", first)
	}
	e.report(t)
	e.report(t)
	if got := e.sink.ensures.Load(); got != first {
		t.Fatalf("later reports ensured %d more instances; with Gatehouse each is a permission check", got-first)
	}
}
