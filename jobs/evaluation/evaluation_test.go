package evaluation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/typedvalue"
)

func TestTransitionsMatchTheStateDiagram(t *testing.T) {
	legal := [][2]structure.State{
		{structure.StatePending, structure.StateClaimed},
		{structure.StatePending, structure.StateDead},
		{structure.StatePending, structure.StateCancelled},
		{structure.StateClaimed, structure.StateSucceeded},
		{structure.StateClaimed, structure.StatePending},
		{structure.StateClaimed, structure.StateDead},
		{structure.StateClaimed, structure.StateCancelled},
	}
	isLegal := map[[2]structure.State]bool{}
	for _, l := range legal {
		isLegal[l] = true
	}
	all := []structure.State{structure.StatePending, structure.StateClaimed, structure.StateSucceeded, structure.StateDead, structure.StateCancelled}
	for _, from := range all {
		for _, to := range all {
			if got, want := CanTransition(from, to), isLegal[[2]structure.State{from, to}]; got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	for _, terminal := range []structure.State{structure.StateSucceeded, structure.StateDead, structure.StateCancelled} {
		for _, to := range all {
			if CanTransition(terminal, to) {
				t.Errorf("terminal state %s must have no outgoing transitions, found ->%s", terminal, to)
			}
		}
	}
}

func TestAfterFailure(t *testing.T) {
	cases := []struct {
		name                      string
		retryable                 bool
		attempt, max              int
		terminal, cancelRequested bool
		want                      structure.State
	}{
		{"retryable with attempts left", true, 1, 3, false, false, structure.StatePending},
		{"retryable, last attempt used", true, 3, 3, false, false, structure.StateDead},
		{"not retryable", false, 1, 1, false, false, structure.StateDead},
		{"terminal overrides retries", true, 1, 3, true, false, structure.StateDead},
		{"a cancel request beats a retry", true, 1, 3, false, true, structure.StateCancelled},
		{"a cancel request beats a terminal failure", false, 1, 1, true, true, structure.StateCancelled},
	}
	for _, c := range cases {
		if got := AfterFailure(c.retryable, c.attempt, c.max, c.terminal, c.cancelRequested); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDelay(t *testing.T) {
	fixed := structure.BackoffPolicy{Kind: structure.BackoffFixed, Base: 7 * time.Second}
	for attempt := 1; attempt <= 5; attempt++ {
		if got := Delay(fixed, attempt); got != 7*time.Second {
			t.Errorf("fixed attempt %d: %v", attempt, got)
		}
	}
	exp := structure.BackoffPolicy{Kind: structure.BackoffExponential, Base: time.Second, Max: 10 * time.Second}
	want := []time.Duration{1, 2, 4, 8, 10, 10, 10}
	for i, w := range want {
		if got := Delay(exp, i+1); got != w*time.Second {
			t.Errorf("exponential attempt %d: got %v, want %v", i+1, got, w*time.Second)
		}
	}
	// An absurd attempt count must not overflow into a tiny or negative delay.
	if got := Delay(exp, 10_000); got != 10*time.Second {
		t.Errorf("attempt 10000: got %v, want the cap", got)
	}
	huge := structure.BackoffPolicy{Kind: structure.BackoffExponential, Base: time.Hour, Max: 1<<62 - 1}
	if got := Delay(huge, 200); got <= 0 {
		t.Errorf("overflow produced %v", got)
	}
	if got := Delay(exp, 0); got != time.Second {
		t.Errorf("attempt 0 should behave as attempt 1, got %v", got)
	}
	if got := Delay(structure.BackoffPolicy{}, 1); got != 0 {
		t.Errorf("an invalid policy should return 0, got %v", got)
	}
}

func def() structure.TaskDefinition {
	return structure.TaskDefinition{
		TaskKey: "myapp.report.render", DefaultQueueKey: "myapp.reports",
		Params: []structure.ParamSpec{
			{Name: "report_id", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}, Required: true},
			{Name: "copies", Definition: typedvalue.Definition{Storage: typedvalue.StorageInt}},
			{Name: "tenant", Definition: typedvalue.Definition{Storage: typedvalue.StorageString}},
		},
		Scope: []structure.ScopeEntry{
			{PermissionKey: "myapp.report.read", ContextType: "myapp.tenant", ContextIDParam: "tenant"},
			{PermissionKey: "myapp.audit.write"},
		},
		DefaultMaxAttempts: 1, DefaultAttemptTimeout: time.Minute, PriorityCap: structure.PriorityNormal,
	}
}

func TestNormalizeParamsCanonicalFormAndHash(t *testing.T) {
	a, hashA, err := NormalizeParams(def(), json.RawMessage(`{"report_id":"r1","copies":2}`))
	if err != nil {
		t.Fatalf("NormalizeParams: %v", err)
	}
	// Same content, different key order and numeric spelling: same hash.
	b, hashB, err := NormalizeParams(def(), json.RawMessage(`{"copies":2.0,"report_id":"  r1 "}`))
	if err != nil {
		t.Fatalf("NormalizeParams: %v", err)
	}
	if string(a) != string(b) || hashA != hashB {
		t.Errorf("equivalent params should canonicalize identically:\n%s %s\n%s %s", a, hashA, b, hashB)
	}
	// Different content: different hash.
	_, hashC, err := NormalizeParams(def(), json.RawMessage(`{"report_id":"r2","copies":2}`))
	if err != nil {
		t.Fatalf("NormalizeParams: %v", err)
	}
	if hashC == hashA {
		t.Error("different params must not share a hash")
	}
}

func TestNormalizeParamsRejections(t *testing.T) {
	cases := map[string]string{
		"unknown parameter": `{"report_id":"r1","evil":"rm -rf /"}`,
		"missing required":  `{"copies":1}`,
		"wrong type":        `{"report_id":"r1","copies":"many"}`,
		"not an object":     `["r1"]`,
		"not json":          `{`,
	}
	for name, raw := range cases {
		if _, _, err := NormalizeParams(def(), json.RawMessage(raw)); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	if _, _, err := NormalizeParams(def(), json.RawMessage(`{"report_id":"`+strings.Repeat("x", structure.MaxParamsBytes)+`"}`)); err == nil {
		t.Error("oversized params accepted")
	}
}

func TestNormalizeParamsEmptyAndNull(t *testing.T) {
	d := def()
	d.Params[0].Required = false
	for _, raw := range []string{"", "   ", "null", "{}"} {
		got, _, err := NormalizeParams(d, json.RawMessage(raw))
		if err != nil {
			t.Errorf("%q: %v", raw, err)
		}
		if string(got) != "{}" {
			t.Errorf("%q: got %s, want {}", raw, got)
		}
	}
}

func TestResolveScopeAndScopePermits(t *testing.T) {
	scope, err := ResolveScope(def(), json.RawMessage(`{"report_id":"r1","tenant":"acme"}`))
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if !ScopePermits(scope, "myapp.report.read", "myapp.tenant", "acme") {
		t.Error("the declared context-scoped permission should be permitted for its own tenant")
	}
	if ScopePermits(scope, "myapp.report.read", "myapp.tenant", "globex") {
		t.Error("a different tenant must not be permitted")
	}
	if ScopePermits(scope, "myapp.report.read", "", "") {
		t.Error("a context-scoped declaration must not permit the unscoped check")
	}
	if !ScopePermits(scope, "myapp.audit.write", "", "") {
		t.Error("an unscoped declaration should permit the unscoped check")
	}
	if ScopePermits(scope, "myapp.audit.write", "myapp.tenant", "acme") {
		t.Error("an unscoped declaration must not quietly grant the permission in every context")
	}
	if ScopePermits(scope, "myapp.admin.everything", "", "") {
		t.Error("an undeclared permission must be denied")
	}
}

func TestResolveScopeNeverWidensWhenAParameterIsMissing(t *testing.T) {
	if _, err := ResolveScope(def(), json.RawMessage(`{"report_id":"r1"}`)); err == nil {
		t.Error("a scope entry whose parameter is absent must be an error, not a silent widening")
	}
	if _, err := ResolveScope(def(), json.RawMessage(`{"report_id":"r1","tenant":""}`)); err == nil {
		t.Error("an empty context ID parameter must be an error")
	}
	if _, err := ResolveScope(def(), json.RawMessage(`{"report_id":"r1","tenant":5}`)); err == nil {
		t.Error("a non-string context ID parameter must be an error")
	}
}

func TestSameTaskDefinition(t *testing.T) {
	a := def()
	b := def()
	if !SameTaskDefinition(a, b) {
		t.Fatal("identical definitions should be the same")
	}
	// nil versus empty lists and metadata are the same thing.
	a.Scope, b.Scope = nil, []structure.ScopeEntry{}
	a.Metadata, b.Metadata = nil, json.RawMessage(`{}`)
	if !SameTaskDefinition(a, b) {
		t.Error("nil and empty lists/metadata must compare equal")
	}
	// An unstated backoff is the default backoff.
	a.DefaultBackoff = structure.BackoffPolicy{}
	b.DefaultBackoff = structure.DefaultBackoff
	if !SameTaskDefinition(a, b) {
		t.Error("an unstated backoff must equal the default backoff")
	}
	// Real differences are still differences.
	for name, mutate := range map[string]func(*structure.TaskDefinition){
		"idempotence":  func(d *structure.TaskDefinition) { d.Idempotent = !d.Idempotent },
		"queue":        func(d *structure.TaskDefinition) { d.DefaultQueueKey = "other" },
		"a param":      func(d *structure.TaskDefinition) { d.Params[0].Required = false },
		"scope":        func(d *structure.TaskDefinition) { d.Scope = append(d.Scope, structure.ScopeEntry{PermissionKey: "x"}) },
		"description":  func(d *structure.TaskDefinition) { d.Description = "changed" },
		"priority cap": func(d *structure.TaskDefinition) { d.PriorityCap = structure.PriorityCritical },
	} {
		c := def()
		mutate(&c)
		if SameTaskDefinition(def(), c) {
			t.Errorf("a changed %s should be a different definition", name)
		}
	}
}
