package structure

import (
	"strings"
	"testing"
	"time"

	"github.com/croge130/archipelago/typedvalue"
	"github.com/google/uuid"
)

func validDefinition() TaskDefinition {
	return TaskDefinition{
		TaskKey:         "myapp.report.render",
		DefaultQueueKey: "myapp.reports",
		Params: []ParamSpec{
			{Name: "report_id", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
			{Name: "tenant", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}},
		},
		Scope:                 []ScopeEntry{{PermissionKey: "myapp.report.read", ContextType: "myapp.tenant", ContextIDParam: "tenant"}},
		Idempotent:            true,
		DefaultMaxAttempts:    3,
		DefaultAttemptTimeout: time.Minute,
		PriorityCap:           PriorityImportant,
	}
}

func validJob() Job {
	return Job{
		JobID: uuid.New(), TaskKey: "myapp.report.render", QueueKey: "myapp.reports",
		Params: []byte(`{"report_id":"r1"}`), ParamsHash: "abc", State: StatePending, Priority: PriorityNormal,
		Retryable: true, MaxAttempts: 3, AttemptTimeout: time.Minute, Backoff: DefaultBackoff,
		RequestedBy: uuid.New(), AuthorityMode: AuthorityOwner,
	}
}

func TestValidTaskDefinitionAndJob(t *testing.T) {
	if err := validDefinition().Validate(); err != nil {
		t.Fatalf("definition: %v", err)
	}
	if err := validJob().Validate(); err != nil {
		t.Fatalf("job: %v", err)
	}
}

func TestTaskDefinitionRejections(t *testing.T) {
	cases := map[string]func(*TaskDefinition){
		"bad task key":         func(d *TaskDefinition) { d.TaskKey = "Has Space" },
		"no queue":             func(d *TaskDefinition) { d.DefaultQueueKey = "" },
		"duplicate param":      func(d *TaskDefinition) { d.Params = append(d.Params, d.Params[0]) },
		"bad param name":       func(d *TaskDefinition) { d.Params[0].Name = "Bad.Name" },
		"scope without perm":   func(d *TaskDefinition) { d.Scope[0].PermissionKey = "" },
		"scope half a context": func(d *TaskDefinition) { d.Scope[0].ContextIDParam = "" },
		"scope unknown param":  func(d *TaskDefinition) { d.Scope[0].ContextIDParam = "nope" },
		"no attempts":          func(d *TaskDefinition) { d.DefaultMaxAttempts = 0 },
		"no timeout":           func(d *TaskDefinition) { d.DefaultAttemptTimeout = 0 },
		"bad priority cap":     func(d *TaskDefinition) { d.PriorityCap = "urgent" },
		"bad backoff":          func(d *TaskDefinition) { d.DefaultBackoff = BackoffPolicy{Kind: BackoffFixed} },
	}
	for name, mutate := range cases {
		d := validDefinition()
		mutate(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestEffectiveMaxAttemptsOnlyHonorsIdempotentKinds(t *testing.T) {
	d := validDefinition()
	if got := d.EffectiveMaxAttempts(); got != 3 {
		t.Errorf("idempotent kind: got %d, want 3", got)
	}
	d.Idempotent = false
	if got := d.EffectiveMaxAttempts(); got != 1 {
		t.Errorf("non-idempotent kind: got %d, want 1 regardless of DefaultMaxAttempts", got)
	}
}

func TestEffectiveBackoffDefaults(t *testing.T) {
	d := validDefinition()
	if d.EffectiveBackoff() != DefaultBackoff {
		t.Error("an unstated backoff should fall back to DefaultBackoff")
	}
	d.DefaultBackoff = BackoffPolicy{Kind: BackoffFixed, Base: time.Second}
	if d.EffectiveBackoff().Base != time.Second {
		t.Error("a stated backoff should be used as given")
	}
}

func TestJobRejections(t *testing.T) {
	assumedNoRunAs := func(j *Job) { j.AuthorityMode = AuthorityAssumed }
	runAsWithoutAssumed := func(j *Job) { id := uuid.New(); j.RunAs = &id }
	cases := map[string]func(*Job){
		"nil id":                    func(j *Job) { j.JobID = uuid.Nil },
		"bad state":                 func(j *Job) { j.State = "queued" },
		"bad priority":              func(j *Job) { j.Priority = "p1" },
		"no hash":                   func(j *Job) { j.ParamsHash = "" },
		"no owner":                  func(j *Job) { j.RequestedBy = uuid.Nil },
		"bad mode":                  func(j *Job) { j.AuthorityMode = "root" },
		"assumed without RunAs":     assumedNoRunAs,
		"RunAs without assumed":     runAsWithoutAssumed,
		"zero attempts":             func(j *Job) { j.MaxAttempts = 0 },
		"not retryable but retried": func(j *Job) { j.Retryable = false },
		"no timeout":                func(j *Job) { j.AttemptTimeout = 0 },
		"oversized params":          func(j *Job) { j.Params = []byte(strings.Repeat("x", MaxParamsBytes+1)) },
		"overlong idempotency key":  func(j *Job) { j.IdempotencyKey = strings.Repeat("k", MaxIdempotencyKeyRunes+1) },
	}
	for name, mutate := range cases {
		j := validJob()
		mutate(&j)
		if err := j.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestStatesAndTerminality(t *testing.T) {
	for _, s := range []State{StateSucceeded, StateDead, StateCancelled} {
		if !s.Terminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	for _, s := range []State{StatePending, StateClaimed} {
		if s.Terminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
	if State("queued").Valid() {
		t.Error("an unknown state should be invalid")
	}
}

func TestPriorityOrderingAndMin(t *testing.T) {
	order := []Priority{PriorityBackground, PriorityNormal, PriorityImportant, PriorityCritical}
	for i := 1; i < len(order); i++ {
		if order[i].Rank() <= order[i-1].Rank() {
			t.Errorf("%s should outrank %s", order[i], order[i-1])
		}
	}
	if MinPriority(PriorityCritical, PriorityNormal) != PriorityNormal {
		t.Error("MinPriority should return the less urgent")
	}
	for _, p := range order {
		back, err := PriorityFromRank(p.Rank())
		if err != nil || back != p {
			t.Errorf("rank round trip for %s: %v %v", p, back, err)
		}
	}
	if _, err := PriorityFromRank(99); err == nil {
		t.Error("an unknown rank should be rejected")
	}
}

func TestBackoffValidate(t *testing.T) {
	good := []BackoffPolicy{
		{Kind: BackoffFixed, Base: time.Second},
		{Kind: BackoffExponential, Base: time.Second, Max: time.Minute},
	}
	for _, b := range good {
		if err := b.Validate(); err != nil {
			t.Errorf("%+v: %v", b, err)
		}
	}
	bad := []BackoffPolicy{
		{}, {Kind: BackoffFixed}, {Kind: BackoffExponential, Base: time.Second},
		{Kind: BackoffExponential, Base: time.Minute, Max: time.Second}, {Kind: "linear", Base: time.Second},
	}
	for _, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("%+v should be rejected", b)
		}
	}
}

func TestTruncateErrorRespectsRunes(t *testing.T) {
	long := strings.Repeat("é", MaxErrorRunes+10)
	got := TruncateError(long)
	if len([]rune(got)) != MaxErrorRunes {
		t.Errorf("got %d runes, want %d", len([]rune(got)), MaxErrorRunes)
	}
	if TruncateError("short") != "short" {
		t.Error("a short error should be unchanged")
	}
}
