package noderolespolicy

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/policy/facade"
	"github.com/croge130/archipelago/policy/structure"
)

// Ref kinds a policy value can attach to. Policy treats them as opaque.
const (
	RefDomain = "domain"
	RefRole   = "noderole"
	RefDuty   = "noderole_duty"
)

// Scope is what a policy value applies to.
type Scope struct {
	global bool
	ref    structure.Ref
}

// Global applies to every node role on every domain.
func Global() Scope { return Scope{global: true} }

// ForDomain applies to every node role the named domain runs.
func ForDomain(domain string) Scope { return Scope{ref: structure.Ref{Kind: RefDomain, Key: domain}} }

// ForRole applies to one node role.
func ForRole(role string) Scope { return Scope{ref: structure.Ref{Kind: RefRole, Key: role}} }

// ForDuty applies to one duty of a node role. Only the schedule keys are
// meaningful at this scope.
func ForDuty(role, duty string) Scope {
	return Scope{ref: structure.Ref{Kind: RefDuty, Key: role + "/" + duty}}
}

func (s Scope) String() string {
	if s.global {
		return "global"
	}
	return s.ref.Kind + ":" + s.ref.Key
}

func (s Scope) validate() error {
	if s.global {
		return nil
	}
	return s.ref.Validate()
}

func (s Scope) target() (structure.TargetKind, *structure.Ref) {
	if s.global {
		return structure.TargetKindGlobal, nil
	}
	ref := s.ref
	return structure.TargetKindRef, &ref
}

// Set stores a value for a key at a scope, replacing any value already there.
// A value outside the key's constraints is rejected, not clamped.
func Set(ctx context.Context, r facade.Reader, w facade.Writer, key string, scope Scope, value any, actor string) (structure.PolicyInstance, error) {
	if err := scope.validate(); err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("noderolespolicy: set %s at %s: %w", key, scope, err)
	}
	def, found, err := r.GetPolicyDefinition(ctx, key)
	if err != nil {
		return structure.PolicyInstance{}, fmt.Errorf("noderolespolicy: set %s: %w", key, err)
	}
	if !found {
		return structure.PolicyInstance{}, fmt.Errorf("noderolespolicy: set %s: no such policy key (EnsureDefinitions first)", key)
	}
	kind, ref := scope.target()
	return facade.SetPolicyInstance(ctx, r, w, def, kind, ref, nil, value, structure.BindingInheritedLive, actor, false)
}

// Clear removes the value for a key at a scope. It reports whether there was
// one.
func Clear(ctx context.Context, r facade.Reader, w facade.Writer, key string, scope Scope, actor string) (bool, error) {
	if err := scope.validate(); err != nil {
		return false, fmt.Errorf("noderolespolicy: clear %s at %s: %w", key, scope, err)
	}
	def, found, err := r.GetPolicyDefinition(ctx, key)
	if err != nil {
		return false, fmt.Errorf("noderolespolicy: clear %s: %w", key, err)
	}
	if !found {
		return false, fmt.Errorf("noderolespolicy: clear %s: no such policy key", key)
	}
	var inst structure.PolicyInstance
	var has bool
	if scope.global {
		inst, has, err = r.GlobalInstance(ctx, def.PolicyDefinitionID)
	} else {
		var list []structure.PolicyInstance
		list, err = r.RefInstances(ctx, def.PolicyDefinitionID, []structure.Ref{scope.ref})
		if err == nil && len(list) > 0 {
			inst, has = list[0], true
		}
	}
	if err != nil {
		return false, fmt.Errorf("noderolespolicy: clear %s at %s: %w", key, scope, err)
	}
	if !has || inst.Lifecycle != structure.LifecycleActive {
		return false, nil
	}
	if _, err := facade.ArchivePolicyInstance(ctx, r, w, inst.PolicyInstanceID, actor); err != nil {
		return false, fmt.Errorf("noderolespolicy: clear %s at %s: %w", key, scope, err)
	}
	return true, nil
}
