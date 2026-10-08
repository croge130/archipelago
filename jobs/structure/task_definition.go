package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/croge130/archipelago/typedvalue"
)

// ParamSpec is one named parameter of a task kind. Parameters are a
// flat list of named typed values rather than a nested schema:
// typedvalue's object storage type carries no field schema, so a
// kind's parameters are expressed as several ParamSpecs, each
// normalized with typedvalue.NormalizeValue.
type ParamSpec struct {
	Name       string
	Definition typedvalue.Definition
	Required   bool
}

// ScopeEntry is one permission a task kind's handler may exercise on
// behalf of whoever the job runs as. The declared scope is the upper
// bound of what a job can do: effective authority is the effective
// principal's current grants intersected with it, so a handler cannot
// use something else its principal happens to hold.
//
// ContextType with ContextIDParam scopes the permission to a context
// whose ID is the value of the named parameter (for example a tenant ID
// parameter), so one definition can serve many contexts without
// declaring them all.
type ScopeEntry struct {
	PermissionKey  string
	ContextType    string
	ContextIDParam string
}

// TaskDefinition is the domain-level record of a task kind: what it is
// called, what parameters it takes, what it may exercise, and its retry
// defaults. It is separate from the handler (code on an executor node,
// which is what actually makes a node able to run the kind) and
// separate from any endpoint: being able to perform a kind of job does
// not require exposing a callable endpoint for it.
type TaskDefinition struct {
	TaskKey     string
	Description string
	Params      []ParamSpec

	// DefaultQueueKey is the queue context jobs of this kind go to
	// unless the submitter names one. It is the authorization scope.
	DefaultQueueKey string

	// Scope is what the handler may exercise; see ScopeEntry.
	Scope []ScopeEntry

	// Idempotent is the only way automatic retry is enabled. A kind not
	// declared idempotent runs at most once per submission: an expired
	// claim ends the job as dead rather than running it again.
	Idempotent bool

	// DefaultMaxAttempts is ignored (treated as 1) unless Idempotent.
	DefaultMaxAttempts    int
	DefaultAttemptTimeout time.Duration
	DefaultBackoff        BackoffPolicy // zero value means DefaultBackoff

	// PriorityCap is the most urgent priority a submitter may ask for.
	PriorityCap Priority

	Metadata json.RawMessage // opaque, never authorized on
}

// EffectiveMaxAttempts applies the Idempotent rule.
func (d TaskDefinition) EffectiveMaxAttempts() int {
	if !d.Idempotent {
		return 1
	}
	return d.DefaultMaxAttempts
}

// EffectiveBackoff returns DefaultBackoff when the definition states none.
func (d TaskDefinition) EffectiveBackoff() BackoffPolicy {
	if d.DefaultBackoff.Kind == "" {
		return DefaultBackoff
	}
	return d.DefaultBackoff
}

func (d TaskDefinition) Validate() error {
	if err := ValidateTaskKey(d.TaskKey); err != nil {
		return fmt.Errorf("structure: task definition: %w", err)
	}
	if err := ValidateQueueKey(d.DefaultQueueKey); err != nil {
		return fmt.Errorf("structure: task definition: default queue: %w", err)
	}
	params := make(map[string]bool, len(d.Params))
	for _, p := range d.Params {
		if err := validateParamName(p.Name); err != nil {
			return fmt.Errorf("structure: task definition: %w", err)
		}
		if params[p.Name] {
			return fmt.Errorf("structure: task definition: duplicate param %q", p.Name)
		}
		params[p.Name] = true
		if err := p.Definition.Validate(); err != nil {
			return fmt.Errorf("structure: task definition: param %q: %w", p.Name, err)
		}
	}
	for _, s := range d.Scope {
		if s.PermissionKey == "" {
			return fmt.Errorf("structure: task definition: scope entry has no PermissionKey")
		}
		if (s.ContextType == "") != (s.ContextIDParam == "") {
			return fmt.Errorf("structure: task definition: scope entry %q needs ContextType and ContextIDParam together or neither", s.PermissionKey)
		}
		if s.ContextIDParam != "" && !params[s.ContextIDParam] {
			return fmt.Errorf("structure: task definition: scope entry %q names unknown param %q", s.PermissionKey, s.ContextIDParam)
		}
	}
	if d.DefaultMaxAttempts < 1 {
		return fmt.Errorf("structure: task definition: DefaultMaxAttempts must be at least 1")
	}
	if d.DefaultAttemptTimeout <= 0 {
		return fmt.Errorf("structure: task definition: DefaultAttemptTimeout must be positive")
	}
	if d.DefaultBackoff.Kind != "" {
		if err := d.DefaultBackoff.Validate(); err != nil {
			return err
		}
	}
	if !d.PriorityCap.Valid() {
		return fmt.Errorf("structure: task definition: invalid PriorityCap %q", d.PriorityCap)
	}
	return nil
}
