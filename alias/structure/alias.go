package structure

import (
	"fmt"
	"time"
)

// Lifecycle mirrors Context's own active/released vocabulary in
// Gatehouse-core, deliberately — both are "a handle with a lifecycle,"
// never load-bearing beyond identifying one specific thing.
type Lifecycle string

const (
	LifecycleActive   Lifecycle = "active"
	LifecycleReleased Lifecycle = "released"
)

func (l Lifecycle) Valid() bool {
	return l == LifecycleActive || l == LifecycleReleased
}

// Alias is a table+name pair resolving to an opaque target value.
// Table and Name together are the natural primary key — no surrogate
// ID, since there's nothing to identify beyond the pair itself. Target
// stays whatever it was last set to even after release, preserving
// "what did this used to point to" for debugging rather than being
// cleared.
type Alias struct {
	Table      string
	Name       string
	Target     string
	Lifecycle  Lifecycle
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ReleasedAt *time.Time
}

func (a Alias) Validate() error {
	if a.Table == "" {
		return fmt.Errorf("structure: alias: Table is required")
	}
	if a.Name == "" {
		return fmt.Errorf("structure: alias: Name is required")
	}
	if a.Target == "" {
		return fmt.Errorf("structure: alias: Target is required")
	}
	if !a.Lifecycle.Valid() {
		return fmt.Errorf("structure: alias: invalid Lifecycle %q", a.Lifecycle)
	}
	if a.Lifecycle == LifecycleActive && a.ReleasedAt != nil {
		return fmt.Errorf("structure: alias: an active alias must not carry a ReleasedAt")
	}
	if a.Lifecycle == LifecycleReleased && a.ReleasedAt == nil {
		return fmt.Errorf("structure: alias: a released alias must carry a ReleasedAt")
	}
	return nil
}
