package structure

import "fmt"

// Ref is the one generic attachment atom Policy resolves against —
// an open (Kind, Key) pair, opaque to Policy the same way Gatehouse-
// core's own Grant Context (context_type/context_id) is opaque to it.
// Kind is unvalidated vocabulary ("principal", "group", "role",
// "context", or anything a caller invents later); Key is an opaque
// identifier within that kind. Policy never parses either — this is
// what lets it bind to principals, groups, roles, contexts, and more
// without knowing what any of those are.
type Ref struct {
	Kind string
	Key  string
}

func (r Ref) Validate() error {
	if r.Kind == "" {
		return fmt.Errorf("structure: ref: Kind is required")
	}
	if r.Key == "" {
		return fmt.Errorf("structure: ref: Key is required")
	}
	return nil
}

func (r Ref) IsZero() bool {
	return r.Kind == "" && r.Key == ""
}
