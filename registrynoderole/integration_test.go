// Tests in this file need a real PostgreSQL instance; they skip without
// ARCHIPELAGO_TEST_DATABASE_URL. The scheduler runs on a manual clock so
// duties fire when the test says; the registry underneath is the real one.
package registrynoderole

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	archidb "github.com/croge130/archipelago/db"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	"github.com/google/uuid"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ctx() context.Context { return context.Background() }

func every(t *testing.T, d time.Duration) recurrence.Schedule {
	t.Helper()
	s, err := recurrence.Every(d, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fx struct {
	reader *dbstore.PostgresReader
	writer *dbstore.PostgresWriter
	pool   *archidb.Pool
	p1, p2 structure.Principal
}

func setup(t *testing.T) *fx {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping registrynoderole integration test")
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
	if _, err := pool.Pgx().Exec(ctx(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_leases, gatehouse_instances, gatehouse_endpoint_definitions,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`); err != nil {
		t.Fatal(err)
	}
	f := &fx{reader: dbstore.NewPostgresReader(pool.Pgx()), writer: dbstore.NewPostgresWriter(pool.Pgx()), pool: pool}
	mk := func(key string) structure.Principal {
		p, err := facade.EnsurePrincipal(ctx(), f.reader, f.writer, key, structure.PrincipalTypeServiceAccount)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	f.p1, f.p2 = mk("node.one"), mk("node.two")
	return f
}

// node is one process: a governor, a runtime on a manual clock.
type node struct {
	gov *governor.Governor
	clk *scheduler.ManualClock
	rt  *noderoles.Runtime
}

func newNode(t *testing.T) *node {
	t.Helper()
	gov, err := governor.New(governor.Config{Node: governor.Ceiling{Concurrency: 4}})
	if err != nil {
		t.Fatal(err)
	}
	clk := scheduler.NewManualClock(t0)
	rt, err := noderoles.New(noderoles.Config{Governor: gov, Domain: "d", Clock: clk, Rand: func() float64 { return 0 },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Stop(ctx()); gov.Close() })
	return &node{gov: gov, clk: clk, rt: rt}
}

func (n *node) idle() bool {
	if st := n.gov.Stats(); st.Running != 0 || st.Waiting != 0 {
		return false
	}
	for _, r := range n.rt.Status() {
		for _, d := range r.Duties {
			if d.InFlight != 0 {
				return false
			}
		}
	}
	return true
}

func (n *node) tick(t *testing.T, d time.Duration) {
	t.Helper()
	n.clk.Advance(d)
	deadline := time.Now().Add(3 * time.Second)
	for !n.idle() {
		if time.Now().After(deadline) {
			t.Fatal("runs never finished")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func useDefault() noderoles.Adoption {
	return noderoles.Adoption{Budget: governor.Adoption{UseDefault: true}}
}

func instances(t *testing.T, f *fx, group string) []structure.Instance {
	t.Helper()
	got, err := facade.ListPeers(ctx(), f.reader, group, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMembershipRegistersHeartbeatsAndDeregisters(t *testing.T) {
	f := setup(t)
	n := newNode(t)
	m, err := NewMembership(MembershipConfig{Reader: f.reader, Writer: f.writer, PrincipalID: f.p1.PrincipalID, Group: "workers", HeartbeatEvery: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.rt.Adopt(ctx(), m.Definition(), useDefault()); err != nil {
		t.Fatal(err)
	}
	if err := n.rt.Start(); err != nil {
		t.Fatal(err)
	}

	got := instances(t, f, "workers")
	if len(got) != 1 || got[0].InstanceID != m.InstanceID() || got[0].PrincipalID != f.p1.PrincipalID {
		t.Fatalf("registered instances %+v, node's instance %s", got, m.InstanceID())
	}
	registered := got[0].LastHeartbeatAt

	n.tick(t, 10*time.Second)
	n.tick(t, 10*time.Second)
	after := instances(t, f, "workers")[0]
	if !after.LastHeartbeatAt.After(registered) {
		t.Errorf("heartbeat did not advance: %v -> %v", registered, after.LastHeartbeatAt)
	}
	if st := n.rt.Status()[0]; st.Duties[0].Completed != 2 || st.Limits.Concurrency != 1 {
		t.Errorf("status %+v", st)
	}

	n.rt.Stop(ctx())
	if got := instances(t, f, "workers"); len(got) != 0 {
		t.Errorf("the instance is still listed after the node role closed: %+v", got)
	}
}

func TestALostRowIsRegisteredAgainWithANewInstance(t *testing.T) {
	f := setup(t)
	n := newNode(t)
	m, _ := NewMembership(MembershipConfig{Reader: f.reader, Writer: f.writer, PrincipalID: f.p1.PrincipalID, Group: "workers", HeartbeatEvery: 10 * time.Second})
	if _, err := n.rt.Adopt(ctx(), m.Definition(), useDefault()); err != nil {
		t.Fatal(err)
	}
	n.rt.Start()
	old := m.InstanceID()

	// An operator (or a cleanup job) removes the row out from under the node.
	if err := facade.Deregister(ctx(), f.writer, old); err != nil {
		t.Fatal(err)
	}
	n.tick(t, 10*time.Second)
	if m.InstanceID() == old || m.Reregistrations() != 1 {
		t.Fatalf("instance %s (was %s), re-registrations %d", m.InstanceID(), old, m.Reregistrations())
	}
	got := instances(t, f, "workers")
	if len(got) != 1 || got[0].InstanceID != m.InstanceID() {
		t.Errorf("registry after re-registering: %+v", got)
	}
}

func TestMembershipValidatesItsConfig(t *testing.T) {
	f := setup(t)
	for name, cfg := range map[string]MembershipConfig{
		"no reader":    {Writer: f.writer, PrincipalID: f.p1.PrincipalID, Group: "g"},
		"no principal": {Reader: f.reader, Writer: f.writer, Group: "g"},
		"no group":     {Reader: f.reader, Writer: f.writer, PrincipalID: f.p1.PrincipalID},
		"negative":     {Reader: f.reader, Writer: f.writer, PrincipalID: f.p1.PrincipalID, Group: "g", HeartbeatEvery: -1},
	} {
		if _, err := NewMembership(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Two nodes, one lease, one exclusive node role: only the holder's duty runs,
// the other takes over when the holder lets go, and the loser's run in
// progress is cancelled when it loses.
func TestTwoNodesShareOneLeaseAndTheExclusiveDutyRunsOnlyOnTheHolder(t *testing.T) {
	f := setup(t)
	type side struct {
		n    *node
		m    *Membership
		gate *LeaseGate
		runs chan string
	}
	mk := func(name string, p structure.Principal) *side {
		s := &side{n: newNode(t), runs: make(chan string, 16)}
		var err error
		s.m, err = NewMembership(MembershipConfig{Reader: f.reader, Writer: f.writer, PrincipalID: p.PrincipalID, Group: "workers"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.n.rt.Adopt(ctx(), s.m.Definition(), useDefault()); err != nil {
			t.Fatal(err)
		}
		s.gate, err = NewLeaseGate(LeaseConfig{Writer: f.writer, Group: "workers", Name: "leader", TTL: 30 * time.Second, Instance: s.m.InstanceID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.n.rt.Adopt(ctx(), s.gate.Definition(), useDefault()); err != nil {
			t.Fatal(err)
		}
		lead := noderoles.Definition{Key: "myapp.leader.work", Exclusive: true, Default: &governor.Limits{Concurrency: 1}, Duties: []noderoles.Duty{{
			Name: "lead", Run: func(ctx context.Context, _ scheduler.Fire) error { s.runs <- name; return nil },
			Schedule: every(t, time.Minute), Overlap: scheduler.OverlapSkip,
		}}}
		a := useDefault()
		a.Gate = s.gate
		if _, err := s.n.rt.Adopt(ctx(), lead, a); err != nil {
			t.Fatal(err)
		}
		if err := s.n.rt.Start(); err != nil {
			t.Fatal(err)
		}
		return s
	}
	a := mk("a", f.p1)
	b := mk("b", f.p2)

	// a adopted first, so a took the lease at Setup; b's attempt found it held.
	if _, held := a.gate.Acquire(); !held {
		t.Fatal("the first node does not hold the lease")
	}
	if _, held := b.gate.Acquire(); held {
		t.Fatal("both nodes believe they hold one lease")
	}
	a.n.tick(t, time.Minute)
	b.n.tick(t, time.Minute)
	select {
	case who := <-a.runs:
		if who != "a" {
			t.Fatalf("ran on %s", who)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the holder's exclusive duty never ran")
	}
	select {
	case who := <-b.runs:
		t.Fatalf("the exclusive duty ran on %s, which does not hold the lease", who)
	case <-time.After(150 * time.Millisecond):
	}
	if st := b.n.rt.Status(); st[len(st)-1].NotHeld == 0 && !statusHasNotHeld(st) {
		t.Errorf("the non-holder did not record skipping: %+v", st)
	}

	// a lets go (graceful stop); b takes the lease on its next renewal.
	lost, _ := a.gate.Acquire()
	if err := a.gate.Release(ctx()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lost:
	default:
		t.Fatal("releasing the lease did not close the holder's lost channel")
	}
	b.n.tick(t, 10*time.Second) // one third of the 30s TTL: b's renewal duty
	if _, held := b.gate.Acquire(); !held {
		t.Fatal("the second node did not take the freed lease")
	}
	b.n.tick(t, time.Minute)
	select {
	case who := <-b.runs:
		if who != "b" {
			t.Fatalf("ran on %s", who)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the new holder's exclusive duty never ran")
	}
}

func statusHasNotHeld(st []noderoles.RoleStatus) bool {
	for _, s := range st {
		if s.NotHeld > 0 {
			return true
		}
	}
	return false
}

func TestALeaseTheRegistryNoLongerGrantsIsGivenUpBeforeAnotherNodeCanTakeIt(t *testing.T) {
	f := setup(t)
	now := t0
	clock := func() time.Time { return now }
	inst := uuid.New()
	// A real Instance row is needed for the lease holder.
	i, err := facade.RegisterInstance(ctx(), f.writer, f.p1.PrincipalID, "workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	inst = i.InstanceID
	g, err := NewLeaseGate(LeaseConfig{Writer: f.writer, Group: "workers", Name: "leader", TTL: 30 * time.Second, Instance: func() uuid.UUID { return inst }, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Renew(ctx()); err != nil {
		t.Fatal(err)
	}
	lost, held := g.Acquire()
	if !held {
		t.Fatal("did not acquire a free lease")
	}
	// Renewals stop working (the gate's clock moves past its deadline).
	now = t0.Add(31 * time.Second)
	if _, held := g.Acquire(); held {
		t.Error("the gate still claims a lease past its own deadline")
	}
	select {
	case <-lost:
	default:
		t.Error("the lost channel was not closed when the gate gave the lease up")
	}

	// Someone else holds it: Renew reports no error, and the gate is not held.
	other, _ := facade.RegisterInstance(ctx(), f.writer, f.p2.PrincipalID, "workers", nil)
	if _, ok, err := facade.AcquireOrRenewLease(ctx(), f.writer, "workers", "contested", other.InstanceID, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	g2, _ := NewLeaseGate(LeaseConfig{Writer: f.writer, Group: "workers", Name: "contested", TTL: 30 * time.Second, Instance: func() uuid.UUID { return inst }})
	if err := g2.Renew(ctx()); err != nil {
		t.Errorf("Renew against a lease held elsewhere: %v", err)
	}
	if _, held := g2.Acquire(); held {
		t.Error("the gate claims a lease another instance holds")
	}
}

func TestLeaseGateValidatesItsConfig(t *testing.T) {
	f := setup(t)
	id := func() uuid.UUID { return uuid.New() }
	for name, cfg := range map[string]LeaseConfig{
		"no writer": {Group: "g", Name: "n", TTL: time.Second, Instance: id},
		"no group":  {Writer: f.writer, Name: "n", TTL: time.Second, Instance: id},
		"no name":   {Writer: f.writer, Group: "g", TTL: time.Second, Instance: id},
		"no ttl":    {Writer: f.writer, Group: "g", Name: "n", Instance: id},
		"no holder": {Writer: f.writer, Group: "g", Name: "n", TTL: time.Second},
	} {
		if _, err := NewLeaseGate(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_ = errors.New
}

// A holder whose renewal is refused because someone else now holds the lease
// must stop believing at once, not at its own deadline.
func TestAHolderWhoseRenewalIsRefusedStopsHoldingImmediately(t *testing.T) {
	f := setup(t)
	mine, err := facade.RegisterInstance(ctx(), f.writer, f.p1.PrincipalID, "workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := facade.RegisterInstance(ctx(), f.writer, f.p2.PrincipalID, "workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewLeaseGate(LeaseConfig{Writer: f.writer, Group: "workers", Name: "leader", TTL: time.Hour, Instance: func() uuid.UUID { return mine.InstanceID }})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Renew(ctx()); err != nil {
		t.Fatal(err)
	}
	lost, held := g.Acquire()
	if !held {
		t.Fatal("did not take a free lease")
	}
	// The registry now says another instance holds it (this node was slow to
	// renew and the lease passed on).
	if _, err := f.pool.Pgx().Exec(ctx(), `UPDATE gatehouse_leases SET holder_instance_id = $1, expires_at = now() + interval '1 hour' WHERE lease_group = 'workers' AND lease_name = 'leader'`, theirs.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := g.Renew(ctx()); err != nil {
		t.Fatalf("a refused renewal is not an error: %v", err)
	}
	if _, held := g.Acquire(); held {
		t.Error("still holding after the registry refused the renewal (the deadline is an hour away)")
	}
	select {
	case <-lost:
	default:
		t.Error("the run-in-progress cancellation signal was not sent")
	}
}
