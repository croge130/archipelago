package noderolesvitals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/recurrence"
	"github.com/croge130/archipelago/scheduler"
	vitalsfacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
)

// ReportKey is the node role that reports the others' status.
const ReportKey = "archipelago.noderoles.vitals_report"

// Sink is where instances and readings go. The Vitals facade satisfies it
// through NewDirectSink; an integration with Gatehouse supplies one that checks
// the reporting principal first.
type Sink interface {
	EnsureInstance(ctx context.Context, i structure.Instance) (structure.Instance, error)
	WriteReading(ctx context.Context, r structure.Reading) (structure.Reading, error)
}

// DefinitionReader finds the definitions EnsureDefinitions registered.
type DefinitionReader interface {
	GetDefinitionByKeyVersion(ctx context.Context, key string, version int) (structure.Definition, bool, error)
}

// PolicyNotes tells what policy could not apply as asked for a node role (a
// request capped at the adoption, an interval raised to a floor).
// noderolespolicy.Binding satisfies it.
type PolicyNotes interface {
	Notes(role string) []string
}

type directSink struct {
	r vitalsfacade.Reader
	w vitalsfacade.Writer
}

// NewDirectSink writes straight to the Vitals facade, with no authority check.
func NewDirectSink(r vitalsfacade.Reader, w vitalsfacade.Writer) Sink { return directSink{r, w} }

func (s directSink) EnsureInstance(ctx context.Context, i structure.Instance) (structure.Instance, error) {
	return vitalsfacade.EnsureInstance(ctx, s.r, s.w, i)
}

func (s directSink) WriteReading(ctx context.Context, r structure.Reading) (structure.Reading, error) {
	return vitalsfacade.WriteReading(ctx, s.r, s.w, r)
}

// Config configures a Reporter.
type Config struct {
	Runtime *noderoles.Runtime
	// Governor, if set, adds the node's governor reading.
	Governor    *governor.Governor
	Definitions DefinitionReader
	Sink        Sink

	// ScopeType and ScopeID say whose readings these are: the scope the
	// instances live in and the context a permission check is made against.
	// ScopeType defaults to "node"; ScopeID is required, and must be unique
	// among the nodes reporting into the same Vitals store (the node's
	// principal in its domain is a natural choice).
	ScopeType, ScopeID string

	// Policy, if set, adds policy's notes to each node role's reading.
	Policy PolicyNotes
	// Actor, if set, is recorded on each reading as who wrote it.
	Actor *uuid.UUID
	// TTL is how long a reading is good for; the reading goes stale after.
	// Zero means readings never expire. The node role from Definition sets it
	// to three reporting intervals when left zero.
	TTL time.Duration

	Clock  func() time.Time
	Logger *slog.Logger
}

// Reporter turns the runtime's status into Vitals readings.
type Reporter struct {
	cfg Config
	log *slog.Logger

	mu        sync.Mutex
	defs      map[string]uuid.UUID
	instances map[string]uuid.UUID
	prev      map[string]counters
}

// New returns a Reporter.
func New(cfg Config) (*Reporter, error) {
	switch {
	case cfg.Runtime == nil:
		return nil, errors.New("noderolesvitals: a Reporter needs the node role Runtime")
	case cfg.Definitions == nil || cfg.Sink == nil:
		return nil, errors.New("noderolesvitals: a Reporter needs a DefinitionReader and a Sink")
	case cfg.ScopeID == "":
		return nil, errors.New("noderolesvitals: a Reporter needs a ScopeID")
	case cfg.TTL < 0:
		return nil, errors.New("noderolesvitals: TTL must not be negative")
	}
	if cfg.ScopeType == "" {
		cfg.ScopeType = "node"
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	r := &Reporter{cfg: cfg, log: cfg.Logger, defs: map[string]uuid.UUID{}, instances: map[string]uuid.UUID{}, prev: map[string]counters{}}
	if r.log == nil {
		r.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return r, nil
}

// Report writes one reading per adopted node role, and the governor's. One
// failing does not stop the rest; the errors are joined. A node role whose
// reading could not be written is judged against the same earlier counters
// next time, so what happened in between is not lost.
func (r *Reporter) Report(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.cfg.Clock()
	statuses := r.cfg.Runtime.Status()
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Key < statuses[j].Key })

	var errs []error
	live := map[string]bool{}
	for _, st := range statuses {
		live[st.Key] = true
		if err := r.reportRole(ctx, st, now); err != nil {
			errs = append(errs, fmt.Errorf("node role %q: %w", st.Key, err))
		}
	}
	for key := range r.prev {
		if !live[key] {
			delete(r.prev, key) // closed; a re-adoption starts afresh
		}
	}
	if r.cfg.Governor != nil {
		if err := r.reportGovernor(ctx, now); err != nil {
			errs = append(errs, fmt.Errorf("governor: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (r *Reporter) definitionID(ctx context.Context, key string) (uuid.UUID, error) {
	if id, ok := r.defs[key]; ok {
		return id, nil
	}
	d, found, err := r.cfg.Definitions.GetDefinitionByKeyVersion(ctx, key, definitionVersion)
	if err != nil {
		return uuid.Nil, fmt.Errorf("looking up definition %s: %w", key, err)
	}
	if !found {
		return uuid.Nil, fmt.Errorf("definition %s is not registered (EnsureDefinitions first)", key)
	}
	r.defs[key] = d.DefinitionID
	return d.DefinitionID, nil
}

func (r *Reporter) instanceID(ctx context.Context, cacheKey, defKey, instanceKey, subjectType, subjectKey, display string) (uuid.UUID, error) {
	if id, ok := r.instances[cacheKey]; ok {
		return id, nil
	}
	defID, err := r.definitionID(ctx, defKey)
	if err != nil {
		return uuid.Nil, err
	}
	inst, err := r.cfg.Sink.EnsureInstance(ctx, structure.Instance{
		ScopeType: r.cfg.ScopeType, ScopeID: r.cfg.ScopeID, InstanceKey: instanceKey, DefinitionID: defID,
		SubjectType: subjectType, SubjectKey: subjectKey, DisplayName: display,
		CategoryPath: []string{"node roles"},
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ensuring instance %s: %w", instanceKey, err)
	}
	r.instances[cacheKey] = inst.InstanceID
	return inst.InstanceID, nil
}

func (r *Reporter) write(ctx context.Context, id uuid.UUID, state structure.State, reason, summary string, details any, now time.Time) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encoding details: %w", err)
	}
	rd := structure.Reading{
		InstanceID: id, State: state, ReasonCode: reason, Summary: summary, DetailsJSON: raw,
		ObservedAt: &now, ActorPrincipalID: r.cfg.Actor,
	}
	if r.cfg.TTL > 0 {
		exp := now.Add(r.cfg.TTL)
		rd.ExpiresAt = &exp
	}
	if _, err := r.cfg.Sink.WriteReading(ctx, rd); err != nil {
		return fmt.Errorf("writing reading: %w", err)
	}
	return nil
}

func (r *Reporter) reportRole(ctx context.Context, st noderoles.RoleStatus, now time.Time) error {
	id, err := r.instanceID(ctx, "role/"+st.Key, DefinitionRoleStatus, "noderole."+st.Key, "node_role", st.Key, st.Key)
	if err != nil {
		return err
	}
	var prev *counters
	if p, ok := r.prev[st.Key]; ok {
		prev = &p
	}
	var notes []string
	if r.cfg.Policy != nil {
		notes = r.cfg.Policy.Notes(st.Key)
	}
	a := assess(st, prev, notes)
	if err := r.write(ctx, id, a.state, a.reason, a.summary, roleDetails(st, a, notes), now); err != nil {
		return err
	}
	r.prev[st.Key] = countersOf(st)
	r.log.Debug("node role status reported", "node_role", st.Key, "state", a.state, "reason", a.reason)
	return nil
}

func (r *Reporter) reportGovernor(ctx context.Context, now time.Time) error {
	id, err := r.instanceID(ctx, "governor", DefinitionGovernor, "governor", "governor", "governor", "Resource governor")
	if err != nil {
		return err
	}
	st := r.cfg.Governor.Stats()
	state, reason, summary := assessGovernor(st.Pressure)
	return r.write(ctx, id, state, reason, summary, governorDetails(st), now)
}

// Definition returns a node role that reports every interval. Adopt it like
// any other. Readings last three intervals unless Config.TTL says otherwise.
func (r *Reporter) Definition(every time.Duration, anchor time.Time) (noderoles.Definition, error) {
	sched, err := recurrence.Every(every, anchor)
	if err != nil {
		return noderoles.Definition{}, fmt.Errorf("noderolesvitals: report interval: %w", err)
	}
	r.mu.Lock()
	if r.cfg.TTL == 0 {
		r.cfg.TTL = 3 * every
	}
	r.mu.Unlock()
	one := governor.Limits{Concurrency: 1}
	return noderoles.Definition{
		Key:         ReportKey,
		Description: "reports the status of the node roles this node runs to Vitals",
		Tiers:       governor.TierTable{governor.TierLow: one},
		Default:     &one,
		Duties: []noderoles.Duty{{
			Name: "report", Schedule: sched, Overlap: scheduler.OverlapSkip, Overflow: governor.OverflowDrop,
			Run: func(ctx context.Context, _ scheduler.Fire) error { return r.Report(ctx) },
		}},
	}, nil
}
