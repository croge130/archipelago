package structure

import "fmt"

// ContextLifecycle mirrors the Alias base's active/released vocabulary
// deliberately — both are "a handle with a lifecycle," never load-
// bearing beyond identifying one specific resource instance.
type ContextLifecycle string

const (
	ContextLifecycleActive   ContextLifecycle = "active"
	ContextLifecycleReleased ContextLifecycle = "released"
)

func (l ContextLifecycle) Valid() bool {
	return l == ContextLifecycleActive || l == ContextLifecycleReleased
}

// ContextType registers a namespaced context kind (e.g.
// "myapp.readinglist") that Context instances and Grants can reference.
type ContextType struct {
	TypeKey     string
	Description string
}

func (t ContextType) Validate() error {
	if t.TypeKey == "" {
		return fmt.Errorf("structure: context type: TypeKey is required")
	}
	return nil
}

// Context identifies one specific resource instance a Grant can be
// scoped to — a specific reading list, never "reading lists" as a
// class. Exact-match only: no hierarchy, no policy conditions, per the
// model doc's deliberate restraint carried forward from Lighthouse's
// own Context v0.
type Context struct {
	Type      string // a registered ContextType's TypeKey
	ID        string
	Lifecycle ContextLifecycle
}

func (c Context) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("structure: context: Type is required")
	}
	if c.ID == "" {
		return fmt.Errorf("structure: context: ID is required")
	}
	if !c.Lifecycle.Valid() {
		return fmt.Errorf("structure: context: invalid Lifecycle %q", c.Lifecycle)
	}
	return nil
}
